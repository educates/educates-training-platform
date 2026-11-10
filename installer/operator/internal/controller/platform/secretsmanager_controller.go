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
	"strings"
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

// Constants shared by every platform reconciler. They are package-level
// rather than struct fields because they describe the operator's
// install convention (where platform components live), not per-CR
// state.
const (
	// platformNamespace is where the operator installs the runtime
	// platform components (secrets-manager, lookup-service, and
	// session-manager). Mirrors v3 behavior: the umbrella
	// `educates-training-platform` Helm chart
	// has historically been `helm install -n educates`, and three
	// co-located components share a single namespace.
	platformNamespace = "educates"

	// secretsManagerReleaseName is the Helm release name used for the
	// secrets-manager subchart install. Co-locating it in
	// platformNamespace alongside future LookupService /
	// SessionManager releases requires unique release names per
	// namespace, hence the per-component constant.
	secretsManagerReleaseName = "secrets-manager"

	// secretsManagerDeploymentName matches the chart template's
	// fixed-name Deployment. The reconciler uses this to gate Ready
	// on the upstream component's availability without parsing chart
	// output.
	secretsManagerDeploymentName = "secrets-manager"

	// singletonName mirrors the CEL rule on the SecretsManager CRD:
	// the cluster has exactly one named "cluster". Used by watch
	// mappers to enqueue the singleton on relevant events.
	singletonName = "cluster"

	// configSingletonName is the EducatesClusterConfig's singleton
	// name. Platform components consume that CR's status as their
	// input contract.
	configSingletonName = "cluster"
)

// DeploymentWatchNamespaces returns the namespaces the platform reconcilers
// (SecretsManager, LookupService, SessionManager) read their component
// Deployments in — all of them the shared platform namespace. Exposed so the
// manager's Deployment informer can be scoped to it instead of caching every
// Deployment in the cluster.
func DeploymentWatchNamespaces() []string {
	return []string{platformNamespace}
}

const (
	// finalizerSecretsManager guarantees the reconciler gets a chance
	// to uninstall the helm release + delete the platform namespace
	// before the CR is removed.
	finalizerSecretsManager = "secretsmanager.platform.educates.dev/finalizer"

	// Condition types published on SecretsManager.status. Status
	// contract: aggregate Ready plus two phase-specific
	// types — ClusterConfigAvailable (does EducatesClusterConfig
	// exist and report Ready?) and Deployed (did the helm install
	// land + Deployment become Available?).
	conditionReady                  = "Ready"
	conditionClusterConfigAvailable = "ClusterConfigAvailable"
	conditionDeployed               = "Deployed"
)

// SecretsManagerReconciler drives the SecretsManager CR. Per-phase
// flow mirrors the EducatesClusterConfig Managed-mode reconciler:
// validate prerequisites (here: EducatesClusterConfig.Ready), install
// the helm chart, gate Ready on Deployment availability, finalizer
// drain on delete.
type SecretsManagerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// HelmClientFor returns a Helm client scoped to the given
	// namespace. Production wiring builds a REST-config-backed
	// client (main.go); envtest injects an in-memory factory so the
	// install/upgrade/status paths can be exercised without a real
	// apiserver behind Helm SDK's kube client.
	HelmClientFor func(namespace string) (*helm.Client, error)
}

// +kubebuilder:rbac:groups=platform.educates.dev,resources=secretsmanagers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.educates.dev,resources=secretsmanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.educates.dev,resources=secretsmanagers/finalizers,verbs=update

// The reconciler reads EducatesClusterConfig.status as its input
// contract. It never writes to that resource — read-only.
// +kubebuilder:rbac:groups=config.educates.dev,resources=educatesclusterconfigs,verbs=get;list;watch

// Helm install of the secrets-manager subchart creates a Namespace
// + Deployment + ServiceAccount + ClusterRole + ClusterRoleBinding.
// The reconciler also reads the Deployment status as its readiness
// gate. The cluster-admin shortcut binding from the
// `educates-installer` Helm chart covers any other resource the
// SDK touches; these will be scoped down.
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch;create;patch;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch

