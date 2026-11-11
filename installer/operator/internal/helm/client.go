/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package helm wraps Helm SDK v4's action package with a small, opinionated
// surface tailored to the operator's needs: install/upgrade/uninstall/status
// keyed by release name, with chart bytes loaded from vendored tarballs
// (see installer/operator/vendored-charts/) rather than pulled at runtime.
//
// The wrapper exists so reconcilers don't have to repeat the
// action.Configuration boilerplate, and so test fixtures can swap in an
// in-memory release store without touching production call sites.
package helm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"time"

	"helm.sh/helm/v4/pkg/action"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/kube"
	releasecommon "helm.sh/helm/v4/pkg/release/common"
	release "helm.sh/helm/v4/pkg/release/v1"
	"helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/client-go/rest"
)

// helmDriver selects Helm's release-record storage backend. Secrets is
// what the helm CLI defaults to in v3+, and what an operator running
// in-cluster should use so that `helm list` from a kubectl shell sees
// releases the operator created.
const helmDriver = "secrets"

// actionTimeout bounds every Install/Upgrade/Uninstall/Rollback call.
// Because all four run with HookOnlyStrategy (below), this governs only
// how long Helm blocks on a chart's hooks — general resource readiness
// is enforced by the operator's own reconcile loop, not by Helm. We set
// it explicitly rather than inheriting Helm v4's implicit fallback
// (kube.DefaultStatusWatcherTimeout, 30s): that default is undocumented
// at the call site and too tight for the slower hooks the vendored
// charts ship (Kyverno's pre-delete webhook cleanup and scale-to-zero,
// its post-upgrade resource migration). Helm v4's Uninstall and Rollback
// take no context, so this timeout is their only bound; Install/Upgrade
// use RunWithContext and are additionally cancelled if the reconcile
// context is.
const actionTimeout = 5 * time.Minute

// ErrReleaseNotFound is returned by Status when the release is absent
// from the configured driver. Wrapping helm's own driver.ErrReleaseNotFound
// gives callers a stable sentinel without leaking the helm storage type.
var ErrReleaseNotFound = errors.New("helm release not found")

// Client is the operator-facing handle for Helm SDK actions against a
// single namespace. Construct with NewClient (production) or
// NewMemoryClient (tests).
type Client struct {
	cfg       *action.Configuration
	namespace string

	// skipCRDs, when true, instructs the underlying Install action
	// to bypass the special-case CRD-install code path in Helm. Set
	// only by NewMemoryClient: kubefake.PrintingKubeClient.Build()
	// returns an empty resource list, and Helm's installCRDs guards
	// against that with a "resources are empty" hard error. Charts
	// that ship CRDs in the special `crds/` directory (e.g.,
	// external-dns) would otherwise be uninstallable through the
	// memory client. Production NewClient uses the real Kubernetes
	// KubeClient, which parses YAML correctly, so this stays false
	// there; a caller that applies a chart's CRDs itself skips them
	// per install with SkipCRDs.
	skipCRDs bool
}

// InstallOption adjusts one Install, or every install an EnsureRelease
// call runs.
type InstallOption func(*installOptions)

type installOptions struct {
	skipCRDs bool
}

// SkipCRDs keeps Helm from installing the chart's crds/ directory, for a
// caller that applies those CRDs itself (see crds.ApplyChart). Helm creates
// them with a server-side apply that does not force conflicts, so once the
// caller owns their fields, any value the API server normalises on storage,
// such as a printer column's priority: 0, conflicts and fails the install.
func SkipCRDs() InstallOption {
	return func(o *installOptions) { o.skipCRDs = true }
}

// NewClient builds a Client backed by the cluster reachable via cfg.
// Releases are stored as Secrets in the given namespace, matching the
// Helm CLI default. The namespace also scopes resource installs in the
// absence of explicit metadata.namespace.
func NewClient(cfg *rest.Config, namespace string) (*Client, error) {
	if cfg == nil {
		return nil, errors.New("rest.Config is required")
	}
	if namespace == "" {
		return nil, errors.New("namespace is required")
	}

	actionCfg := new(action.Configuration)
	getter := newRESTClientGetter(cfg, namespace)
	if err := actionCfg.Init(getter, namespace, helmDriver); err != nil {
		return nil, fmt.Errorf("init helm action config: %w", err)
	}
	// Pipe helm SDK's internal slog output to the operator pod's
	// stderr at Debug level. controller-runtime captures the pod's
	// stderr alongside its own logs. Helm logs critical paths at
	// Debug — including the per-resource error detail Uninstall
	// collapses into the opaque "failed to delete release: <name>"
	// return value. Without this, a botched uninstall is invisible.
	actionCfg.SetLogger(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))
	return &Client{cfg: actionCfg, namespace: namespace}, nil
}

