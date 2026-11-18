package cmd

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"
	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localClusterStatusExample = `
  # Show the status of the local cluster:
  educates local cluster status

  # Show the status of the named local cluster test2:
  educates local cluster status --local-cluster test2

  # Show every label of each node, those Kubernetes sets included:
  educates local cluster status --all-labels
`

func (p *ProjectInfo) NewLocalClusterStatusCmd() *cobra.Command {
	var allLabels bool

	var c = &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "status",
		Short: "Status of the local Kubernetes cluster",
		Long: `Shows whether the local cluster's node containers run: all of them
(Running), none (NOT Running), or only some (partly running, naming those
that are not).

Unless the cluster is stopped, it then lists the cluster's Kubernetes
nodes as its API server reports them, each with its role (control-plane
or worker), its status, its kubelet version, its labels and its taints.
The labels Kubernetes puts on every node, such as kubernetes.io/hostname,
are left out, so the list shows those that cluster.nodes declares in the
cluster's config and ingress-ready on the control-plane node. With
--all-labels it shows every label of each node.

Last, it shows whether Educates is installed on the cluster and whether
each of its platform components is Ready.`,
		Example: localClusterStatusExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			id, err := selectedEducatesCluster(cmd)
			if err != nil {
				return err
			}
			kindConfig := cluster.NewKindClusterConfig("", id)

			state, err := kindConfig.ClusterStatus()
			if err != nil {
				return err
			}

			if state != localclusters.Stopped {
				printLocalClusterNodes(cmd.Context(), kindConfig, allLabels)
			}

			printEducatesPlatformStatus(cmd.Context(), kindConfig)

			return nil
		},
	}

	addLocalClusterFlag(c)
	c.Flags().BoolVar(&allLabels, "all-labels", false,
		"show every label of each node, including those Kubernetes sets on every node")

	return c
}

// localClusterNodesTimeout bounds the query for the local cluster's nodes,
// so a cluster whose API server does not answer, such as one whose
// control-plane node is still starting, does not hold the command up.
const localClusterNodesTimeout = 10 * time.Second

// localClusterNodeColumns are the headers of the table of nodes local
// cluster status shows.
var localClusterNodeColumns = []string{"NODE", "ROLE", "STATUS", "VERSION", "LABELS", "TAINTS"}

// printLocalClusterNodes lists the local cluster's Kubernetes nodes as its
// API server reports them, with every label of each node when allLabels is
// set. Like printEducatesPlatformStatus it never fails the command: an API
// server it can't reach, as when the control-plane node is not running, is
// reported instead.
func printLocalClusterNodes(ctx context.Context, kindConfig *cluster.KindClusterConfig, allLabels bool) {
	// Target the kind cluster by its own kube context, whatever the
	// user's current kubectl context is.
	client, err := cluster.NewClusterConfig(kindConfig.Config.Kubeconfig, kindConfig.Identity.Context).GetClient()
	if err != nil {
		fmt.Println("\nNodes: unable to query the cluster")
		return
	}

	ctx, cancel := context.WithTimeout(ctx, localClusterNodesTimeout)
	defer cancel()
	nodes, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		fmt.Println("\nNodes: unable to query (cluster not reachable)")
		return
	}

	fmt.Println()
	fmt.Println(utils.PrintTable(localClusterNodeColumns, localClusterNodeRows(nodes.Items, allLabels)))
}

// localClusterNodeRows returns a row of the table of nodes for each of
// nodes, in their order, with every label of each node when allLabels is
// set.
func localClusterNodeRows(nodes []apiv1.Node, allLabels bool) [][]string {
	rows := make([][]string, 0, len(nodes))
	for _, node := range nodes {
		rows = append(rows, []string{
			node.Name,
			nodeRole(node),
			nodeStatus(node),
			orDash(node.Status.NodeInfo.KubeletVersion),
			orDash(strings.Join(nodeLabels(node, allLabels), ",")),
			orDash(strings.Join(nodeTaints(node), ",")),
		})
	}
	return rows
}

// nodeRole returns the node's role by kind's names: control-plane for a
// node labelled as one, and worker for every other node.
func nodeRole(node apiv1.Node) string {
	for _, key := range []string{"node-role.kubernetes.io/control-plane", "node-role.kubernetes.io/master"} {
		if _, ok := node.Labels[key]; ok {
			return localclusters.ControlPlaneRole
		}
	}
	return "worker"
}