// Reconcile drives a SecretsManager CR through its lifecycle.
func (r *SecretsManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	obj := &platformv1alpha1.SecretsManager{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.V(1).Info("Reconciling SecretsManager")

	// Deletion path: drain helm release + namespace, then drop the
	// finalizer so garbage collection finishes.
	if !obj.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(obj, finalizerSecretsManager) {
			r.markPhase(obj, platformv1alpha1.ComponentPhaseUninstalling)
			if err := r.updateStatusWithTransitionLog(ctx, obj); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.cleanup(ctx); err != nil {
				return ctrl.Result{}, err
			}
			// updateStatusWithTransitionLog above re-Gets a live copy and
			// updates status against it; our local `obj` ResourceVersion
			// is now stale. Wrap finalizer removal in RetryOnConflict so
			// a concurrent watch event can't race the cleanup path.
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				live := &platformv1alpha1.SecretsManager{}
				if err := r.Get(ctx, req.NamespacedName, live); err != nil {
					return client.IgnoreNotFound(err)
				}
				if !controllerutil.ContainsFinalizer(live, finalizerSecretsManager) {
					return nil
				}
				controllerutil.RemoveFinalizer(live, finalizerSecretsManager)
				return r.Update(ctx, live)
			}); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	// Add the finalizer on first reconcile and fall through. We can't
	// rely on the Update event to re-fire Reconcile under the For()
	// target's GenerationChangedPredicate (finalizer Updates don't
	// bump generation), so the same call has to drive the resource
	// to its first published status.
	if !controllerutil.ContainsFinalizer(obj, finalizerSecretsManager) {
		controllerutil.AddFinalizer(obj, finalizerSecretsManager)
		if err := r.Update(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		// Re-Get so subsequent Status().Update writes against the
		// post-Update ResourceVersion. updateStatusWithTransitionLog
		// also re-Gets via RetryOnConflict, so this is belt-and-
		// suspenders; we keep it explicit so the obj we mutate
		// matches what's in the apiserver.
		if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Gate everything on the EducatesClusterConfig being Ready. This
	// is the cross-CR input contract for every platform component.
	cfg, ready, err := r.clusterConfigReady(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read EducatesClusterConfig: %w", err)
	}
	if !ready {
		r.markClusterConfigAvailable(obj, metav1.ConditionFalse, "ClusterConfigNotReady",
			"EducatesClusterConfig 'cluster' is not yet Ready; waiting")
		r.markReady(obj, metav1.ConditionFalse, "WaitingForClusterConfig",
			"EducatesClusterConfig 'cluster' must reach Ready before secrets-manager can install")
		r.markPhase(obj, platformv1alpha1.ComponentPhasePending)
		// Watch on EducatesClusterConfig re-fires when its Ready
		// condition flips; RequeueAfter is belt-and-suspenders for
		// the cache-vs-watch race we hit on cluster services.
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateStatusWithTransitionLog(ctx, obj)
	}
	r.markClusterConfigAvailable(obj, metav1.ConditionTrue, "ClusterConfigReady",
		"EducatesClusterConfig 'cluster' is Ready")

	// Helm install/upgrade. Idempotent: helm.Install on an existing
	// release returns AlreadyExists, which we translate into an
	// Upgrade call so re-renders pick up spec changes.
	r.markPhase(obj, platformv1alpha1.ComponentPhaseInstalling)
	res, err := r.installOrUpgrade(ctx, obj, cfg)
	if err != nil {
		r.markDeployed(obj, metav1.ConditionFalse, "InstallFailed", err.Error())
		r.markReady(obj, metav1.ConditionFalse, "InstallFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, obj)
		return ctrl.Result{}, fmt.Errorf("helm install secrets-manager: %w", err)
	}
	if proceed, result, err := handlePlatformReleaseResult("secrets-manager", res,
		func(reason, message string) {
			r.markDeployed(obj, metav1.ConditionFalse, reason, message)
			r.markReady(obj, metav1.ConditionFalse, reason, message)
		},
		func(phase platformv1alpha1.ComponentPhase) { r.markPhase(obj, phase) },
		func() error { return r.updateStatusWithTransitionLog(ctx, obj) },
	); !proceed {
		return result, err
	}
	r.markDeployed(obj, metav1.ConditionTrue, "ChartInstalled",
		fmt.Sprintf("secrets-manager chart %s installed in namespace %s",
			vendoredcharts.SecretsManagerChartVersion, platformNamespace))

	// Readiness gate: the helm install completed, but the upstream
	// Deployment may still be rolling. Same belt-and-suspenders
	// RequeueAfter pattern as the cluster-services reconcilers — the
	// Deployment watch should fire, but cache-vs-watch races have
	// bitten us before.
	avail, err := r.deploymentAvailable(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read secrets-manager Deployment: %w", err)
	}
	if !avail {
		r.markReady(obj, metav1.ConditionFalse, "WaitingForDeployment",
			"secrets-manager Deployment not yet Available")
		r.markPhase(obj, platformv1alpha1.ComponentPhaseInstalling)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateStatusWithTransitionLog(ctx, obj)
	}

	// Publish the status surface.
	obj.Status.InstalledVersion = vendoredcharts.SecretsManagerChartVersion
	obj.Status.DeploymentRef = &platformv1alpha1.NamespacedRef{
		Namespace: platformNamespace,
		Name:      secretsManagerDeploymentName,
	}
	r.markReady(obj, metav1.ConditionTrue, "SecretsManagerReady",
		"secrets-manager is installed and Available")
	r.markPhase(obj, platformv1alpha1.ComponentPhaseReady)
	obj.Status.ObservedGeneration = obj.Generation
	return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, obj)
}

// clusterConfigReady fetches the EducatesClusterConfig singleton and
// reports whether its Ready condition is True. Returns the parsed CR
// alongside the bool so callers can read status fields without a
// second Get.
func (r *SecretsManagerReconciler) clusterConfigReady(ctx context.Context) (*configv1alpha1.EducatesClusterConfig, bool, error) {
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

// secretsManagerStatus fetches the SecretsManager singleton and reports
// whether its aggregate Ready condition is True and which secrets-manager
// chart version it runs. NotFound is treated as "not ready, may appear
// later", the same shape as the cluster config gate.
//
// SessionManager and LookupService gate on both. Their charts create
// SecretCopier and SecretInjector resources, whose secrets.educates.dev CRDs
// ship in the secrets-manager chart and reach that chart's schemas only when
// this reconciler converges its release. After an operator upgrade
// SecretsManager stays Ready from the previous release until then, so Ready
// alone would let a dependent chart's Helm upgrade run against the previous
// release's CRD schemas, which reject or prune the fields it adds. Comparing
// the version with the chart this operator embeds orders a released
// upgrade; a development build whose chart version does not change is not
// ordered.
func secretsManagerStatus(ctx context.Context, c client.Reader) (ready bool, installedVersion string, err error) {
	sm := &platformv1alpha1.SecretsManager{}
	if err := c.Get(ctx, types.NamespacedName{Name: singletonName}, sm); err != nil {
		if apierrors.IsNotFound(err) {
			return false, "", nil
		}
		return false, "", err
	}
	cond := meta.FindStatusCondition(sm.Status.Conditions, conditionReady)
	return cond != nil && cond.Status == metav1.ConditionTrue, sm.Status.InstalledVersion, nil
}

// installOrUpgrade renders chart values from CR + cluster config, applies
// the chart's CRDs and converges the helm release via convergeRelease
// (install/upgrade on drift, self-heal a failed release once its inputs
// change, hold otherwise).
// The returned Result tells the caller whether to proceed to the Deployment
// readiness gate.
func (r *SecretsManagerReconciler) installOrUpgrade(ctx context.Context, obj *platformv1alpha1.SecretsManager, cfg *configv1alpha1.EducatesClusterConfig) (helm.Result, error) {
	if err := ensurePlatformNamespace(ctx, r.Client); err != nil {
		return helm.Result{}, err
	}
	chrt, err := vendoredcharts.SecretsManager()
	if err != nil {
		return helm.Result{}, fmt.Errorf("load embedded chart: %w", err)
	}
	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		return helm.Result{}, fmt.Errorf("build helm client: %w", err)
	}
	return convergeRelease(ctx, r.Client, hc, secretsManagerReleaseName, chrt,
		renderSecretsManagerValues(obj, cfg, chartImageNamespace(chrt)))
}

// renderSecretsManagerValues maps SecretsManager spec + the cluster
// config status into the secrets-manager subchart's values shape. The
// subchart's values.yaml is the contract — keep this aligned with
// `installer/charts/educates-training-platform/charts/secrets-manager/values.yaml`.
// imageNamespace is the chart's own image registry namespace, kept
// under the cluster config's image registry prefix.
func renderSecretsManagerValues(obj *platformv1alpha1.SecretsManager, cfg *configv1alpha1.EducatesClusterConfig, imageNamespace string) map[string]any {
	values := map[string]any{
		"logLevel": defaultLogLevel(obj.Spec.LogLevel),
	}

	if dev := developmentImageRegistryValues(cfg, imageNamespace); dev != nil {
		values["development"] = dev
	}

	// image overrides from SecretsManager spec. Empty fields stay
	// empty so the chart derives from imageRegistry + appVersion.
	if obj.Spec.Image != nil {
		values["image"] = map[string]any{
			"repository": obj.Spec.Image.Repository,
			"tag":        obj.Spec.Image.Tag,
		}
	}

	// imagePullSecrets — propagate from cluster config so workshop
	// images and platform images share the same pull credentials.
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

	// clusterSecurity.policyEngine — propagated from the resolved
	// cluster config. Only OpenShiftSCC alters subchart rendering;
	// other values are inert here but match the chart's standalone
	// install contract for diagnosability.
	if cfg.Status.PolicyEnforcement != nil && cfg.Status.PolicyEnforcement.ClusterPolicyEngine != "" {
		values["clusterSecurity"] = map[string]any{
			"policyEngine": string(cfg.Status.PolicyEnforcement.ClusterPolicyEngine),
		}
	}
	return values
}

// splitImageRef splits a full image reference into repository and tag
// on the last ':' after the last '/'. A reference without a tag comes
// back with an empty tag, which the charts treat as fall-through to
// Chart.AppVersion. Digest-pinned references (...@sha256:...) cannot
// round-trip through {repository,tag}-shaped chart values and are
// returned whole as the repository — unsupported, documented.
func splitImageRef(ref string) (repository, tag string) {
	if strings.Contains(ref, "@") {
		return ref, ""
	}
	slash := strings.LastIndex(ref, "/")
	if colon := strings.LastIndex(ref, ":"); colon > slash {
		return ref[:colon], ref[colon+1:]
	}
	return ref, ""
}

// defaultLogLevel returns "info" when the spec didn't set a level. The
// CRD's +kubebuilder:default=info usually handles this server-side,
// but envtest with stale CRDs / standalone unit tests can pass an
// empty string — guarding here keeps the chart values sane.
func defaultLogLevel(l platformv1alpha1.LogLevel) string {
	if l == "" {
		return string(platformv1alpha1.LogLevelInfo)
	}
	return string(l)
}

// deploymentAvailable reports whether the secrets-manager Deployment
// has Available=True. Missing Deployment is treated as "not ready,
// not yet rolled out" rather than an error — helm install may not
// have created it yet, or a deletion-replay is in progress.
func (r *SecretsManagerReconciler) deploymentAvailable(ctx context.Context) (bool, error) {
	dep := &appsv1.Deployment{}
	key := types.NamespacedName{Namespace: platformNamespace, Name: secretsManagerDeploymentName}
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

// cleanup uninstalls the helm release. The Namespace is left in
// place — it's shared with future LookupService / SessionManager
// installs, and removing it would tear them down too. This is
// asymmetric with the cluster-services reconcilers (which own their
// per-service namespace end-to-end); a follow-up may add a
// once-everything-is-gone namespace sweeper.
func (r *SecretsManagerReconciler) cleanup(ctx context.Context) error {
	_ = ctx // helm SDK uses its own context internally
	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for cleanup: %w", err)
	}
	if err := hc.Uninstall(secretsManagerReleaseName); err != nil {
		return fmt.Errorf("uninstall release: %w", err)
	}
	return nil
}

// --- Status helpers -------------------------------------------------

func (r *SecretsManagerReconciler) markReady(obj *platformv1alpha1.SecretsManager, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *SecretsManagerReconciler) markClusterConfigAvailable(obj *platformv1alpha1.SecretsManager, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionClusterConfigAvailable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *SecretsManagerReconciler) markDeployed(obj *platformv1alpha1.SecretsManager, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionDeployed,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *SecretsManagerReconciler) markPhase(obj *platformv1alpha1.SecretsManager, phase platformv1alpha1.ComponentPhase) {
	obj.Status.Phase = phase
}

// updateStatusWithTransitionLog writes status with conflict-retry and
// logs the aggregate-Ready transition once per change. Mirrors the
// pattern in the config-controller package so behavior is consistent
// across CRD groups.
func (r *SecretsManagerReconciler) updateStatusWithTransitionLog(ctx context.Context, obj *platformv1alpha1.SecretsManager) error {
	log := logf.FromContext(ctx)
	desiredReady := meta.FindStatusCondition(obj.Status.Conditions, conditionReady)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		live := &platformv1alpha1.SecretsManager{}
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
			log.Info("SecretsManager Ready transition",
				"status", desiredReady.Status, "reason", desiredReady.Reason,
				"message", desiredReady.Message)
		}
		return nil
	})
}