// Install creates a new release with the given name from chrt. Returns
// the resulting release record. The reconciler is responsible for
// idempotency — Install will fail if the release already exists; call
// Status first to disambiguate "first install" from "upgrade".
func (c *Client) Install(ctx context.Context, releaseName string, chrt *chart.Chart, vals map[string]any, opts ...InstallOption) (*release.Release, error) {
	o := installOptions{skipCRDs: c.skipCRDs}
	for _, opt := range opts {
		opt(&o)
	}

	act := action.NewInstall(c.cfg)
	act.ReleaseName = releaseName
	act.Namespace = c.namespace
	act.CreateNamespace = false              // operator manages cluster-service namespaces explicitly elsewhere
	act.WaitStrategy = kube.HookOnlyStrategy // readiness is enforced by the reconciler, not Helm
	act.Timeout = actionTimeout
	act.SkipCRDs = o.skipCRDs

	rel, err := act.RunWithContext(ctx, chrt, vals)
	if err != nil {
		return nil, fmt.Errorf("helm install %q: %w", releaseName, err)
	}
	r, ok := rel.(*release.Release)
	if !ok {
		return nil, fmt.Errorf("helm install %q: unexpected release type %T", releaseName, rel)
	}
	return r, nil
}

// Upgrade applies an updated chart or values to an existing release. If
// the release does not exist this returns an error rather than installing;
// reconcilers should call Status first and route to Install on absence.
func (c *Client) Upgrade(ctx context.Context, releaseName string, chrt *chart.Chart, vals map[string]any) (*release.Release, error) {
	act := action.NewUpgrade(c.cfg)
	act.Namespace = c.namespace
	act.WaitStrategy = kube.HookOnlyStrategy
	act.Timeout = actionTimeout
	// vals is always the release's complete desired values. Without
	// ResetValues, Helm copies the previous release's values over empty
	// ones, so a release whose values become empty would keep its old
	// settings and never match what the operator renders.
	act.ResetValues = true

	rel, err := act.RunWithContext(ctx, releaseName, chrt, vals)
	if err != nil {
		return nil, fmt.Errorf("helm upgrade %q: %w", releaseName, err)
	}
	r, ok := rel.(*release.Release)
	if !ok {
		return nil, fmt.Errorf("helm upgrade %q: unexpected release type %T", releaseName, rel)
	}
	return r, nil
}

// Uninstall removes the named release. Idempotent: if the release does
// not exist, this returns nil (the operator's finalizer path retries on
// drift, and "already gone" is the desired terminal state).
//
// WaitStrategy is required by Helm v4 even for uninstall — leaving it
// unset returns "wait strategy not set" rather than defaulting. We pick
// HookOnlyStrategy to match Install/Upgrade: readiness is enforced by
// the operator's own reconcile loop (it polls Deployment availability
// and Certificate readiness), not by Helm blocking the action call.
func (c *Client) Uninstall(releaseName string) error {
	act := action.NewUninstall(c.cfg)
	act.IgnoreNotFound = true
	act.WaitStrategy = kube.HookOnlyStrategy
	act.Timeout = actionTimeout

	if _, err := act.Run(releaseName); err != nil {
		return fmt.Errorf("helm uninstall %q: %w", releaseName, err)
	}
	return nil
}

// Rollback reverts the named release to the given revision. The revision
// must be one Helm recorded as deployed/superseded (see
// LastDeployedRevision); passing 0 lets Helm pick the immediately previous
// revision, which may be a failed one, so callers should pass an explicit
// target. Used to recover a lock-stuck pending release without deleting its
// resources. WaitStrategy mirrors Install/Upgrade (readiness is the
// operator's concern); CleanupOnFail removes any resources a partial
// rollback created.
func (c *Client) Rollback(releaseName string, revision int) error {
	act := action.NewRollback(c.cfg)
	act.Version = revision
	act.WaitStrategy = kube.HookOnlyStrategy
	act.Timeout = actionTimeout
	act.CleanupOnFail = true

	if err := act.Run(releaseName); err != nil {
		return fmt.Errorf("helm rollback %q to revision %d: %w", releaseName, revision, err)
	}
	return nil
}

// LastDeployedRevision returns the highest revision number of releaseName
// that reached a deployed state (deployed or superseded), and whether one
// exists. Helm's Upgrade diffs against the last deployed revision and errors
// with "has no deployed releases" when none exists; the boolean lets callers
// pick upgrade/rollback (a good revision exists) over uninstall+reinstall
// (none does). Returns (0, false, nil) when the release is absent.
func (c *Client) LastDeployedRevision(releaseName string) (int, bool, error) {
	hist, err := action.NewHistory(c.cfg).Run(releaseName)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("helm history %q: %w", releaseName, err)
	}

	best, found := 0, false
	for _, item := range hist {
		rel, ok := item.(*release.Release)
		if !ok || rel.Info == nil {
			continue
		}
		if rel.Info.Status == releasecommon.StatusDeployed ||
			rel.Info.Status == releasecommon.StatusSuperseded {
			if rel.Version > best {
				best, found = rel.Version, true
			}
		}
	}
	return best, found, nil
}

// Status returns the latest release record for releaseName, or
// ErrReleaseNotFound when no release exists. The Releaser interface is
// downcast to *release.Release because the operator only deals in v1
// releases; the v2 path is internal Helm work-in-progress.
func (c *Client) Status(releaseName string) (*release.Release, error) {
	act := action.NewStatus(c.cfg)
	rel, err := act.Run(releaseName)
	if err != nil {
		if errors.Is(err, driver.ErrReleaseNotFound) {
			return nil, ErrReleaseNotFound
		}
		return nil, fmt.Errorf("helm status %q: %w", releaseName, err)
	}
	r, ok := rel.(*release.Release)
	if !ok {
		return nil, fmt.Errorf("helm status %q: unexpected release type %T", releaseName, rel)
	}
	return r, nil
}
