// Package deployer is the v4 install path: load a CLI config, translate
// to operator chart values + four platform CRs, install the operator
// chart via Helm SDK, server-side-apply the CRs in dependency order, and
// wait for each to be Ready=True.
package deployer

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/educates/educates-training-platform/client-programs/pkg/cluster"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/apply"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/chart"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/crds"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/helm"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/wait"
	"github.com/educates/educates-training-platform/client-programs/pkg/secrets"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
)

const (
	// OperatorNamespace is where the educates-installer helm release
	// lives. Matches the chart's recommended deploy namespace and the
	// samples in installer/samples/.
	OperatorNamespace = "educates-installer"

	// OperatorReleaseName is the helm release name.
	OperatorReleaseName = "educates-installer"

	// DefaultTimeout caps the wait on each CR's Ready=True condition.
	DefaultTimeout = 5 * time.Minute
)

// Options configures a deploy run.
type Options struct {
	// Getter is the kubectl-style RESTClientGetter — typically a
	// configured genericclioptions.ConfigFlags from cobra.
	Getter genericclioptions.RESTClientGetter

	// Out is where progress lines are written. Pass cmd.OutOrStdout()
	// from cobra; pass io.Discard from tests.
	Out io.Writer

	// HelmLog receives helm SDK debug output. io.Discard by default.
	HelmLog io.Writer

	// Timeout overrides DefaultTimeout per CR wait.
	Timeout time.Duration

	// SkipCRDApply bypasses the operator CRD apply step. The default
	// is to push CRDs from the embedded chart before helm-install so a
	// CRD shape change reaches the cluster on re-deploy (helm itself
	// only installs CRDs on first install, never on upgrade). Set true
	// when the user manages CRDs out of band (GitOps, separate
	// kubectl apply, etc.).
	SkipCRDApply bool

	// SyncLocalSecrets, when true, ensures the cluster's
	// 'educates-secrets' namespace and copies the cached Secrets named in
	// LocalSecretNames into it before applying the platform CRs. Matches
	// the v3 laptop flow: cached CA/TLS material is pushed at deploy
	// time, ECC's caKeyPairSecretRef points there. Only meaningful for
	// EducatesLocalConfig deploys.
	SyncLocalSecrets bool

	// LocalSecretNames names the cached Secrets SyncLocalSecrets copies:
	// the local certificate resolution plan's SyncSecretNames.
	LocalSecretNames []string

	// Progress is the structured progress reporter. When nil, plain
	// io.Discard-backed reporter is used (no progress output, but the
	// helpers still call into it so the call-site code is uniform).
	// Cmd code typically passes progress.NewForStdout(...) to render
	// to the user's terminal with TTY-aware overwriting.
	Progress progress.Reporter
}

