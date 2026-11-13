package cmd

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

var localClusterStatusExample = `
  # Show the status of the local cluster:
  educates local cluster status
`

func (p *ProjectInfo) NewLocalClusterStatusCmd() *cobra.Command {
	var c = &cobra.Command{
		Args:    cobra.NoArgs,
		Use:     "status",
		Short:   "Status of the local Kubernetes cluster",
		Example: localClusterStatusExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			kindConfig := cluster.NewKindClusterConfig("", defaultLocalCluster())

			if err := kindConfig.ClusterStatus(); err != nil {
				return err
			}

			printEducatesPlatformStatus(cmd.Context(), kindConfig)

			return nil
		},
	}

	return c
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