// --- Watch wiring ---------------------------------------------------

// SetupWithManager configures watches and predicates. Watches:
//   - SecretsManager (For target) — GenerationChangedPredicate so
//     status-only updates don't self-fire.
//   - EducatesClusterConfig — enqueue our singleton when the input
//     contract (its status.Ready) might have flipped.
//   - apps/v1 Deployment — narrowing mapper to platform namespace +
//     secrets-manager name only.
func (r *SecretsManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.SecretsManager{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&configv1alpha1.EducatesClusterConfig{},
			handler.EnqueueRequestsFromMapFunc(mapClusterConfigToSecretsManager)).
		Watches(&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(mapSecretsManagerDeployment)).
		Named("platform-secretsmanager").
		Complete(r)
}

// mapClusterConfigToSecretsManager enqueues the SecretsManager
// singleton when the cluster config's Ready condition might have
// changed. We always enqueue regardless of the actual transition —
// the reconciler is idempotent and the watch event itself is
// already filtered to the cluster singleton by name.
func mapClusterConfigToSecretsManager(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() != configSingletonName {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
}

// mapSecretsManagerDeployment narrows Deployment events to the one
// our reconciler cares about. Cluster-wide Deployment churn doesn't
// reach the reconcile queue.
func mapSecretsManagerDeployment(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetNamespace() != platformNamespace || obj.GetName() != secretsManagerDeploymentName {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
}