// nodeStatus returns the node's status as kubectl get nodes shows it:
// Ready, NotReady, or Unknown when the node reports no Ready condition,
// followed by ,SchedulingDisabled when the node is cordoned.
func nodeStatus(node apiv1.Node) string {
	status := "Unknown"
	for _, condition := range node.Status.Conditions {
		if condition.Type != apiv1.NodeReady {
			continue
		}
		if condition.Status == apiv1.ConditionTrue {
			status = "Ready"
		} else {
			status = "NotReady"
		}
		break
	}
	if node.Spec.Unschedulable {
		status += ",SchedulingDisabled"
	}
	return status
}

// nodeLabels returns the node's labels as key=value, sorted by key. Unless
// all is set, those Kubernetes puts on every node are left out: see
// hiddenNodeLabel.
func nodeLabels(node apiv1.Node, all bool) []string {
	var labels []string
	for key, value := range node.Labels {
		if all || !hiddenNodeLabel(key) {
			labels = append(labels, key+"="+value)
		}
	}
	slices.Sort(labels)
	return labels
}

// hiddenNodeLabel reports whether the table of nodes leaves out the node
// label key: one the kubelet or kubeadm put on every kind node, such as
// kubernetes.io/hostname, or a node-role one, which the role shows.
func hiddenNodeLabel(key string) bool {
	return strings.HasPrefix(key, "kubernetes.io/") ||
		strings.HasPrefix(key, "beta.kubernetes.io/") ||
		strings.HasPrefix(key, "node-role.kubernetes.io/") ||
		key == "node.kubernetes.io/exclude-from-external-load-balancers"
}

// nodeTaints returns the node's taints as key=value:effect, or key:effect
// for a taint without a value, in the node's order.
func nodeTaints(node apiv1.Node) []string {
	taints := make([]string, 0, len(node.Spec.Taints))
	for _, taint := range node.Spec.Taints {
		if taint.Value != "" {
			taints = append(taints, fmt.Sprintf("%s=%s:%s", taint.Key, taint.Value, taint.Effect))
		} else {
			taints = append(taints, fmt.Sprintf("%s:%s", taint.Key, taint.Effect))
		}
	}
	return taints
}

// printEducatesPlatformStatus reports whether Educates is installed on the
// local cluster and whether its platform CRs are Ready, so the user can tell
// a cluster-only cluster (kind up, no Educates) from a non-functional one
// (installed but not Ready) from a fully-working one. It never fails the
// command: an unreachable API — for example a stopped cluster — is reported
// as unknown rather than returned as an error.
func printEducatesPlatformStatus(ctx context.Context, kindConfig *cluster.KindClusterConfig) {
	// Target the kind cluster explicitly by its context so the status
	// reflects the local cluster regardless of the user's current kubectl
	// context.
	dynamicClient, err := cluster.NewClusterConfig(kindConfig.Config.Kubeconfig, kindConfig.Identity.Context).GetDynamicClient()
	if err != nil {
		fmt.Println("\nEducates platform: unable to query the cluster")
		return
	}

	components, err := deployer.PlatformStatus(ctx, dynamicClient)
	if err != nil {
		fmt.Println("\nEducates platform: unable to query (cluster not reachable)")
		return
	}

	clusterConfigPresent := false
	fullyReady := true
	for _, comp := range components {
		if comp.Kind == "EducatesClusterConfig" && comp.Present {
			clusterConfigPresent = true
		}
		if comp.Optional {
			// Optional components only count against readiness when present
			// and not Ready.
			if comp.Present && !comp.Ready {
				fullyReady = false
			}
		} else {
			// Required components must be present and Ready.
			if !comp.Present || !comp.Ready {
				fullyReady = false
			}
		}
	}

	// No EducatesClusterConfig means Educates was never installed — a
	// cluster-only cluster (e.g. created with --cluster-only).
	if !clusterConfigPresent {
		fmt.Println("\nEducates is not installed on this cluster (cluster-only).")
		return
	}

	rows := make([][]string, 0, len(components))
	for _, comp := range components {
		var status string
		switch {
		case comp.Present && comp.Ready:
			status = "Ready"
		case comp.Present && comp.Phase != "":
			status = "NotReady (" + comp.Phase + ")"
		case comp.Present:
			status = "NotReady"
		case comp.Optional:
			status = "not enabled"
		default:
			status = "not installed"
		}
		rows = append(rows, []string{comp.Kind, status})
	}

	fmt.Println()
	fmt.Println(utils.PrintTable([]string{"COMPONENT", "STATUS"}, rows))
	fmt.Println()

	if fullyReady {
		fmt.Println("Educates is installed and ready.")
	} else {
		fmt.Println("Educates is installed but not fully ready.")
	}
}
