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

package platform

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
)

// extrasIntent is the resolved install intent for an optional extra.
// The string values intentionally match the condition Reason field
// the reconciler publishes, so consumers reading the condition see
// the same vocabulary as the spec/mode the user wrote.
type extrasIntent int

const (
	// intentInstall: install or upgrade the release.
	intentInstall extrasIntent = iota
	// intentSkip: keep the release uninstalled (Mode=Disabled, or
	// Mode=Auto and the auto signal isn't satisfied). If a release
	// exists from a prior reconcile, drain it.
	intentSkip
	// intentRefuse: the user set Mode=Enabled but a prerequisite is
	// missing. Publish a clear NotReady condition and stop reconciling
	// the extra (the main session-manager install path continues).
	intentRefuse
)

// reasonExtraInstalled is the shared condition reason for every
// install-outcome branch of the extras resolvers below.
const reasonExtraInstalled = "Installed"

// resolveNodeCATrust evaluates the tri-state mode against whether a CA
// is in effect for the session manager (effectiveIngressCARef), from
// the cluster or from the SessionManager's CA override. Returns the
// intent plus a reason+message used for the status condition the
// reconciler publishes.
func resolveNodeCATrust(obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) (extrasIntent, string, string) {
	mode := platformv1alpha1.ComponentModeAuto
	if obj.Spec.NodeCATrust != nil && obj.Spec.NodeCATrust.Mode != "" {
		mode = obj.Spec.NodeCATrust.Mode
	}
	// Only presence matters here, so the override copy's namespace is
	// irrelevant.
	hasCA := effectiveIngressCARef(obj, cfg, "") != nil
	switch mode {
	case platformv1alpha1.ComponentModeDisabled:
		return intentSkip, "ModeDisabled",
			"nodeCATrust disabled by spec"
	case platformv1alpha1.ComponentModeEnabled:
		if !hasCA {
			return intentRefuse, "NodeCATrustMissingCA",
				"nodeCATrust mode=Enabled but neither EducatesClusterConfig.status.ingress.caCertificateSecretRef nor SessionManager " + caOverrideField + " is set"
		}
		return intentInstall, reasonExtraInstalled,
			"node-ca-injector installed (mode=Enabled)"
	case platformv1alpha1.ComponentModeAuto:
		fallthrough
	default:
		if hasCA {
			return intentInstall, reasonExtraInstalled,
				"node-ca-injector installed (mode=Auto: an ingress CA is configured)"
		}
		return intentSkip, "ModeAutoNoCA",
			"nodeCATrust skipped (mode=Auto: no ingress CA is configured)"
	}
}

// resolveRemoteAccess evaluates remoteAccess mode against the
// presence of a LookupService singleton. NotFound on the lookup is
// folded into "Auto auto-skip" semantics rather than surfaced as an
// error.
func (r *SessionManagerReconciler) resolveRemoteAccess(ctx context.Context, obj *platformv1alpha1.SessionManager) (extrasIntent, string, string, error) {
	mode := platformv1alpha1.ComponentModeAuto
	if obj.Spec.RemoteAccess != nil && obj.Spec.RemoteAccess.Mode != "" {
		mode = obj.Spec.RemoteAccess.Mode
	}

	switch mode {
	case platformv1alpha1.ComponentModeDisabled:
		return intentSkip, "ModeDisabled",
			"remoteAccess disabled by spec", nil
	case platformv1alpha1.ComponentModeEnabled:
		return intentInstall, reasonExtraInstalled,
			"remote-access installed (mode=Enabled)", nil
	case platformv1alpha1.ComponentModeAuto:
		fallthrough
	default:
		hasLookup, err := r.lookupServiceExists(ctx)
		if err != nil {
			return intentSkip, "", "", err
		}
		if hasLookup {
			return intentInstall, reasonExtraInstalled,
				"remote-access installed (mode=Auto: LookupService present)", nil
		}
		return intentSkip, "ModeAutoNoLookupService",
			"remoteAccess skipped (mode=Auto: no LookupService CR)", nil
	}
}

