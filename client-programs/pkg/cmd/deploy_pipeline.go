package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

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

	// Before the plan writes to the local secrets cache, or anything
	// reaches the cluster.
	if _, err := reachDeployCluster(cf, flags); err != nil {
		return err
	}

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

	kubeContext, err := reachDeployCluster(cf, flags)
	if err != nil {
		return err
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

// clusterReachTimeout bounds the request that checks a deploy can reach
// its cluster.
const clusterReachTimeout = 15 * time.Second

// reachDeployCluster returns the name of the kube context a deploy or its
// preview reaches its cluster through, with cf and flags, once the
// cluster has answered, or why it can't: the kubeconfig selects no kube
// context, as when it has no current context and --context names none,
// it lacks the context named, or the cluster's API server does not
// answer. It runs before anything reaches the cluster, so that the user
// reads which context and server are wrong rather than the first request
// that fails, which, with no context, goes to client-go's default
// server, localhost:8080.
func reachDeployCluster(cf *genericclioptions.ConfigFlags, flags deployPipelineFlags) (string, error) {
	raw, err := cf.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return "", fmt.Errorf("unable to load the kubeconfig: %w", err)
	}

	name := flags.Context
	if name == "" {
		name = raw.CurrentContext
	}
	switch {
	case name != "":
		if _, ok := raw.Contexts[name]; !ok {
			return "", fmt.Errorf("kube context %q is not in the kubeconfig, which holds %s", name, kubeContextChoices(raw))
		}
	case inCluster():
		// In a pod without a kubeconfig context, client-go reaches the
		// cluster through the pod's service account.
		name = "(in-cluster service account)"
	default:
		return "", fmt.Errorf("no kube context is selected to deploy to: the kubeconfig sets no current context, and holds %s. "+
			"Pass --context to name one, or --local-cluster to deploy to a local cluster", kubeContextChoices(raw))
	}

	cfg, err := cf.ToRESTConfig()
	if err != nil {
		return "", fmt.Errorf("kube context %q: %w", name, err)
	}
	cfg = rest.CopyConfig(cfg)
	cfg.Timeout = clusterReachTimeout
	dc, err := discovery.NewDiscoveryClientForConfig(cfg)
	if err != nil {
		return "", fmt.Errorf("kube context %q: %w", name, err)
	}
	if _, err := dc.ServerVersion(); err != nil {
		// The URL error repeats the server's address; its cause says why.
		var urlErr *url.Error
		if errors.As(err, &urlErr) {
			err = urlErr.Err
		}
		return "", fmt.Errorf("unable to reach the cluster of kube context %q at %s: %w", name, cfg.Host, err)
	}

	return name, nil
}

// inCluster reports whether the CLI runs in a pod whose service account
// client-go can reach the cluster through.
func inCluster() bool {
	_, err := rest.InClusterConfig()
	return err == nil
}

// kubeContextChoices names the contexts the kubeconfig raw holds, to
// choose from in an error, as "the contexts a and b", "the context a" or
// "no context".
func kubeContextChoices(raw clientcmdapi.Config) string {
	names := make([]string, 0, len(raw.Contexts))
	for name := range raw.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	switch len(names) {
	case 0:
		return "no context"
	case 1:
		return "the context " + names[0]
	default:
		return fmt.Sprintf("the contexts %s and %s", strings.Join(names[:len(names)-1], ", "), names[len(names)-1])
	}
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
