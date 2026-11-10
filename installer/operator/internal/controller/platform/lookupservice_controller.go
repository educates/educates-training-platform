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

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

const (
	// lookupServiceReleaseName is the Helm release name for the
	// lookup-service subchart. Co-located with secrets-manager and
	// (eventually) session-manager in platformNamespace.
	lookupServiceReleaseName = "lookup-service"

	// lookupServiceDeploymentName matches the chart template's fixed
	// Deployment name. Readiness gate for Ready=True.
	lookupServiceDeploymentName = "lookup-service"

	// finalizerLookupService guarantees the reconciler drains the
	// helm release before the CR is removed.
	finalizerLookupService = "lookupservice.platform.educates.dev/finalizer"

	// An IngressReady condition is reserved in the status contract
	// but not yet published. v1alpha1 doesn't gate on
	// it separately — Deployment.Available is sufficient signal
	// because the chart renders the Ingress alongside the Deployment
	// in the same helm install. A future probe (LoadBalancer
	// resolution, HTTP reachability) reintroduces the condition.
)

// LookupServiceReconciler drives the LookupService CR. Mirrors
// SecretsManagerReconciler with the addition of a status.url field
// derived from spec.ingress.prefix +
// EducatesClusterConfig.status.ingress.domain.
type LookupServiceReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// HelmClientFor returns a Helm client scoped to the given
	// namespace. Production: REST-config-backed. Envtest: in-memory.
	HelmClientFor func(namespace string) (*helm.Client, error)
}

// +kubebuilder:rbac:groups=platform.educates.dev,resources=lookupservices,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.educates.dev,resources=lookupservices/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.educates.dev,resources=lookupservices/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.educates.dev,resources=secretsmanagers,verbs=get;list;watch

