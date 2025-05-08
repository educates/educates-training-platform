package cmd

import (
	"context"
	_ "embed"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/go-connections/nat"
	"github.com/pkg/errors"
	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/installer"
	"github.com/educates/educates-training-platform/client-programs/pkg/registry"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var (
	localClusterCreateExample = `
  # Create local educates cluster (no configuration, uses nip.io wildcard domain and Kind as provider config defaults)
  educates local cluster create

  # Create local educates cluster with custom configuration
  educates local cluster create --config config.yaml

  # Create local kind cluster but don't install anything on it (it creates local registry but not local secrets)
  educates local cluster create --cluster-only

  # Create local kind cluster but don't install anything on it, but providing some config for kind
  educates local cluster create --cluster-only --config kind-config.yaml

  # Create local educates cluster with bundle from different repository
  educates local cluster create --package-repository ghcr.io/jorgemoralespou --version installer-clean

  # Create local educates cluster with local build (for development)
  educates local cluster create --package-repository localhost:5001 --version 0.0.1

  # Create local educates cluster with default configuration for a given domain
  educates local cluster create --domain test.educates.io

  # Create local educates cluster with custom configuration providing a domain
  educates local cluster create --config config.yaml --domain test.educates.io

`
)

type LocalClusterCreateOptions struct {
	Config              string
	Kubeconfig          string
	ClusterImage        string
	Domain              string
	PackageRepository   string
	Version             string
	ClusterOnly         bool
	Verbose             bool
	SkipImageResolution bool
	RegistryBindIP      string
}

func (o *LocalClusterCreateOptions) Run() error {

	fullConfig, err := config.ConfigForLocalClusters(o.Config, o.Domain, true)

	if err != nil {
		return err
	}

	if o.Verbose {
		config.PrintConfigToStdout(fullConfig)
	}

	clusterConfig := cluster.NewKindClusterConfig(o.Kubeconfig)

	if exists, err := clusterConfig.ClusterExists(); exists && err != nil {
		return err
	}

	httpAvailable, err := checkPortAvailability(fullConfig.LocalKindCluster.ListenAddress, []uint{80, 443}, o.Verbose)

	if err != nil {
		return errors.Wrap(err, "couldn't test whether ports 80/443 available")
	}

	if !httpAvailable {
		return errors.New("ports 80/443 not available")
	}

	err = clusterConfig.CreateCluster(fullConfig, o.ClusterImage)

	if err != nil {
		return err
	}

	client, err := clusterConfig.Config.GetClient()

	if err != nil {
		return err
	}

	// This creates the educates-secrets namespace if it doesn't exist and creates the
	// wildcard and CA secrets in there
	if !o.ClusterOnly {
		if err = secrets.SyncLocalCachedSecretsToCluster(client); err != nil {
			return err
		}
	}

	if err = registry.DeployRegistryAndLinkToCluster(o.RegistryBindIP, client); err != nil {
		return errors.Wrap(err, "failed to deploy registry")
	}

	// This is needed for imgpkg pull from locally published workshops
	if err = registry.UpdateRegistryK8SService(client); err != nil {
		return errors.Wrap(err, "failed to create service for registry")
	}

	// This is for hugo livereload (educates serve-workshop)
	if err = cluster.CreateLoopbackService(client, fullConfig.ClusterIngress.Domain); err != nil {
		return err
	}

	// Create and add registry mirrors defined in config to Kind nodes
	for _, mirror := range fullConfig.LocalKindCluster.RegistryMirrors {
		if err = registry.DeployMirrorAndLinkToCluster(&mirror); err != nil {
			return errors.Wrap(err, "failed to deploy registry mirror "+mirror.Mirror)
		}
	}

	if !o.ClusterOnly {
		installer := installer.NewInstaller()
		err = installer.Run(o.Version, o.PackageRepository, fullConfig, &clusterConfig.Config, o.Verbose, false, o.SkipImageResolution, false)
		if err != nil {
			return errors.Wrap(err, "educates could not be installed")
		}
	}

	fmt.Println("Educates cluster has been created succesfully")

	return nil
}

func (p *ProjectInfo) NewLocalClusterCreateCmd() *cobra.Command {
	var o LocalClusterCreateOptions

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "create",
		Short: "Creates a local Kubernetes cluster",
		RunE: func(cmd *cobra.Command, _ []string) error {
			ip, err := utils.ValidateAndResolveIP(o.RegistryBindIP)
			if err != nil {
				return errors.Wrap(err, "invalid registry bind IP")
			}
			o.RegistryBindIP = ip

			return o.Run()
		},
		Example: localClusterCreateExample,
	}

	c.Flags().StringVar(
		&o.Config,
		"config",
		"",
		"path to the installation config file for Educates",
	)
	c.Flags().StringVar(
		&o.Kubeconfig,
		"kubeconfig",
		"",
		"kubeconfig file to use instead of $HOME/.kube/config",
	)
	c.Flags().StringVar(
		&o.ClusterImage,
		"kind-cluster-image",
		"",
		"docker image to use when booting the kind cluster",
	)
	c.Flags().StringVar(
		&o.Domain,
		"domain",
		"",
		"wildcard ingress subdomain name for Educates",
	)
	c.Flags().StringVar(
		&o.PackageRepository,
		"package-repository",
		p.ImageRepository,
		"image repository hosting package bundles",
	)
	c.Flags().StringVar(
		&o.Version,
		"version",
		p.Version,
		"version of Educates training platform to be installed",
	)
	c.Flags().BoolVar(
		&o.ClusterOnly,
		"cluster-only",
		false,
		"only create the cluster, do not install Educates",
	)
	c.Flags().BoolVar(
		&o.Verbose,
		"verbose",
		false,
		"print verbose output",
	)
	c.Flags().BoolVar(
		&o.SkipImageResolution,
		"skip-image-resolution",
		false,
		"skips resolution of referenced images so that all will be fetched from their original location",
	)
	c.Flags().StringVar(
		&o.RegistryBindIP,
		"registry-bind-ip",
		"127.0.0.1",
		"Bind ip for the registry service",
	)
	return c
}