// Deploy executes the install pipeline against the cluster reachable
// via opts.Getter:
//
//  1. Helm install/upgrade the operator chart in the operator namespace.
//  2. Apply EducatesClusterConfig → wait Ready.
//  3. Apply SecretsManager → wait Ready.
//  4. Apply LookupService (if present) → wait Ready.
//  5. Apply SessionManager → wait Ready.
//  6. Point the loopback service of a local cluster, when it has one, at
//     the ingress domain.
//
// Returns the last LookupService/SessionManager objects observed so the
// caller can print URLs from .status.
func Deploy(ctx context.Context, out *translator.Output, opts Options) error {
	if opts.Out == nil {
		opts.Out = io.Discard
	}
	if opts.HelmLog == nil {
		opts.HelmLog = io.Discard
	}
	if opts.Timeout == 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.Progress == nil {
		opts.Progress = progress.New(io.Discard, 0, false)
	}

	// Push cached local secrets so the operator can consume them.
	if opts.SyncLocalSecrets {
		step := opts.Progress.Start("syncing cached local secrets to cluster")
		if err := syncLocalSecrets(opts.Getter, opts.LocalSecretNames); err != nil {
			step.Fail(err)
			return err
		}
		step.Done("")
	}

	chrt, err := chart.Load()
	if err != nil {
		return fmt.Errorf("load embedded chart: %w", err)
	}
	// Applier needs to exist before the CRD step; hoist before helm.
	applier, err := apply.New(opts.Getter)
	if err != nil {
		return err
	}
	waiter, err := wait.New(opts.Getter)
	if err != nil {
		return err
	}

	// 1. CRDs.
	if !opts.SkipCRDApply {
		step := opts.Progress.Start("apply CRDs")
		applied, err := crds.Apply(ctx, applier, chrt)
		if err != nil {
			step.Fail(err)
			return err
		}
		step.Done(fmt.Sprintf("%d applied", len(applied)))
	}

	// 2. Helm install/upgrade the operator chart.
	step := opts.Progress.Start("Installing Educates Kubernetes Operator")
	helmClient, err := helm.New(opts.Getter, OperatorNamespace, opts.HelmLog)
	if err != nil {
		step.Fail(err)
		return err
	}
	if _, err := helmClient.UpgradeOrInstall(ctx, OperatorReleaseName, chrt, out.OperatorChartValues); err != nil {
		step.Fail(err)
		return err
	}
	step.Done("")

	// 3. Apply EducatesClusterConfig + wait.
	ecc, err := installStep(ctx, opts, applier, waiter, out.EducatesClusterConfig, "EducatesClusterConfig")
	if err != nil {
		return err
	}

	// 4. SecretsManager + wait.
	if _, err := installStep(ctx, opts, applier, waiter, out.SecretsManager, "SecretsManager"); err != nil {
		return err
	}

	// 5. SessionManager + LookupService. There's a Ready cycle between
	//    them: LookupService's pod mounts a remote-access-token Secret that
	//    SessionManager's remote-access install creates, and in Auto mode
	//    SessionManager installs remote-access only when a LookupService CR
	//    is present. Both CRs are therefore applied before either is waited
	//    on, then waited on concurrently. The order between the two applies
	//    is not load-bearing — the SessionManager reconciler watches
	//    LookupService and re-evaluates the Auto decision when one appears
	//    — so they go SessionManager-first to read top-to-bottom in the
	//    progress block. A shared progress group renders both installs side
	//    by side, each line morphing with its own status.phase, so the user
	//    sees them progress in parallel — one may go Ready while the other
	//    is still reconciling — instead of one appearing to finish before
	//    the other starts.
	type installTarget struct {
		obj   map[string]interface{}
		label string
	}
	targets := make([]installTarget, 0, 2)
	targets = append(targets, installTarget{out.SessionManager, "SessionManager"})
	if out.LookupService != nil {
		targets = append(targets, installTarget{out.LookupService, "LookupService"})
	}

	// Apply all before waiting on any (remote-access-token cycle).
	objs := make([]*unstructured.Unstructured, len(targets))
	for i, t := range targets {
		u, err := mapToUnstructured(t.obj)
		if err != nil {
			return fmt.Errorf("%s: %w", t.label, err)
		}
		if _, err := applier.Apply(ctx, u); err != nil {
			return fmt.Errorf("apply %s: %w", t.label, err)
		}
		objs[i] = u
	}

	// Wait on all concurrently, each reporting to its own line in the group.
	labels := make([]string, len(targets))
	for i, t := range targets {
		labels[i] = "Installing " + t.label
	}
	steps := opts.Progress.StartConcurrent(labels...)

	var wg sync.WaitGroup
	errs := make([]error, len(targets))
	for i := range targets {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			u, step := objs[i], steps[i]
			if _, err := waiter.WaitReadyWithPhase(ctx, u.GroupVersionKind(), u.GetNamespace(), u.GetName(), opts.Timeout, step.Update); err != nil {
				step.Fail(err)
				errs[i] = err
				return
			}
			step.Done("")
		}(i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}

	// 6. Point the loopback service of a local cluster at the ingress
	//    domain the EducatesClusterConfig publishes.
	refreshLoopbackService(ctx, opts, ecc)

	opts.Progress.Note("Educates successfully deployed")
	return nil
}

// installStep applies obj and waits for it to become Ready under a
// single "Installing <label>" progress step, returning the object as
// last observed. The apply is surfaced as an "applying" phase and the
// wait surfaces the CR's status.phase changes, so one morphing line
// carries the whole install of one component. Used by ECC and
// SecretsManager, where apply and wait are strictly sequential.
func installStep(ctx context.Context, opts Options, applier *apply.Client, waiter *wait.Client, obj map[string]interface{}, label string) (*unstructured.Unstructured, error) {
	u, err := mapToUnstructured(obj)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", label, err)
	}
	step := opts.Progress.Start("Installing " + label)
	step.Update("applying")
	if _, err := applier.Apply(ctx, u); err != nil {
		err = explainApplyRejection(err)
		step.Fail(err)
		return nil, err
	}
	ready, err := waiter.WaitReadyWithPhase(ctx, u.GroupVersionKind(), u.GetNamespace(), u.GetName(), opts.Timeout, step.Update)
	if err != nil {
		step.Fail(err)
		return nil, err
	}
	step.Done("")
	return ready, nil
}