// lookupServiceExists reports whether the singleton LookupService
// CR is present in the cluster. NotFound is the steady state when
// the user hasn't opted into cross-cluster federation; that's the
// expected signal for Auto skipping remote-access.
func (r *SessionManagerReconciler) lookupServiceExists(ctx context.Context) (bool, error) {
	ls := &platformv1alpha1.LookupService{}
	if err := r.Get(ctx, types.NamespacedName{Name: singletonName}, ls); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// extraOutcome reports, to the SessionManager reconcile loop, the result of
// reconciling one optional extra beyond the hard-error return:
//
//   - notReady is true when the extra is wanted (Install intent) but its
//     release is not actually running — a held failure or a mid-repair
//     rollback. The caller folds this into the aggregate Ready demotion,
//     mirroring an intentRefuse, so a broken extra does not report Ready.
//   - requeueAfter is non-zero when the extra is mid-repair (a rollback) and
//     the caller should requeue to drive the follow-up upgrade, since neither
//     the CR nor the component Deployment will produce a watch event for it.
type extraOutcome struct {
	notReady     bool
	requeueAfter time.Duration
}

// reconcileExtra drives one optional subchart through its resolved intent.
// Returns a hard error only when a Helm operation actually errors this pass;
// Install/Skip/Refuse otherwise count as successfully reconciled (Refuse is a
// published condition, not a reconciler error). The extraOutcome carries the
// softer "wanted but not running" / "requeue to finish repair" signals. The
// caller invokes this after the main session-manager install lands; extras
// live in the same namespace and helm releases are unique by release-name.
//
// The intent → action mapping:
//   - Install: converge the release via convergeRelease (install, upgrade
//     on drift, self-heal a failed release once its inputs change, or hold a
//     failed release whose inputs are unchanged). Publish condition: True with
//     reason `Installed` on success; False with reason `ReleaseFailed`
//     (held failure) or `RepairingRelease` (mid-repair rollback) otherwise.
//   - Skip:    if a release exists from a previous reconcile, drain it.
//     Publish condition: type, Status=False, reason from resolver. False
//     because the extra isn't running; users grep for True to confirm it's up.
//   - Refuse:  publish condition: type, Status=False, reason from resolver,
//     with a message pointing at the missing prerequisite. No release work.
func (r *SessionManagerReconciler) reconcileExtra(
	ctx context.Context,
	obj *platformv1alpha1.SessionManager,
	conditionType string,
	releaseName string,
	loadChart func() (*chart.Chart, error),
	renderValues func(sm *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig, imageNamespace string) map[string]any,
	cfg *configv1alpha1.EducatesClusterConfig,
	intent extrasIntent, reason, message string,
) (extraOutcome, error) {
	log := logf.FromContext(ctx)
	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		return extraOutcome{}, fmt.Errorf("build helm client for %s: %w", releaseName, err)
	}

	switch intent {
	case intentInstall:
		chrt, err := loadChart()
		if err != nil {
			return extraOutcome{}, fmt.Errorf("load chart %s: %w", releaseName, err)
		}
		log.V(1).Info("converging extra", "release", releaseName)
		res, err := convergeRelease(ctx, r.Client, hc, releaseName, chrt, renderValues(obj, cfg, chartImageNamespace(chrt)))
		if err != nil {
			setExtraCondition(obj, conditionType, metav1.ConditionFalse, "InstallFailed", err.Error())
			return extraOutcome{}, fmt.Errorf("ensure release %s: %w", releaseName, err)
		}
		switch res.Action {
		case helm.ActionHeldFailed:
			setExtraCondition(obj, conditionType, metav1.ConditionFalse, "ReleaseFailed",
				helm.FailureMessage(res.Release, fmt.Sprintf("%s Helm release is in a failed state", releaseName)))
			return extraOutcome{notReady: true}, nil
		case helm.ActionRepairedRollback:
			setExtraCondition(obj, conditionType, metav1.ConditionFalse, "RepairingRelease",
				fmt.Sprintf("rolled %s release back to its last deployed revision; re-applying desired configuration", releaseName))
			return extraOutcome{notReady: true, requeueAfter: 15 * time.Second}, nil
		default:
			setExtraCondition(obj, conditionType, metav1.ConditionTrue, reason, message)
			return extraOutcome{}, nil
		}

	case intentSkip:
		// Idempotent uninstall: drains any release from a prior
		// reconcile when the intent flipped from Install to Skip.
		// helm.Uninstall classifies NotFound internally, so this is
		// a no-op when nothing is there.
		if err := hc.Uninstall(releaseName); err != nil {
			setExtraCondition(obj, conditionType, metav1.ConditionFalse, "UninstallFailed", err.Error())
			return extraOutcome{}, fmt.Errorf("helm uninstall %s: %w", releaseName, err)
		}
		setExtraCondition(obj, conditionType, metav1.ConditionFalse, reason, message)
		return extraOutcome{}, nil

	case intentRefuse:
		setExtraCondition(obj, conditionType, metav1.ConditionFalse, reason, message)
		return extraOutcome{}, nil
	}
	return extraOutcome{}, nil
}