// Reconcile drives a LookupService CR through its lifecycle.
func (r *LookupServiceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	obj := &platformv1alpha1.LookupService{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.V(1).Info("Reconciling LookupService")

	if !obj.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(obj, finalizerLookupService) {
			r.markLSPhase(obj, platformv1alpha1.ComponentPhaseUninstalling)
			if err := r.updateLSStatusWithTransitionLog(ctx, obj); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.cleanupLS(ctx); err != nil {
				return ctrl.Result{}, err
			}
			// See SecretsManager rationale: status update above leaves
			// the local obj stale; re-Get under RetryOnConflict.
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				live := &platformv1alpha1.LookupService{}
				if err := r.Get(ctx, req.NamespacedName, live); err != nil {
					return client.IgnoreNotFound(err)
				}
				if !controllerutil.ContainsFinalizer(live, finalizerLookupService) {
					return nil
				}
				controllerutil.RemoveFinalizer(live, finalizerLookupService)
				return r.Update(ctx, live)
			}); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(obj, finalizerLookupService) {
		controllerutil.AddFinalizer(obj, finalizerLookupService)
		if err := r.Update(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
			return ctrl.Result{}, err
		}
	}

	cfg, ready, err := r.clusterConfigReadyLS(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read EducatesClusterConfig: %w", err)
	}
	if !ready {
		r.markLSClusterConfigAvailable(obj, metav1.ConditionFalse, "ClusterConfigNotReady",
			"EducatesClusterConfig 'cluster' is not yet Ready; waiting")
		r.markLSReady(obj, metav1.ConditionFalse, "WaitingForClusterConfig",
			"EducatesClusterConfig 'cluster' must reach Ready before lookup-service can install")
		r.markLSPhase(obj, platformv1alpha1.ComponentPhasePending)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateLSStatusWithTransitionLog(ctx, obj)
	}
	r.markLSClusterConfigAvailable(obj, metav1.ConditionTrue, "ClusterConfigReady",
		"EducatesClusterConfig 'cluster' is Ready")

	// LookupService additionally requires the cluster config to have
	// published a usable Ingress contract. Without status.ingress, we
	// can't derive the lookup-service hostname or its TLS Secret.
	if cfg.Status.Ingress == nil {
		r.markLSReady(obj, metav1.ConditionFalse, "MissingIngressContract",
			"EducatesClusterConfig.status.ingress is not populated; waiting")
		r.markLSPhase(obj, platformv1alpha1.ComponentPhasePending)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateLSStatusWithTransitionLog(ctx, obj)
	}

	// LookupService's subchart renders SecretCopier resources to
	// fan ingress TLS / pull secrets into workshop namespaces. The
	// `secrets.educates.dev` CRDs that back those kinds ship inside
	// the secrets-manager subchart's `crds/` directory and only land
	// once SecretsManager has reconciled. Without this gate, helm
	// install fails with: `no matches for kind "SecretCopier" in
	// version "secrets.educates.dev/v1beta1"`. After an operator upgrade
	// it also waits for SecretsManager to run the chart this operator
	// embeds, whose CRD schemas the new SecretCopier resources are written
	// against (see secretsManagerStatus). Mirrors the equivalent gate on
	// SessionManager.
	smReady, smVersion, err := secretsManagerStatus(ctx, r.Client)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read SecretsManager: %w", err)
	}
	if !smReady {
		r.markLSReady(obj, metav1.ConditionFalse, "WaitingForSecretsManager",
			"SecretsManager 'cluster' must reach Ready before lookup-service can install (SecretCopier CRD ships with secrets-manager)")
		r.markLSPhase(obj, platformv1alpha1.ComponentPhasePending)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateLSStatusWithTransitionLog(ctx, obj)
	}
	if smVersion != vendoredcharts.SecretsManagerChartVersion {
		r.markLSReady(obj, metav1.ConditionFalse, "WaitingForSecretsManager",
			fmt.Sprintf("SecretsManager 'cluster' runs secrets-manager chart %q, not %q; lookup-service waits for its release to converge, since lookup-service's SecretCopier resources use the CRDs it ships",
				smVersion, vendoredcharts.SecretsManagerChartVersion))
		r.markLSPhase(obj, platformv1alpha1.ComponentPhasePending)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateLSStatusWithTransitionLog(ctx, obj)
	}

	r.markLSPhase(obj, platformv1alpha1.ComponentPhaseInstalling)
	res, err := r.installOrUpgradeLS(ctx, obj, cfg)
	if err != nil {
		r.markLSDeployed(obj, metav1.ConditionFalse, "InstallFailed", err.Error())
		r.markLSReady(obj, metav1.ConditionFalse, "InstallFailed", err.Error())
		_ = r.updateLSStatusWithTransitionLog(ctx, obj)
		return ctrl.Result{}, fmt.Errorf("helm install lookup-service: %w", err)
	}
	if proceed, result, err := handlePlatformReleaseResult("lookup-service", res,
		func(reason, message string) {
			r.markLSDeployed(obj, metav1.ConditionFalse, reason, message)
			r.markLSReady(obj, metav1.ConditionFalse, reason, message)
		},
		func(phase platformv1alpha1.ComponentPhase) { r.markLSPhase(obj, phase) },
		func() error { return r.updateLSStatusWithTransitionLog(ctx, obj) },
	); !proceed {
		return result, err
	}
	r.markLSDeployed(obj, metav1.ConditionTrue, "ChartInstalled",
		fmt.Sprintf("lookup-service chart %s installed in namespace %s",
			vendoredcharts.LookupServiceChartVersion, platformNamespace))

	avail, err := r.deploymentAvailableLS(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read lookup-service Deployment: %w", err)
	}
	if !avail {
		r.markLSReady(obj, metav1.ConditionFalse, "WaitingForDeployment",
			"lookup-service Deployment not yet Available")
		r.markLSPhase(obj, platformv1alpha1.ComponentPhaseInstalling)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateLSStatusWithTransitionLog(ctx, obj)
	}

	host := lookupServiceHost(obj, cfg)
	obj.Status.URL = "https://" + host
	obj.Status.InstalledVersion = vendoredcharts.LookupServiceChartVersion
	obj.Status.DeploymentRef = &platformv1alpha1.NamespacedRef{
		Namespace: platformNamespace,
		Name:      lookupServiceDeploymentName,
	}
	r.markLSReady(obj, metav1.ConditionTrue, "LookupServiceReady",
		"lookup-service is installed and Available")
	r.markLSPhase(obj, platformv1alpha1.ComponentPhaseReady)
	obj.Status.ObservedGeneration = obj.Generation
	return ctrl.Result{}, r.updateLSStatusWithTransitionLog(ctx, obj)
}