// refreshLoopbackService points the loopback service, which `educates
// local cluster create` creates for `educates serve-workshop`, at the
// ingress domain the ready EducatesClusterConfig publishes in
// status.ingress.domain. The service is otherwise fixed when the cluster
// is created, so a deploy with another domain, or with the nip.io domain
// of a listen address that has since changed, would leave it
// resolving the old one. A cluster without the service is left alone.
// Educates is installed by the time this runs, so a failure is reported
// as a warning and does not fail the deploy.
func refreshLoopbackService(ctx context.Context, opts Options, ecc *unstructured.Unstructured) {
	domain, _, _ := unstructured.NestedString(ecc.Object, "status", "ingress", "domain")
	if domain == "" {
		return
	}
	changed, err := func() (bool, error) {
		cfg, err := opts.Getter.ToRESTConfig()
		if err != nil {
			return false, fmt.Errorf("REST config: %w", err)
		}
		cs, err := kubernetes.NewForConfig(cfg)
		if err != nil {
			return false, fmt.Errorf("kubernetes client: %w", err)
		}
		return cluster.RefreshLoopbackService(ctx, cs, domain)
	}()
	switch {
	case err != nil:
		opts.Progress.Note(fmt.Sprintf("warning: loopback service for 'educates serve-workshop' not refreshed: %v", err))
	case changed:
		opts.Progress.Note(fmt.Sprintf("loopback service for 'educates serve-workshop' now resolves localhost.%s", domain))
	}
}

// providerImmutableRules pairs the start of each message the
// EducatesClusterConfig CRD's validation rules report, when an update
// changes how a cluster service is provided, with the setting the CLI
// names in its explanation.
var providerImmutableRules = []struct {
	messagePrefix string
	setting       string
}{
	{"spec.ingress.certificates.provider is immutable", "certificate provider"},
	{"spec.ingress.controller.provider is immutable", "ingress controller provider"},
	{"spec.dns.provider is immutable", "DNS provider"},
	{"spec.policyEnforcement engines and kyverno provider are immutable", "policy enforcement"},
}

// explainApplyRejection adds the remedy to an API server rejection the
// user can only resolve outside a redeploy. How each cluster service of
// an existing install is provided is immutable, so a deploy that would
// change it, such as toggling ingress.insecure on a local cluster,
// needs the platform deleted or the cluster recreated first. Any other
// error is returned unchanged.
func explainApplyRejection(err error) error {
	if !apierrors.IsInvalid(err) {
		return err
	}
	var settings []string
	for _, rule := range providerImmutableRules {
		if strings.Contains(err.Error(), rule.messagePrefix) {
			settings = append(settings, rule.setting)
		}
	}
	if len(settings) == 0 {
		return err
	}
	settingsPhrase := settings[len(settings)-1]
	if len(settings) > 1 {
		settingsPhrase = strings.Join(settings[:len(settings)-1], ", ") + " and " + settingsPhrase
	}
	return fmt.Errorf("the %s of an existing Educates install cannot change; "+
		"run 'educates admin platform delete' and deploy again, or recreate the cluster: %w", settingsPhrase, err)
}

// syncLocalSecrets copies the named cached Secrets into the cluster's
// 'educates-secrets' namespace. Reuses the secrets package so the
// laptop flow stays the same.
func syncLocalSecrets(getter genericclioptions.RESTClientGetter, names []string) error {
	cfg, err := getter.ToRESTConfig()
	if err != nil {
		return fmt.Errorf("REST config for secrets sync: %w", err)
	}
	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return fmt.Errorf("kubernetes client for secrets sync: %w", err)
	}
	if err := secrets.SyncLocalCachedSecretsToCluster(cs, names); err != nil {
		return fmt.Errorf("sync local secrets: %w", err)
	}
	return nil
}

// mapToUnstructured roundtrips a translator-produced CR map through JSON
// to get an *unstructured.Unstructured. The translator's maps are already
// JSON-shaped (string keys, no yaml-v2 interface{} maps) since they're
// built with map[string]interface{} literals.
func mapToUnstructured(m map[string]interface{}) (*unstructured.Unstructured, error) {
	raw, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	u := &unstructured.Unstructured{}
	if err := u.UnmarshalJSON(raw); err != nil {
		return nil, fmt.Errorf("unmarshal: %w", err)
	}
	if u.GroupVersionKind() == (schema.GroupVersionKind{}) {
		return nil, fmt.Errorf("missing apiVersion/kind")
	}
	return u, nil
}
