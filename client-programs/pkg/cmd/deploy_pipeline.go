package cmd

import (
	"context"
	"io"
	"os"
	"time"

	"k8s.io/cli-runtime/pkg/genericclioptions"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
)

// deployPipelineFlags collects the kubectl/helm connection flags shared
// by both admin platform deploy and local cluster create's tail-call.
// Extracted so the two cmd paths can't drift on which flags they plumb
// (the original drift was tailCallDeploy missing --context).
type deployPipelineFlags struct {
	Kubeconfig string
	Context    string
	Timeout    time.Duration
	Verbose    bool
}

// translateAndDeploy is the shared "config → translator output → deploy"
// tail. Both admin platform deploy (after load+default) and local
// cluster create (after kind+registry bring-up) call this with a
// fully-loaded+defaulted config. Keeps the deploy.Options + Getter
// construction in one place so a new flag or default reaches both.
//
// plan is the local certificate resolution plan when cfg is
// *EducatesLocalConfig, and nil for every other kind. It supplies the
// translator's certificate decision and the cached Secrets to sync, and
// is committed (its cache changes made and its notices printed) before
// the install starts.
func translateAndDeploy(
	ctx context.Context,
	w io.Writer,
	cfg v1alpha1.Config,
	plan *localcerts.Plan,
	flags deployPipelineFlags,
) error {
	opts := translator.Options{}
	if plan != nil {
		opts = plan.TranslatorOptions()
	}
	out, err := translator.Translate(cfg, opts)
	if err != nil {
		return err
	}

	cf := deployConfigFlags(flags)

	helmLog := io.Discard
	if flags.Verbose {
		helmLog = w
	}

	deployOpts := deployer.Options{
		Getter:  cf,
		Out:     w,
		HelmLog: helmLog,
		Timeout: flags.Timeout,
		// Verbose turns off the in-place line morphing: helm debug
		// streams to the same writer, so every step state change is
		// committed as its own line to interleave cleanly with that
		// detail instead of being overwritten (or corrupted).
		Progress: progress.New(w, 0, isStdoutTTY(w) && !flags.Verbose),
	}
	if plan != nil {
		if err := plan.Commit(deployOpts.Progress); err != nil {
			return err
		}
		deployOpts.SyncLocalSecrets = true
		deployOpts.LocalSecretNames = plan.SyncSecretNames
	}

	return deployer.Deploy(ctx, out, deployOpts)
}

// translateAndDiff is translateAndDeploy as a preview: it translates cfg
// as a deploy does and shows what the deploy would change in the
// cluster, changing nothing there or in the local secrets cache. plan is
// not committed: the CA key pairs it minted, which a deploy writes to the
// cache before it syncs them, are previewed from the plan itself.
func translateAndDiff(
	ctx context.Context,
	w io.Writer,
	cfg v1alpha1.Config,
	plan *localcerts.Plan,
	flags deployPipelineFlags,
) error {
	opts := translator.Options{}
	if plan != nil {
		opts = plan.TranslatorOptions()
	}
	out, err := translator.Translate(cfg, opts)
	if err != nil {
		return err
	}

	cf := deployConfigFlags(flags)

	kubeContext := flags.Context
	if kubeContext == "" {
		if raw, err := cf.ToRawKubeConfigLoader().RawConfig(); err == nil {
			kubeContext = raw.CurrentContext
		}
	}

	diffOpts := deployer.DiffOptions{
		Getter:  cf,
		Out:     w,
		Context: kubeContext,
		Verbose: flags.Verbose,
	}
	if plan != nil {
		diffOpts.Notes = append(diffOpts.Notes, plan.Notices...)
		for _, warning := range plan.Warnings {
			diffOpts.Notes = append(diffOpts.Notes, "warning: "+warning)
		}
		diffOpts.SyncLocalSecrets = true
		diffOpts.LocalSecretNames = plan.SyncSecretNames
		diffOpts.PendingSecrets = plan.PendingCAKeyPairs()
	}

	return deployer.Diff(ctx, out, diffOpts)
}

// deployConfigFlags returns the kubectl-style connection flags of a deploy
// or its preview: flags' kubeconfig and context, and the operator
// namespace.
func deployConfigFlags(flags deployPipelineFlags) *genericclioptions.ConfigFlags {
	cf := genericclioptions.NewConfigFlags(true)
	if flags.Kubeconfig != "" {
		cf.KubeConfig = &flags.Kubeconfig
	}
	if flags.Context != "" {
		cf.Context = &flags.Context
	}
	ns := deployer.OperatorNamespace
	cf.Namespace = &ns
	return cf
}

// isStdoutTTY tells the progress reporter whether to use \r-based
// in-place updates. Cmd code passes cmd.OutOrStdout() into the
// pipeline; that's *os.File when running interactively, *bytes.Buffer
// in tests.
func isStdoutTTY(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