// clusterConfigReadyLS fetches the EducatesClusterConfig singleton
// and reports whether its aggregate Ready condition is True.
func (r *LookupServiceReconciler) clusterConfigReadyLS(ctx context.Context) (*configv1alpha1.EducatesClusterConfig, bool, error) {
	cfg := &configv1alpha1.EducatesClusterConfig{}
	if err := r.Get(ctx, types.NamespacedName{Name: configSingletonName}, cfg); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	cond := meta.FindStatusCondition(cfg.Status.Conditions, conditionReady)
	if cond == nil || cond.Status != metav1.ConditionTrue {
		return cfg, false, nil
	}
	return cfg, true, nil
}

// lookupServiceHost composes the fully-qualified Ingress hostname
// from CR prefix + cluster config domain — `<prefix>.<domain>`.
func lookupServiceHost(obj *platformv1alpha1.LookupService, cfg *configv1alpha1.EducatesClusterConfig) string {
	return fmt.Sprintf("%s.%s", obj.Spec.Ingress.Prefix, cfg.Status.Ingress.Domain)
}

func (r *LookupServiceReconciler) installOrUpgradeLS(ctx context.Context, obj *platformv1alpha1.LookupService, cfg *configv1alpha1.EducatesClusterConfig) (helm.Result, error) {
	if err := ensurePlatformNamespace(ctx, r.Client); err != nil {
		return helm.Result{}, err
	}
	chrt, err := vendoredcharts.LookupService()
	if err != nil {
		return helm.Result{}, fmt.Errorf("load embedded chart: %w", err)
	}
	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		return helm.Result{}, fmt.Errorf("build helm client: %w", err)
	}
	return convergeRelease(ctx, r.Client, hc, lookupServiceReleaseName, chrt,
		renderLookupServiceValues(obj, cfg, chartImageNamespace(chrt)))
}

// renderLookupServiceValues maps the CR spec + cluster config status
// into the lookup-service subchart's values shape. Keep aligned with
// `installer/charts/educates-training-platform/charts/lookup-service/values.yaml`.
// imageNamespace is the chart's own image registry namespace, kept
// under the cluster config's image registry prefix.
func renderLookupServiceValues(obj *platformv1alpha1.LookupService, cfg *configv1alpha1.EducatesClusterConfig, imageNamespace string) map[string]any {
	values := map[string]any{}

	if dev := developmentImageRegistryValues(cfg, imageNamespace); dev != nil {
		values["development"] = dev
	}

	if obj.Spec.Image != nil {
		values["image"] = map[string]any{
			"repository": obj.Spec.Image.Repository,
			"tag":        obj.Spec.Image.Tag,
		}
	}

	if cfg.Status.ImageRegistry != nil && len(cfg.Status.ImageRegistry.PullSecrets) > 0 {
		refs := make([]any, 0, len(cfg.Status.ImageRegistry.PullSecrets))
		for _, ref := range cfg.Status.ImageRegistry.PullSecrets {
			refs = append(refs, map[string]any{"name": ref.Name})
		}
		values["imagePullSecrets"] = refs
	}

	if obj.Spec.Resources != nil {
		values["resources"] = obj.Spec.Resources
	}

	// clusterIngress — tlsCertificateRef defaults to the wildcard cert
	// published in cluster config status. The CR can override the
	// Secret name; namespace stays the cluster config's (the chart's
	// auto-SecretCopier handles cross-namespace placement when the
	// Secret lives outside the release namespace). Both name and
	// namespace are empty when there is no in-cluster wildcard
	// certificate (None provider); the chart then renders a plain
	// Ingress with no TLS block.
	wildcardNS := ""
	tlsName := ""
	if wc := cfg.Status.Ingress.WildcardCertificateSecretRef; wc != nil {
		wildcardNS = wc.Namespace
		tlsName = wc.Name
	}
	if obj.Spec.Ingress.TLSSecretRef != nil {
		tlsName = obj.Spec.Ingress.TLSSecretRef.Name
	}
	clusterIngress := map[string]any{
		"tlsCertificateRef": map[string]any{"name": tlsName, "namespace": wildcardNS},
	}
	if cfg.Status.Ingress.CACertificateSecretRef != nil {
		clusterIngress["caCertificateRef"] = map[string]any{
			"name":      cfg.Status.Ingress.CACertificateSecretRef.Name,
			"namespace": cfg.Status.Ingress.CACertificateSecretRef.Namespace,
		}
	}
	values["clusterIngress"] = clusterIngress

	values["ingress"] = map[string]any{
		"host":      lookupServiceHost(obj, cfg),
		"className": cfg.Status.Ingress.IngressClassName,
	}

	return values
}