// setExtraCondition writes a condition on the obj's status without
// going through the Update path. The caller publishes status in one
// final write at the end of Reconcile.
func setExtraCondition(obj *platformv1alpha1.SessionManager, condType string, status metav1.ConditionStatus, reason, message string) {
	// Lazy import of meta would create a cycle; inline the same
	// SetStatusCondition behaviour via condition list mutation.
	for i := range obj.Status.Conditions {
		if obj.Status.Conditions[i].Type == condType {
			c := &obj.Status.Conditions[i]
			if c.Status != status {
				c.LastTransitionTime = metav1.Now()
			}
			c.Status = status
			c.Reason = reason
			c.Message = message
			c.ObservedGeneration = obj.Generation
			return
		}
	}
	obj.Status.Conditions = append(obj.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
		LastTransitionTime: metav1.Now(),
	})
}

// renderNodeCAInjectorValues maps cluster config status into the
// node-ca-injector subchart's values shape. The subchart requires
// `clusterIngress.caCertificateRef.{name,namespace}`; without those
// it template-errors. The CA is the one the session manager trusts
// (effectiveIngressCARef), since every registry Ingress the
// injector covers is created by the session manager; the resolver
// above guarantees we only call this when that CA is configured.
// imageNamespace is the chart's own image registry namespace, kept
// under the cluster config's image registry prefix.
func renderNodeCAInjectorValues(obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig, operatorNamespace, imageNamespace string) map[string]any {
	values := map[string]any{}

	if dev := developmentImageRegistryValues(cfg, imageNamespace); dev != nil {
		values["development"] = dev
	}
	if cfg.Status.ImageRegistry != nil && len(cfg.Status.ImageRegistry.PullSecrets) > 0 {
		refs := make([]any, 0, len(cfg.Status.ImageRegistry.PullSecrets))
		for _, ref := range cfg.Status.ImageRegistry.PullSecrets {
			refs = append(refs, map[string]any{"name": ref.Name})
		}
		values["imagePullSecrets"] = refs
	}

	caRef := effectiveIngressCARef(obj, cfg, operatorNamespace)
	values["clusterIngress"] = map[string]any{
		"caCertificateRef": map[string]any{
			"name":      caRef.Name,
			"namespace": caRef.Namespace,
		},
	}
	// The node-ca-injector image lives in its own subchart, so the
	// SessionManager images.overrides entry with that name is routed
	// here rather than into the session-manager chart's inventory.
	if obj.Spec.Images != nil {
		for _, o := range obj.Spec.Images.Overrides {
			if o.Name == "node-ca-injector" {
				repo, tag := splitImageRef(o.Image)
				values["image"] = map[string]any{
					"repository": repo,
					"tag":        tag,
				}
			}
		}
	}
	return values
}

// renderRemoteAccessValues maps cluster config status into the
// remote-access subchart's values shape. The subchart has no
// configurable knobs — pull secrets aren't even needed because no
// image is deployed (it's just RBAC + a token Secret). Returning an
// empty map keeps the chart on its defaults.
func renderRemoteAccessValues(_ *platformv1alpha1.SessionManager, _ *configv1alpha1.EducatesClusterConfig, _ string) map[string]any {
	return map[string]any{}
}

// uninstallExtraQuietly drops a release if it exists; used by the
// SessionManager cleanup path to drain the two optional extras
// alongside the main release on finalizer drain. Logs but does not
// propagate uninstall errors — the main release uninstall is the
// only one that must succeed cleanly, and a stale extra release
// would only block future reconciles, not break the cluster.
func (r *SessionManagerReconciler) uninstallExtraQuietly(log logr.Logger, releaseName string) {
	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		log.Error(err, "build helm client for extras cleanup", "release", releaseName)
		return
	}
	if err := hc.Uninstall(releaseName); err != nil {
		log.Error(err, "uninstall extra release", "release", releaseName)
	}
}
