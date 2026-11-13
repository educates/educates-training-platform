package cmd

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
)

const adminPlatformDeleteExample = `
  # Uninstall Educates (prompts for confirmation on a TTY):
  educates admin platform delete

  # Uninstall without prompting (required in CI / non-interactive shells):
  educates admin platform delete --yes

  # Uninstall and also remove the CRDs and operator namespaces:
  educates admin platform delete --purge --yes
`

type PlatformDeleteOptions struct {
	KubeconfigOptions
	Timeout time.Duration
	Verbose bool
	Yes     bool
	Purge   bool
}

func (p *ProjectInfo) NewAdminPlatformDeleteCmd() *cobra.Command {
	var o PlatformDeleteOptions

	c := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "delete",
		Short: "Uninstall Educates: drain platform CRs + helm uninstall",
		Long: `Reverse of 'admin platform deploy':

  1. delete SessionManager → wait gone
  2. delete LookupService → wait gone
  3. delete SecretsManager → wait gone
  4. delete EducatesClusterConfig → wait gone
       (the ECC finalizer drains kyverno, external-dns, contour,
        cert-manager and the CustomCA Secret copy in reverse install order,
        each only when Educates installed it: kyverno with the Bundled
        kyverno provider and a Kyverno policy engine, external-dns with
        the BundledExternalDNS DNS provider, contour with the
        BundledContour ingress controller provider, and cert-manager with
        the BundledCertManager certificates provider. A cluster service
        you provide yourself stays in place)
  5. helm uninstall the operator chart

By default this is idempotent and leaves cluster-shared state alone:
missing CRs are skipped silently; the four CRDs, the operator
namespace, the educates-secrets namespace, and any locally-cached
secrets stay in place so the next deploy reuses them.

--purge extends the pipeline AFTER helm uninstall to also remove the
CRDs and the operator + educates-secrets namespaces. Local
<data-home>/config.yaml + cached CA Secret YAMLs survive — they're
your authoring inputs, kept across cluster reinstalls.

A confirmation prompt fires when stdout is a TTY; pass --yes to skip
it (required when running under CI / non-interactive shells without
piping).

Unlike deploy, this command takes no --config / --local-config — the
resources are always the four CRs at metadata.name=cluster plus the
educates-installer release.

--local-cluster or EDUCATES_LOCAL_CLUSTER selects a local cluster, and
unless --context names another, the uninstall acts on that cluster's kube
context, kind-educates or kind-educates-<name>. With neither set, it acts
on the current context.`,
		Example: adminPlatformDeleteExample,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return p.runDelete(cmd.Context(), cmd.OutOrStdout(), &o)
		},
	}

	c.Flags().StringVar(&o.Kubeconfig, "kubeconfig", "", "kubeconfig file (defaults to $KUBECONFIG / ~/.kube/config)")
	c.Flags().StringVar(&o.Context, "context", "", "context name to use within the kubeconfig")
	addLocalClusterContextFlag(c, &o.KubeconfigOptions)
	c.Flags().DurationVar(&o.Timeout, "timeout", deployer.DefaultTimeout, "per-CR finalizer-drain wait timeout")
	c.Flags().BoolVar(&o.Verbose, "verbose", false, "show helm SDK debug output on stderr")
	c.Flags().BoolVarP(&o.Yes, "yes", "y", false, "skip the confirmation prompt")
	c.Flags().BoolVar(&o.Purge, "purge", false, "also remove the 4 CRDs + operator namespace + educates-secrets namespace (cluster-shared state)")

	return c
}

func (p *ProjectInfo) runDelete(ctx context.Context, w io.Writer, o *PlatformDeleteOptions) error {
	if err := confirmDelete(w, o); err != nil {
		return err
	}

	cf := genericclioptions.NewConfigFlags(true)
	if o.Kubeconfig != "" {
		cf.KubeConfig = &o.Kubeconfig
	}
	if o.Context != "" {
		cf.Context = &o.Context
	}
	ns := deployer.OperatorNamespace
	cf.Namespace = &ns

	helmLog := io.Discard
	if o.Verbose {
		helmLog = w
	}

	return deployer.Delete(ctx, deployer.DeleteOptions{
		Getter:  cf,
		Out:     w,
		HelmLog: helmLog,
		Timeout: o.Timeout,
		// Verbose streams helm debug to w, so drop in-place morphing and
		// commit each step on its own line to interleave cleanly.
		Progress: progress.New(w, 0, isStdoutTTY(w) && !o.Verbose),
		Purge:    o.Purge,
	})
}

// confirmDelete renders an itemised list of what's about to be deleted
// and gates on the user typing 'yes'. Skipped when --yes is set OR
// when stdin isn't a TTY (CI runs accidentally hanging on a prompt is
// worse than the destructive-action risk; users in CI should pass --yes
// explicitly to be unambiguous).
func confirmDelete(w io.Writer, o *PlatformDeleteOptions) error {
	if o.Yes {
		return nil
	}
	if !isStdinTTY() {
		return fmt.Errorf("non-interactive shell detected; pass --yes to skip the confirmation prompt")
	}
	fmt.Fprintln(w, "This command will delete the following from the cluster:")
	for _, line := range deleteInventory(o.Purge) {
		fmt.Fprintln(w, "  - "+line)
	}
	if !o.Purge {
		fmt.Fprintln(w, "  (CRDs, operator namespace, and educates-secrets namespace stay — pass --purge to remove)")
	}
	fmt.Fprintln(w, "  (Local <data-home>/config.yaml + cached CA Secret YAMLs are never touched)")
	fmt.Fprint(w, "Type 'yes' to confirm: ")

	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read confirmation: %w", err)
	}
	if strings.TrimSpace(answer) != "yes" {
		return fmt.Errorf("aborted")
	}
	return nil
}

// deleteInventory renders the human-readable list of cluster
// resources the run will touch. The order mirrors the actual delete
// sequence so the prompt's mental model matches what the user sees in
// the progress output afterward.
func deleteInventory(purge bool) []string {
	out := []string{
		"SessionManager/cluster (platform.educates.dev)",
		"LookupService/cluster (platform.educates.dev)",
		"SecretsManager/cluster (platform.educates.dev)",
		"EducatesClusterConfig/cluster (config.educates.dev)",
		"helm release: educates-installer (in namespace " + deployer.OperatorNamespace + ")",
	}
	if !purge {
		return out
	}
	plan := deployer.PurgePlan()
	for _, name := range plan.CRDs {
		out = append(out, "CRD: "+name)
	}
	for _, name := range plan.Namespaces {
		out = append(out, "namespace: "+name+" (and everything inside it)")
	}
	return out
}

// isStdinTTY mirrors isStdoutTTY but for the input stream — used by
// confirmDelete to decide whether to prompt or refuse-and-instruct.
func isStdinTTY() bool {
	info, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