func (r *LookupServiceReconciler) deploymentAvailableLS(ctx context.Context) (bool, error) {
	dep := &appsv1.Deployment{}
	key := types.NamespacedName{Namespace: platformNamespace, Name: lookupServiceDeploymentName}
	if err := r.Get(ctx, key, dep); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	for _, c := range dep.Status.Conditions {
		if c.Type == appsv1.DeploymentAvailable {
			return c.Status == corev1.ConditionTrue, nil
		}
	}
	return false, nil
}

// cleanupLS uninstalls the helm release. The platform namespace is
// left in place (shared with secrets-manager / session-manager).
func (r *LookupServiceReconciler) cleanupLS(ctx context.Context) error {
	_ = ctx
	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for cleanup: %w", err)
	}
	if err := hc.Uninstall(lookupServiceReleaseName); err != nil {
		return fmt.Errorf("uninstall release: %w", err)
	}
	return nil
}

// --- Status helpers (LookupService-typed; the SecretsManager
// equivalents are typed against a different CR, hence the duplication.
// Both packages keep their own to avoid an awkward generic helper
// that buys little.)

func (r *LookupServiceReconciler) markLSReady(obj *platformv1alpha1.LookupService, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *LookupServiceReconciler) markLSClusterConfigAvailable(obj *platformv1alpha1.LookupService, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionClusterConfigAvailable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *LookupServiceReconciler) markLSDeployed(obj *platformv1alpha1.LookupService, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionDeployed,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *LookupServiceReconciler) markLSPhase(obj *platformv1alpha1.LookupService, phase platformv1alpha1.ComponentPhase) {
	obj.Status.Phase = phase
}

func (r *LookupServiceReconciler) updateLSStatusWithTransitionLog(ctx context.Context, obj *platformv1alpha1.LookupService) error {
	log := logf.FromContext(ctx)
	desiredReady := meta.FindStatusCondition(obj.Status.Conditions, conditionReady)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		live := &platformv1alpha1.LookupService{}
		if err := r.Get(ctx, types.NamespacedName{Name: obj.Name}, live); err != nil {
			return err
		}
		priorReady := meta.FindStatusCondition(live.Status.Conditions, conditionReady)
		live.Status = obj.Status
		if err := r.Status().Update(ctx, live); err != nil {
			return err
		}
		if desiredReady != nil && (priorReady == nil ||
			priorReady.Status != desiredReady.Status ||
			priorReady.Reason != desiredReady.Reason) {
			log.Info("LookupService Ready transition",
				"status", desiredReady.Status, "reason", desiredReady.Reason,
				"message", desiredReady.Message)
		}
		return nil
	})
}

// --- Watch wiring ---------------------------------------------------

// SetupWithManager configures the LookupService controller. Watches:
//   - LookupService (For target, GenerationChangedPredicate).
//   - EducatesClusterConfig (cross-CR gate 1).
//   - SecretsManager (cross-CR gate 2: ships the SecretCopier CRD
//     the lookup-service chart depends on).
//   - apps/v1 Deployment, narrowed to platform-ns + lookup-service.
func (r *LookupServiceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.LookupService{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&configv1alpha1.EducatesClusterConfig{},
			handler.EnqueueRequestsFromMapFunc(mapClusterConfigToLookupService)).
		Watches(&platformv1alpha1.SecretsManager{},
			handler.EnqueueRequestsFromMapFunc(mapSecretsManagerToLookupService)).
		Watches(&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(mapLookupServiceDeployment)).
		Named("platform-lookupservice").
		Complete(r)
}

func mapClusterConfigToLookupService(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() != configSingletonName {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
}

// mapSecretsManagerToLookupService re-enqueues LookupService when
// the SecretsManager singleton flips Ready — that's the signal that
// the SecretCopier CRD is installed and the lookup-service install
// can finally render.
func mapSecretsManagerToLookupService(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() != singletonName {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
}

func mapLookupServiceDeployment(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetNamespace() != platformNamespace || obj.GetName() != lookupServiceDeploymentName {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
}
