package cluster

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/pkg/errors"
	"golang.org/x/exp/slices"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

type KindClusterConfig struct {
	Config   ClusterConfig
	provider *cluster.Provider
}

func NewKindClusterConfig(kubeconfig string) *KindClusterConfig {
	fallback := ""

	home, err := os.UserHomeDir()

	if err == nil {
		fallback = filepath.Join(home, clientcmd.RecommendedHomeDir, clientcmd.RecommendedFileName)
	}

	provider := cluster.NewProvider(
		cluster.ProviderWithLogger(cmd.NewLogger()),
	)

	return &KindClusterConfig{ClusterConfig{KubeconfigPath(kubeconfig, fallback), ""}, provider}
}

//go:embed kindclusterconfig.yaml.tpl
var clusterConfigTemplateData string

func (o *KindClusterConfig) ClusterExists() (bool, error) {
	clusters, err := o.provider.List()

	if err != nil {
		return false, errors.Wrap(err, "unable to get list of clusters")
	}

	if slices.Contains(clusters, "educates") {
		return true, errors.New("cluster for Educates already exists")
	}

	return false, nil
}

func (o *KindClusterConfig) CreateCluster(config *config.InstallationConfig, image string) error {
	if exists, err := o.ClusterExists(); !exists && err != nil {
		return err
	}

	clusterConfigTemplate, err := template.New("kind-cluster-config").Parse(clusterConfigTemplateData)

	if err != nil {
		return errors.Wrap(err, "failed to parse cluster config template")
	}

	var clusterConfigData bytes.Buffer

	err = clusterConfigTemplate.Execute(&clusterConfigData, config)

	if err != nil {
		return errors.Wrap(err, "failed to generate cluster config")
	}

	// Save the cluster config to a file

	configFileDir := utils.GetEducatesHomeDir()

	err = os.MkdirAll(configFileDir, os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create config directory")
	}

	kindConfigPath := filepath.Join(configFileDir, "educates-cluster-config.yaml")
	err = os.WriteFile(kindConfigPath, clusterConfigData.Bytes(), 0644)
	if err != nil {
		return errors.Wrap(err, "failed to write cluster config to file")
	}
	// TODO: Make this output only show when verbose is enabled
	fmt.Println("Cluster config used is saved to: ", kindConfigPath)

	if err := o.provider.Create(
		"educates",
		cluster.CreateWithRawConfig(clusterConfigData.Bytes()),
		cluster.CreateWithNodeImage(image),
		cluster.CreateWithWaitForReady(time.Duration(time.Duration(60)*time.Second)),
		cluster.CreateWithKubeconfigPath(o.Config.Kubeconfig),
		cluster.CreateWithDisplayUsage(true),
		cluster.CreateWithDisplaySalutation(true),
	); err != nil {
		return errors.Wrap(err, "failed to create cluster")
	}

	return nil
}

func (o *KindClusterConfig) DeleteCluster() error {
	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return err
		}
		return errors.New("cluster for Educates does not exist")
	}

	fmt.Println("Deleting cluster educates ...")

	if err := o.provider.Delete("educates", o.Config.Kubeconfig); err != nil {
		return errors.Wrapf(err, "failed to delete cluster")
	}

	return nil
}

func (o *KindClusterConfig) StopCluster() error {
	ctx := context.Background()

	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return err
		}
		return errors.New("cluster for Educates does not exist")
	}

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, "educates-control-plane")

	if err != nil {
		return errors.Wrap(err, "no container for Educates cluster")
	}

	fmt.Println("Stopping cluster educates ...")

	timeout := 30

	if err := cli.ContainerStop(ctx, "educates-control-plane", container.StopOptions{Timeout: &timeout}); err != nil {
		return errors.Wrapf(err, "failed to stop cluster")
	}

	// timeout := time.Duration(30) * time.Second

	// if err := cli.ContainerStop(ctx, "educates-control-plane", &timeout); err != nil {
	// 	return errors.Wrapf(err, "failed to stop cluster")
	// }

	return nil
}

func (o *KindClusterConfig) StartCluster() error {
	ctx := context.Background()

	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return err
		}
		return errors.New("cluster for Educates does not exist")
	}

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	_, err = cli.ContainerInspect(ctx, "educates-control-plane")

	if err != nil {
		return errors.Wrap(err, "no container for Educates cluster")
	}

	fmt.Println("Starting cluster educates ...")

	if err := cli.ContainerStart(ctx, "educates-control-plane", container.StartOptions{}); err != nil {
		return errors.Wrapf(err, "failed to start cluster")
	}

	return nil
}

func (o *KindClusterConfig) ClusterStatus() error {
	ctx := context.Background()

	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return err
		}
		return errors.New("cluster for Educates does not exist")
	}

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	containerJSON, err := cli.ContainerInspect(ctx, "educates-control-plane")

	if err != nil {
		return errors.Wrap(err, "no container for Educates cluster")
	}

	if containerJSON.State.Running {
		fmt.Println("Educates cluster is Running")
		// if ip, err := config.HostIP(); err == nil {
		// 	fmt.Println("  Cluster IP: ", ip)
		// }
	} else {
		fmt.Println("Educates cluster is NOT Running")
	}

	return nil
}
