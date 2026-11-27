package cluster

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/moby/moby/client"
	"github.com/pkg/errors"
	"golang.org/x/exp/slices"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
	"sigs.k8s.io/kind/pkg/log"

	"github.com/educates/educates-training-platform/client-programs/pkg/docker"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// phaseLogger adapts kind's logger interface to a single status
// callback. kind's status helper (cli.StatusForLogger) only drives its
// terminal spinner when the logger is kind's own *cli.Logger; for any
// other logger it falls back to logging each phase via V(0).Infof
// (" • Ensuring node image  ...", " ✓ Ensuring node image", …). We
// intercept those, clean off the marker/ellipsis, and forward the phase
// text to onPhase so the create flow can surface it on its own single
// progress line. Warnings and errors still reach the user on stderr;
// higher verbosity levels are dropped.
type phaseLogger struct {
	onPhase func(string)
}

func (l phaseLogger) Warn(message string)            { fmt.Fprintln(os.Stderr, message) }
func (l phaseLogger) Warnf(format string, a ...any)  { fmt.Fprintf(os.Stderr, format+"\n", a...) }
func (l phaseLogger) Error(message string)           { fmt.Fprintln(os.Stderr, message) }
func (l phaseLogger) Errorf(format string, a ...any) { fmt.Fprintf(os.Stderr, format+"\n", a...) }
func (l phaseLogger) V(level log.Level) log.InfoLogger {
	return phaseInfo{onPhase: l.onPhase, enabled: level == 0}
}

// phaseInfo is the InfoLogger half of phaseLogger. Only V(0) is enabled,
// so kind skips formatting the noisier debug/trace levels entirely.
type phaseInfo struct {
	onPhase func(string)
	enabled bool
}

func (i phaseInfo) Enabled() bool            { return i.enabled }
func (i phaseInfo) Info(message string)      { i.emit(message) }
func (i phaseInfo) Infof(f string, a ...any) { i.emit(fmt.Sprintf(f, a...)) }

func (i phaseInfo) emit(s string) {
	if !i.enabled || i.onPhase == nil {
		return
	}
	if s = cleanKindStatus(s); s != "" {
		i.onPhase(s)
	}
}

// cleanKindStatus strips the bullet/check/cross marker and trailing
// ellipsis that kind's Status wraps each phase in, leaving just the
// phase text (e.g. "Ensuring node image (kindest/node:v1.36.4) 🖼").
func cleanKindStatus(s string) string {
	s = strings.TrimSpace(s)
	for _, marker := range []string{"•", "✓", "✗"} {
		s = strings.TrimPrefix(s, marker)
	}
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "...")
	return strings.TrimSpace(s)
}

// KindClusterConfig manages the kind cluster of one local cluster, named
// by its identity.
type KindClusterConfig struct {
	Config   ClusterConfig
	Identity localclusters.Identity
	provider *cluster.Provider
}

// NewKindClusterConfig manages the kind cluster of the local cluster id,
// with its context in the kubeconfig file kubeconfig, or in
// ~/.kube/config when kubeconfig is empty. Config reaches the cluster
// through that file and the cluster's own context, never the file's
// current context.
func NewKindClusterConfig(kubeconfig string, id localclusters.Identity) *KindClusterConfig {
	provider := cluster.NewProvider(
		cluster.ProviderWithLogger(cmd.NewLogger()),
	)

	return &KindClusterConfig{ClusterConfig{KindKubeconfigPath(kubeconfig), id.Context}, id, provider}
}

// ClusterExists reports whether the local cluster's kind cluster
// currently exists. err is set only when the underlying list call
// failed; the existence outcome itself is not an error: callers decide
// whether "exists" or "does not exist" is acceptable for the operation
// they're performing (CreateCluster wants !exists; Delete/Start/Stop/Status
// want exists).
func (o *KindClusterConfig) ClusterExists() (bool, error) {
	clusters, err := o.provider.List()
	if err != nil {
		return false, errors.Wrap(err, "unable to get list of clusters")
	}
	return slices.Contains(clusters, o.Identity.KindName), nil
}

// CreateCluster boots the local cluster's kind cluster from the kind
// config file it writes at the identity's kind config path. By default
// kind's own status spinner and end-of-create salutation/usage footer are
// suppressed: each kind phase is forwarded to onPhase (so the caller can
// render it on one line), and the footer chatter is turned off. When
// verbose is true kind's full logger is used instead (spinner + detail
// on stderr) and onPhase is ignored.
func (o *KindClusterConfig) CreateCluster(input *KindBootstrapInput, image string, onPhase func(string), verbose bool) error {
	if exists, err := o.ClusterExists(); err != nil {
		return err
	} else if exists {
		return errors.Errorf("kind cluster %s already exists", o.Identity.KindName)
	}

	kindConfig, err := RenderKindConfig(input)
	if err != nil {
		return err
	}

	// Save the cluster config to a file

	kindConfigPath := o.Identity.KindConfigPath

	err = os.MkdirAll(filepath.Dir(kindConfigPath), os.ModePerm)

	if err != nil {
		return errors.Wrapf(err, "unable to create config directory")
	}

	err = os.WriteFile(kindConfigPath, kindConfig, 0644)
	if err != nil {
		return errors.Wrap(err, "failed to write cluster config to file")
	}
	if verbose {
		fmt.Println("Cluster config used is saved to: ", kindConfigPath)
	}

	// Verbose: kind's own logger (terminal spinner + detail). Default:
	// forward each phase to onPhase and stay silent otherwise.
	var logger log.Logger = phaseLogger{onPhase: onPhase}
	if verbose {
		logger = cmd.NewLogger()
	}
	provider := cluster.NewProvider(cluster.ProviderWithLogger(logger))

	if err := provider.Create(
		o.Identity.KindName,
		cluster.CreateWithRawConfig(kindConfig),
		cluster.CreateWithNodeImage(image),
		cluster.CreateWithWaitForReady(time.Duration(time.Duration(60)*time.Second)),
		cluster.CreateWithKubeconfigPath(o.Config.Kubeconfig),
		cluster.CreateWithDisplayUsage(false),
		cluster.CreateWithDisplaySalutation(false),
	); err != nil {
		return errors.Wrap(err, "failed to create cluster")
	}

	return nil
}