func checkPortAvailability(listenAddress string, ports []uint, verbose bool) (bool, error) {
	ctx := context.Background()

	cli, err := client.NewClientWithOpts(client.FromEnv)

	if err != nil {
		return false, errors.Wrap(err, "unable to create docker client")
	}

	cli.ContainerRemove(ctx, "educates-port-availability-check", container.RemoveOptions{})

	reader, err := cli.ImagePull(ctx, "docker.io/library/busybox:latest", image.PullOptions{})
	if err != nil {
		return false, errors.Wrap(err, "cannot pull busybox image")
	}

	defer reader.Close()

	if verbose {
		io.Copy(os.Stdout, reader)
	} else {
		io.Copy(io.Discard, reader)
	}

	if listenAddress == "" {
		listenAddress, err = config.HostIP()

		if err != nil {
			listenAddress = "127.0.0.1"
		}
	}

	hostConfig := &container.HostConfig{
		PortBindings: nat.PortMap{},
	}

	exposedPorts := nat.PortSet{}

	for _, port := range ports {
		key := nat.Port(fmt.Sprintf("%d/tcp", port))
		hostConfig.PortBindings[key] = []nat.PortBinding{
			{
				HostIP:   listenAddress,
				HostPort: fmt.Sprintf("%d", port),
			},
		}
		exposedPorts[key] = struct{}{}
	}

	resp, err := cli.ContainerCreate(ctx, &container.Config{
		Image:        "docker.io/library/busybox:latest",
		Cmd:          []string{"/bin/true"},
		Tty:          false,
		ExposedPorts: exposedPorts,
	}, hostConfig, nil, nil, "educates-port-availability-check")

	if err != nil {
		return false, errors.Wrap(err, "cannot create busybox container")
	}

	defer cli.ContainerRemove(ctx, "educates-port-availability-check", container.RemoveOptions{})

	if err := cli.ContainerStart(ctx, resp.ID, container.StartOptions{}); err != nil {
		return false, errors.Wrap(err, "cannot start busybox container")
	}

	statusCh, errCh := cli.ContainerWait(ctx, "educates-port-availability-check", container.WaitConditionNotRunning)

	select {
	case err := <-errCh:
		if err != nil {
			return false, nil
		}
	case <-statusCh:
	}

	return true, nil
}
