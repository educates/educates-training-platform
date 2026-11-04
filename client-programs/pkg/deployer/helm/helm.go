// Package helm wraps the helm SDK with the small surface the CLI needs to
// install or upgrade the educates-installer chart in-process.
//
// It mirrors the operator's helm wrapper at installer/operator/internal/helm
// (which is not importable from this module — Go internal/ rule). The
// CLI-side variant differs by accepting a genericclioptions.RESTClientGetter
// directly (kubectl-style kubeconfig discovery) rather than a pre-built
// *rest.Config (which is what the operator has from controller-runtime).
package helm

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"helm.sh/helm/v4/pkg/action"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	release "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/cli-runtime/pkg/genericclioptions"
)

// helmDriver = "secrets" matches the helm CLI default. Releases the CLI
// creates are visible to `helm list` and vice versa.
const helmDriver = "secrets"

// ErrReleaseNotFound is the stable sentinel for "no release with that name".
var ErrReleaseNotFound = errors.New("helm release not found")

// Client is a small helm action wrapper scoped to one namespace.
type Client struct {
	cfg       *action.Configuration
	namespace string
}

// New builds a Client from a kubectl-style RESTClientGetter. Pass the
// genericclioptions.ConfigFlags the cobra command parsed (--kubeconfig,
// --context, --namespace) directly — the SDK consumes the same interface
// the helm CLI does.
//
// logOut receives helm SDK debug output. The CLI usually wants this
// suppressed in production but visible with --verbose; callers pass
// io.Discard or os.Stderr accordingly.
func New(getter genericclioptions.RESTClientGetter, namespace string, logOut io.Writer) (*Client, error) {
	if namespace == "" {
		return nil, errors.New("namespace is required")
	}
	if logOut == nil {
		logOut = io.Discard
	}
	cfg := new(action.Configuration)
	if err := cfg.Init(getter, namespace, helmDriver); err != nil {
		return nil, fmt.Errorf("init helm action config: %w", err)
	}
	cfg.SetLogger(slog.NewTextHandler(logOut, &slog.HandlerOptions{Level: slog.LevelDebug}))
	return &Client{cfg: cfg, namespace: namespace}, nil
}

// UpgradeOrInstall is the idempotent variant: install if no release of
// that name exists, otherwise upgrade. The CLI's deploy command calls
// this; reruns of `educates admin platform deploy` should converge.
func (c *Client) UpgradeOrInstall(ctx context.Context, releaseName string, chrt *chart.Chart, vals map[string]any) (*release.Release, error) {
	existing, err := c.status(releaseName)
	if err != nil && !errors.Is(err, ErrReleaseNotFound) {
		return nil, err
	}
	if existing == nil {
		return c.install(ctx, releaseName, chrt, vals)
	}
	return c.upgrade(ctx, releaseName, chrt, vals)
}

func (c *Client) install(ctx context.Context, name string, chrt *chart.Chart, vals map[string]any) (*release.Release, error) {
	act := action.NewInstall(c.cfg)
	act.ReleaseName = name
	act.Namespace = c.namespace
	act.CreateNamespace = true
	act.WaitStrategy = kube.HookOnlyStrategy
	// CRD lifecycle is owned by pkg/deployer/crds (applied separately
	// before this install so the same path runs on first-install and
	// on every subsequent re-deploy — helm's default would only install
	// CRDs on first install).
	act.SkipCRDs = true

	rel, err := act.RunWithContext(ctx, chrt, vals)
	if err != nil {
		return nil, fmt.Errorf("helm install %q: %w", name, err)
	}
	r, ok := rel.(*release.Release)
	if !ok {
		return nil, fmt.Errorf("helm install %q: unexpected release type %T", name, rel)
	}
	return r, nil
}

func (c *Client) upgrade(ctx context.Context, name string, chrt *chart.Chart, vals map[string]any) (*release.Release, error) {
	act := action.NewUpgrade(c.cfg)
	act.Namespace = c.namespace
	act.WaitStrategy = kube.HookOnlyStrategy
	// ForceConflicts lets a re-deploy steal field ownership from
	// 'kubectl edit'/'kubectl patch' (which claim ownership under
	// the 'kubectl-edit'/'kubectl-patch' field managers). Without
	// this, any user-side manual edit poisons the next deploy with
	// an SSA conflict on the edited field. The CLI's deploy is the
	// source of truth for chart-managed fields.
	act.ForceConflicts = true

	rel, err := act.RunWithContext(ctx, name, chrt, vals)
	if err != nil {
		return nil, fmt.Errorf("helm upgrade %q: %w", name, err)
	}
	r, ok := rel.(*release.Release)
	if !ok {
		return nil, fmt.Errorf("helm upgrade %q: unexpected release type %T", name, rel)
	}
	return r, nil
}

// Uninstall removes the named release. Idempotent: missing → nil.
func (c *Client) Uninstall(name string) error {
	act := action.NewUninstall(c.cfg)
	act.IgnoreNotFound = true
	act.WaitStrategy = kube.HookOnlyStrategy
	if _, err := act.Run(name); err != nil {
		return fmt.Errorf("helm uninstall %q: %w", name, err)
	}
	return nil
}

func (c *Client) status(name string) (*release.Release, error) {
	rel, err := action.NewStatus(c.cfg).Run(name)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, ErrReleaseNotFound
		}
		return nil, fmt.Errorf("helm status %q: %w", name, err)
	}
	r, ok := rel.(*release.Release)
	if !ok {
		return nil, fmt.Errorf("helm status %q: unexpected release type %T", name, rel)
	}
	return r, nil
}