// NotFoundError is the error of an operation on a local cluster whose
// kind cluster does not exist.
type NotFoundError struct {
	// KindName is the name of the kind cluster.
	KindName string
}

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("kind cluster %s does not exist", e.KindName)
}

// IsNotFound reports whether err is, or wraps, a NotFoundError.
func IsNotFound(err error) bool {
	var notFound *NotFoundError
	return errors.As(err, &notFound)
}

// DeleteCluster deletes the local cluster's kind cluster, returning a
// NotFoundError when it does not exist.
func (o *KindClusterConfig) DeleteCluster() error {
	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return err
		}
		return &NotFoundError{KindName: o.Identity.KindName}
	}

	fmt.Printf("Deleting cluster %s ...\n", o.Identity.KindName)

	if err := o.provider.Delete(o.Identity.KindName, o.Config.Kubeconfig); err != nil {
		return errors.Wrapf(err, "failed to delete cluster")
	}

	return nil
}

// StopCluster stops every running node container of the local cluster,
// workers before control-plane nodes, printing a line for each node it
// stops, or one saying the cluster is already stopped.
func (o *KindClusterConfig) StopCluster() error {
	ctx := context.Background()

	observed, err := o.observeExistingCluster(ctx)
	if err != nil {
		return err
	}

	nodes := o.Identity.NodesToStop(observed)
	if len(nodes) == 0 {
		fmt.Printf("Cluster %s is already stopped\n", o.Identity.KindName)
		return nil
	}

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	fmt.Printf("Stopping cluster %s ...\n", o.Identity.KindName)

	timeout := 30

	for _, node := range nodes {
		if _, err := cli.ContainerStop(ctx, node, client.ContainerStopOptions{Timeout: &timeout}); err != nil {
			return errors.Wrapf(err, "failed to stop cluster: node container %s", node)
		}
		fmt.Printf("  Stopped %s\n", node)
	}

	return nil
}

// StartCluster starts every node container of the local cluster that is
// not running, control-plane nodes first, printing a line for each node
// it starts, or one saying the cluster is already running.
func (o *KindClusterConfig) StartCluster() error {
	ctx := context.Background()

	observed, err := o.observeExistingCluster(ctx)
	if err != nil {
		return err
	}

	nodes := o.Identity.NodesToStart(observed)
	if len(nodes) == 0 {
		fmt.Printf("Cluster %s is already running\n", o.Identity.KindName)
		return nil
	}

	cli, err := docker.NewDockerClient()

	if err != nil {
		return errors.Wrap(err, "unable to create docker client")
	}

	fmt.Printf("Starting cluster %s ...\n", o.Identity.KindName)

	for _, node := range nodes {
		if _, err := cli.ContainerStart(ctx, node, client.ContainerStartOptions{}); err != nil {
			return errors.Wrapf(err, "failed to start cluster: node container %s", node)
		}
		fmt.Printf("  Started %s\n", node)
	}

	return nil
}

// ClusterStatus prints whether the local cluster's node containers are
// all running, none running, or only some, naming those that are not, and
// returns that state, or an error when the cluster does not exist.
func (o *KindClusterConfig) ClusterStatus() (localclusters.State, error) {
	ctx := context.Background()

	observed, err := o.observeExistingCluster(ctx)
	if err != nil {
		return "", err
	}

	state := o.Identity.State(observed)
	switch state {
	case localclusters.Running:
		fmt.Printf("Educates cluster %s is Running\n", o.Identity.KindName)
	case localclusters.PartlyRunning:
		fmt.Printf("Educates cluster %s is %s (not running: %s)\n", o.Identity.KindName,
			localclusters.PartlyRunning, strings.Join(o.Identity.NodesToStart(observed), ", "))
	default:
		fmt.Printf("Educates cluster %s is NOT Running\n", o.Identity.KindName)
	}

	return state, nil
}

// observeExistingCluster returns the kind node containers on the Docker
// host, or an error when the local cluster does not exist.
func (o *KindClusterConfig) observeExistingCluster(ctx context.Context) ([]localclusters.NodeContainer, error) {
	if exists, err := o.ClusterExists(); !exists {
		if err != nil {
			return nil, err
		}
		return nil, &NotFoundError{KindName: o.Identity.KindName}
	}

	return ObserveNodeContainers(ctx)
}
