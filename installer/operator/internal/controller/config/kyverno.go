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

package config

import (
	"context"
	"errors"
	"fmt"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	"github.com/educates/educates-training-platform/installer/operator/internal/kyverno"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// Kyverno install constants. Same shape as the other cluster
// services: dedicated namespace, helm release named after the
// chart, four Deployments to gate readiness on. Workload names
// derive from the chart's `kyverno.name` template (defaults to
// chart name "kyverno") plus the per-component suffix.
const (
	kyvernoNamespace   = "kyverno"
	kyvernoReleaseName = "kyverno"
)

// kyvernoDeployments are the four Deployments the chart installs at
// default values (admission, background, cleanup, reports
// controllers — reports-server is opt-in and not enabled). Readiness
// is gated on all four reporting Available=True.
var kyvernoDeployments = []string{
	"kyverno-admission-controller",
	"kyverno-background-controller",
	"kyverno-cleanup-controller",
	"kyverno-reports-controller",
}

// errKyvernoNotReady is the in-flight sentinel ensureKyvernoReady
// returns. Same shape as the other service-readiness sentinels.
var errKyvernoNotReady = errors.New("kyverno Deployments not yet Available")

// reconcileKyvernoPhase runs the Kyverno install pipeline:
//
//  1. helm install/upgrade from the vendored chart.
//  2. Wait for the four kyverno Deployments to report Available.
//
// Kyverno installs validating + mutating admission webhooks via the
// chart, with the cainjector-equivalent built into the
// admission-controller itself (it self-signs and injects its own
// caBundle on startup). Unlike cert-manager, the operator does NOT
// create any kyverno.io custom resources during install (policies
// are created elsewhere, by the session-manager when workshops
// deploy), so there is no admission-webhook race to classify on
// our SSA path — Kyverno's webhooks only act on resources that
// match installed policies, of which there are none at this stage.
//
// CRDs land in templates/ inside subcharts (charts/kyverno-api),
// not the special crds/ directory, so the in-memory helm test
// fake handles them without the SkipCRDs work-around.
//
// Whichever provider supplies Kyverno, the phase then checks that the
// cluster serves the Kyverno policy kinds Educates creates, and holds
// PolicyEnforcementReady and Ready back until it does: an External
// Kyverno is the administrator's to install, and a Bundled one's CRDs
// may not be established yet. Nothing the operator watches changes when
// Kyverno appears, so the phase polls.
//
// When neither policy engine is Kyverno, the phase returns done=true
// without looking for Kyverno.
func (r *EducatesClusterConfigReconciler) reconcileKyvernoPhase(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) (bool, ctrl.Result, error) {
	pe := obj.Spec.PolicyEnforcement
	if pe == nil || !usesKyverno(pe) {
		r.markPolicyEnforcementReadyTrue(obj, "NotRequired", "no policy engine is Kyverno")
		return true, ctrl.Result{}, nil
	}

	bundled := shouldInstallKyverno(obj)
	if bundled {
		if done, res, err := r.installBundledKyverno(ctx, obj); !done {
			return false, res, err
		}
	}

	missing, err := kyverno.MissingPolicyKinds(r.KyvernoDiscovery)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	if len(missing) > 0 {
		r.markKyvernoNotFound(obj, kyverno.NotServedMessage(missing))
		phase := configv1alpha1.ClusterConfigPhaseInstalling
		if !bundled {
			// Only the administrator can install an External Kyverno.
			phase = configv1alpha1.ClusterConfigPhaseDegraded
		}
		r.markManagedPhase(obj, phase)
		return false, ctrl.Result{RequeueAfter: kyverno.PresencePollInterval}, r.updateStatusWithTransitionLog(ctx, obj)
	}

	if bundled {
		r.markPolicyEnforcementReadyTrue(obj, "BundledKyvernoReady", "Bundled Kyverno is Ready")
	} else {
		r.markPolicyEnforcementReadyTrue(obj, "ExternalKyvernoReady", "External Kyverno serves the policy kinds Educates creates")
	}
	return true, ctrl.Result{}, nil
}

// installBundledKyverno installs or upgrades the bundled Kyverno release
// and waits for its four controllers. It returns done=true once they are
// Available.
func (r *EducatesClusterConfigReconciler) installBundledKyverno(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) (bool, ctrl.Result, error) {
	log := logf.FromContext(ctx)
	phaseStop := func(res ctrl.Result, err error) (bool, ctrl.Result, error) {
		return false, res, err
	}

	res, err := r.reconcileKyverno(ctx, obj)
	if err != nil {
		log.Error(err, "kyverno reconcile failed")
		r.markPolicyEnforcementProgressing(obj, "InstallFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, obj)
		return phaseStop(ctrl.Result{}, err)
	}

	if proceed, result, err := r.handleManagedReleaseResult(ctx, obj, "kyverno", res, r.markPolicyEnforcementProgressing); !proceed {
		return false, result, err
	}

	if err := r.ensureKyvernoReady(ctx); err != nil {
		if errors.Is(err, errKyvernoNotReady) {
			r.markPolicyEnforcementProgressing(obj, "WaitingForKyverno", "kyverno Deployments not yet Available")
			r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
			// Same cache-vs-watch race mitigation as the other
			// service-readiness gates.
			return false, ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateStatusWithTransitionLog(ctx, obj)
		}
		return phaseStop(ctrl.Result{}, err)
	}
	return true, ctrl.Result{}, nil
}

// shouldInstallKyverno reports whether the operator is responsible
// for installing Kyverno. Two conditions:
//
//   - At least one of the cluster/workshop policy engines resolves
//     to Kyverno. If both engines are some other value (None,
//     PodSecurityStandards, OpenShiftSCC), there's no need for
//     Kyverno at all.
//   - The Kyverno sourcing block specifies Bundled provider.
//     External sourcing means a user-supplied Kyverno install is
//     expected; we just consume it.
func shouldInstallKyverno(obj *configv1alpha1.EducatesClusterConfig) bool {
	pe := obj.Spec.PolicyEnforcement
	if pe == nil || !usesKyverno(pe) || pe.Kyverno == nil {
		return false
	}
	return pe.Kyverno.Provider == configv1alpha1.KyvernoProviderBundled
}

// usesKyverno reports whether either policy engine is Kyverno.
func usesKyverno(pe *configv1alpha1.PolicyEnforcement) bool {
	return pe.ClusterPolicy.Engine == configv1alpha1.ClusterPolicyEngineKyverno ||
		pe.WorkshopPolicy.Engine == configv1alpha1.WorkshopPolicyEngineKyverno
}

// inlineUsesKyverno reports whether either declared Inline engine is
// Kyverno.
func inlineUsesKyverno(pe configv1alpha1.InlinePolicyEnforcement) bool {
	return pe.ClusterPolicyEngine == configv1alpha1.ClusterPolicyEngineKyverno ||
		pe.WorkshopPolicyEngine == configv1alpha1.WorkshopPolicyEngineKyverno
}

// reconcileKyverno performs the helm install/upgrade. Mirrors the
// cert-manager / Contour / external-dns shape.
func (r *EducatesClusterConfigReconciler) reconcileKyverno(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig) (helm.Result, error) {
	chrt, err := vendoredcharts.Kyverno()
	if err != nil {
		return helm.Result{}, fmt.Errorf("load embedded kyverno chart: %w", err)
	}

	if err := r.ensureNamespace(ctx, kyvernoNamespace, owner, nil); err != nil {
		return helm.Result{}, err
	}
	if err := r.ensureImagePullSecretCopies(ctx, owner, kyvernoNamespace); err != nil {
		return helm.Result{}, err
	}

	hc, err := r.HelmClientFor(kyvernoNamespace)
	if err != nil {
		return helm.Result{}, fmt.Errorf("build helm client for %q: %w", kyvernoNamespace, err)
	}

	res, err := hc.EnsureRelease(ctx, kyvernoReleaseName, chrt, renderKyvernoValues(owner))
	if err != nil {
		return helm.Result{}, err
	}

	if owner.Status.BundledChartVersions == nil {
		owner.Status.BundledChartVersions = map[string]string{}
	}
	owner.Status.BundledChartVersions["kyverno"] = vendoredcharts.KyvernoChartVersion
	return res, nil
}

// renderKyvernoValues builds the values map. v1alpha1 is minimal —
// just plumbing the image-registry prefix; everything else uses
// chart defaults (4 controllers enabled, reports-server disabled,
// default resource limits and replica counts). The chart surface is
// large and we deliberately don't expose more until concrete needs
// emerge — in particular no replica override: Kyverno scales
// per-controller with its own HA rules (3+ admission-controller
// replicas), which a single operational count can't express.
func renderKyvernoValues(obj *configv1alpha1.EducatesClusterConfig) map[string]any {
	values := map[string]any{}

	if obj.Spec.ImageRegistry != nil && obj.Spec.ImageRegistry.Prefix != "" {
		registry := func() map[string]any {
			return map[string]any{
				"image": map[string]any{
					"registry": obj.Spec.ImageRegistry.Prefix,
				},
			}
		}
		values["global"] = registry()
		// The readiness-checker image of the pre-delete webhook cleanup
		// and scale-to-zero hooks, and of the chart tests, pins registry
		// ghcr.io, which wins over global.image.registry (the tests
		// don't read the global value at all). Verified against chart
		// 3.9.2.
		values["webhooksCleanup"] = registry()
		values["test"] = registry()
	}
	// Kyverno's global.imagePullSecrets is what each component's own
	// list, which the operator leaves unset, defaults to. It is not the
	// top-level imagePullSecrets, which creates registry Secrets for
	// image verification policies.
	setGlobalImagePullSecrets(values, obj)

	return values
}

// ensureKyvernoReady gates the rest of the pipeline on the four
// kyverno Deployments reporting Available=True. Same shape as
// ensureCertManagerReady: a missing Deployment (404) maps to
// "not ready" rather than a hard error.
func (r *EducatesClusterConfigReconciler) ensureKyvernoReady(ctx context.Context) error {
	for _, name := range kyvernoDeployments {
		dep := &appsv1.Deployment{}
		key := types.NamespacedName{Namespace: kyvernoNamespace, Name: name}
		if err := r.Get(ctx, key, dep); err != nil {
			if apierrors.IsNotFound(err) {
				return errKyvernoNotReady
			}
			return fmt.Errorf("get Deployment %s: %w", key, err)
		}
		if !deploymentAvailable(dep) {
			return errKyvernoNotReady
		}
	}
	return nil
}

// cleanupKyverno unwinds the install: helm uninstall → kyverno
// namespace delete. helm-managed webhook configurations and CRDs
// cascade with the release uninstall. Idempotent.
//
// The cleanup runs only when the spec has the operator install Kyverno.
// Otherwise the operator installed nothing here, and a Kyverno release
// or namespace under the same names belongs to the administrator, so it
// must survive the teardown. The policy engines and the kyverno provider
// are immutable once set, and the kyverno block can only be added to a
// spec that installed nothing without it, so the spec seen at deletion
// is the one the install ran with.
func (r *EducatesClusterConfigReconciler) cleanupKyverno(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	if !shouldInstallKyverno(obj) {
		return nil
	}
	hc, err := r.HelmClientFor(kyvernoNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for cleanup: %w", err)
	}
	if err := hc.Uninstall(kyvernoReleaseName); err != nil {
		return fmt.Errorf("uninstall kyverno release: %w", err)
	}
	if err := r.deleteIfPresent(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: kyvernoNamespace},
	}); err != nil {
		return fmt.Errorf("delete kyverno namespace: %w", err)
	}
	return nil
}
