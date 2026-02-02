package cluster

import (
	"bytes"
	"context"
	_ "embed"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/pkg/errors"
	"golang.org/x/exp/slices"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/constants"
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

	if slices.Contains(clusters, constants.EducatesClusterName) {
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

	kindConfigPath := filepath.Join(configFileDir, fmt.Sprintf("%s-cluster-config.yaml", constants.EducatesClusterName))
	err = os.WriteFile(kindConfigPath, clusterConfigData.Bytes(), 0644)
	if err != nil {
		return errors.Wrap(err, "failed to write cluster config to file")
	}
	// TODO: Make this output only show when verbose is enabled
	fmt.Println("Cluster config used is saved to: ", kindConfigPath)

	if err := o.provider.Create(
		constants.EducatesClusterName,
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

	if err := o.provider.Delete(constants.EducatesClusterName, o.Config.Kubeconfig); err != nil {
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

	cli, err := utils.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	// Get all kind node containers for the educates cluster
	nodeFilters := filters.NewArgs()
	nodeFilters.Add("label", fmt.Sprintf("io.x-k8s.kind.cluster=%s", constants.EducatesClusterName))

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		Filters: nodeFilters,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list kind node containers")
	}

	if len(containers) == 0 {
		return errors.New("no containers found for Educates cluster")
	}

	fmt.Println("Stopping cluster educates ...")

	timeout := 30

	// Stop all containers (control-plane and workers)
	for _, c := range containers {
		containerName := c.Names[0]
		if len(c.Names) > 0 {
			// Remove leading slash from container name
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}
		}

		if err := cli.ContainerStop(ctx, c.ID, container.StopOptions{Timeout: &timeout}); err != nil {
			return errors.Wrapf(err, "failed to stop container %s", containerName)
		}
		fmt.Printf("  Stopped %s\n", containerName)
	}

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

	cli, err := utils.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	// Get all kind node containers for the educates cluster
	nodeFilters := filters.NewArgs()
	nodeFilters.Add("label", fmt.Sprintf("io.x-k8s.kind.cluster=%s", constants.EducatesClusterName))

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true, // Include stopped containers
		Filters: nodeFilters,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list kind node containers")
	}

	if len(containers) == 0 {
		return errors.New("no containers found for Educates cluster")
	}

	fmt.Println("Starting cluster educates ...")

	// Start all containers (control-plane and workers)
	for _, c := range containers {
		containerName := c.Names[0]
		if len(c.Names) > 0 {
			// Remove leading slash from container name
			if len(containerName) > 0 && containerName[0] == '/' {
				containerName = containerName[1:]
			}
		}

		if c.State != "running" {
			if err := cli.ContainerStart(ctx, c.ID, container.StartOptions{}); err != nil {
				return errors.Wrapf(err, "failed to start container %s", containerName)
			}
			fmt.Printf("  Started %s\n", containerName)
		} else {
			fmt.Printf("  %s already running\n", containerName)
		}
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

	cli, err := utils.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	// Get all kind node containers for the educates cluster
	nodeFilters := filters.NewArgs()
	nodeFilters.Add("label", fmt.Sprintf("io.x-k8s.kind.cluster=%s", constants.EducatesClusterName))

	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: nodeFilters,
	})
	if err != nil {
		return errors.Wrap(err, "failed to list kind node containers")
	}

	if len(containers) == 0 {
		return errors.New("no containers found for Educates cluster")
	}

	// Check if all containers are running
	allRunning := true
	for _, c := range containers {
		if c.State != "running" {
			allRunning = false
			break
		}
	}

	if allRunning {
		fmt.Println("Educates cluster is Running")
	} else {
		fmt.Println("Educates cluster is NOT Running (some containers stopped)")
		return nil
	}

	// Get Kubernetes client to query nodes
	k8sClient, err := o.Config.GetClient()
	if err != nil {
		fmt.Println("  Warning: Unable to connect to Kubernetes API")
		return nil
	}

	// List nodes from Kubernetes API
	nodes, err := k8sClient.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Println("  Warning: Unable to list nodes from Kubernetes")
		return nil
	}

	var formattedData [][]string

	for _, node := range nodes.Items {
		var customLabelsData []string
		var taintsData []string
		// Determine role
		role := "worker"
		if _, ok := node.Labels["node-role.kubernetes.io/control-plane"]; ok {
			role = "control-plane"
		} else if _, ok := node.Labels["node-role.kubernetes.io/master"]; ok {
			role = "control-plane"
		}

		// Get status
		status := "Unknown"
		for _, condition := range node.Status.Conditions {
			if condition.Type == corev1.NodeReady {
				if condition.Status == corev1.ConditionTrue {
					status = "Ready"
				} else {
					status = "NotReady"
				}
				break
			}
		}

		// Get version
		version := node.Status.NodeInfo.KubeletVersion

		// Show custom labels (exclude system labels)
		customLabels := make(map[string]string)
		for k, v := range node.Labels {
			if !strings.HasPrefix(k, "node-role.kubernetes.io/") &&
				!strings.HasPrefix(k, "kubernetes.io/") &&
				!strings.HasPrefix(k, "beta.kubernetes.io/"){
				// && k != "ingress-ready" {
				customLabels[k] = v
			}
		}

		if len(customLabels) > 0 {
			for k, v := range customLabels {
				customLabelsData = append(customLabelsData, fmt.Sprintf("%s=%s", k, v))
			}
		}

		// Show taints
		if len(node.Spec.Taints) > 0 {
			for _, taint := range node.Spec.Taints {
				if taint.Value != "" {
					taintsData = append(taintsData, fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect))
				} else {
					taintsData = append(taintsData, fmt.Sprintf("%s:%s", taint.Key, taint.Effect))
				}
			}
		}
		formattedData = append(formattedData, []string{node.Name, role, status, version, strings.Join(customLabelsData, ", "), strings.Join(taintsData, ", ")})
	}

	fmt.Println(utils.PrintTable(
		[]string{"NODE", "ROLE", "STATUS", "VERSION", "LABELS", "TAINTS"},
		formattedData,
	))

	return nil
}
