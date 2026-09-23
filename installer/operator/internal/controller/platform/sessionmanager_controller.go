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

const (
	// sessionManagerReleaseName is the Helm release name for the
	// session-manager subchart. Co-located with secrets-manager +
	// lookup-service in platformNamespace.
	sessionManagerReleaseName = "session-manager"

	// sessionManagerDeploymentName matches the chart template's fixed
	// Deployment name. Readiness gate for Ready=True.
	sessionManagerDeploymentName = "session-manager"

	// finalizerSessionManager guarantees the reconciler drains the
	// helm release before the CR is removed.
	finalizerSessionManager = "sessionmanager.platform.educates.dev/finalizer"

	// conditionSecretsManagerAvailable is the second cross-CR gate.
	// session-manager's runtime relies on secrets-manager to propagate
	// pull secrets and ingress TLS into workshop namespaces; we refuse
	// to install until SecretsManager.Ready.
	conditionSecretsManagerAvailable = "SecretsManagerAvailable"

	// nodeCAInjectorReleaseName is the Helm release name for the
	// optional node-ca-injector subchart installed by SessionManager
	// when nodeCATrust resolves to Install.
	nodeCAInjectorReleaseName = "node-ca-injector"

	// remoteAccessReleaseName is the Helm release name for the
	// optional remote-access subchart installed by SessionManager when
	// remoteAccess resolves to Install.
	remoteAccessReleaseName = "remote-access"

	// conditionNodeCATrustDeployed and conditionRemoteAccessDeployed
	// report the outcome of each optional install. Status=True with
	// reason `Installed` or `Skipped`; Status=False with reason
	// `Refused` when an explicit Enabled fails its prerequisite check.
	conditionNodeCATrustDeployed  = "NodeCATrustDeployed"
	conditionRemoteAccessDeployed = "RemoteAccessDeployed"
)

// SessionManagerReconciler drives the SessionManager CR. Two cross-CR
// gates (EducatesClusterConfig.Ready + SecretsManager.Ready) plus the
// largest values surface of the three platform reconcilers.
type SessionManagerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// HelmClientFor returns a Helm client scoped to the given
	// namespace. Production: REST-config-backed. Envtest: in-memory.
	HelmClientFor func(namespace string) (*helm.Client, error)
}

// +kubebuilder:rbac:groups=platform.educates.dev,resources=sessionmanagers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.educates.dev,resources=sessionmanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.educates.dev,resources=sessionmanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.educates.dev,resources=secretsmanagers,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.educates.dev,resources=lookupservices,verbs=get;list;watch

// Reconcile drives a SessionManager CR through its lifecycle.
func (r *SessionManagerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	obj := &platformv1alpha1.SessionManager{}
	if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	log.V(1).Info("Reconciling SessionManager")

	if !obj.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(obj, finalizerSessionManager) {
			r.markSMPhase(obj, platformv1alpha1.ComponentPhaseUninstalling)
			if err := r.updateSMStatusWithTransitionLog(ctx, obj); err != nil {
				return ctrl.Result{}, err
			}
			if err := r.cleanupSM(ctx); err != nil {
				return ctrl.Result{}, err
			}
			// See SecretsManager rationale: status update above leaves
			// the local obj stale; re-Get under RetryOnConflict.
			if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				live := &platformv1alpha1.SessionManager{}
				if err := r.Get(ctx, req.NamespacedName, live); err != nil {
					return client.IgnoreNotFound(err)
				}
				if !controllerutil.ContainsFinalizer(live, finalizerSessionManager) {
					return nil
				}
				controllerutil.RemoveFinalizer(live, finalizerSessionManager)
				return r.Update(ctx, live)
			}); err != nil {
				return ctrl.Result{}, fmt.Errorf("remove finalizer: %w", err)
			}
		}
		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(obj, finalizerSessionManager) {
		controllerutil.AddFinalizer(obj, finalizerSessionManager)
		if err := r.Update(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("add finalizer: %w", err)
		}
		if err := r.Get(ctx, req.NamespacedName, obj); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Gate 1: EducatesClusterConfig.Ready
	cfg, ready, err := r.clusterConfigReadySM(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read EducatesClusterConfig: %w", err)
	}
	if !ready {
		r.markSMClusterConfigAvailable(obj, metav1.ConditionFalse, "ClusterConfigNotReady",
			"EducatesClusterConfig 'cluster' is not yet Ready; waiting")
		r.markSMReady(obj, metav1.ConditionFalse, "WaitingForClusterConfig",
			"EducatesClusterConfig 'cluster' must reach Ready before session-manager can install")
		r.markSMPhase(obj, platformv1alpha1.ComponentPhasePending)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}
	r.markSMClusterConfigAvailable(obj, metav1.ConditionTrue, "ClusterConfigReady",
		"EducatesClusterConfig 'cluster' is Ready")

	// session-manager additionally needs the cluster ingress contract
	// (TLS Secret, ingress class) to render its runtime config.
	if cfg.Status.Ingress == nil {
		r.markSMReady(obj, metav1.ConditionFalse, "MissingIngressContract",
			"EducatesClusterConfig.status.ingress is not populated; waiting")
		r.markSMPhase(obj, platformv1alpha1.ComponentPhasePending)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}

	// Gate 2: SecretsManager.Ready. session-manager relies on
	// secrets-manager's SecretCopier+SecretInjector controllers to
	// propagate pull secrets and TLS into workshop namespaces; we
	// refuse to install until it's healthy.
	smReady, err := r.secretsManagerReady(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read SecretsManager: %w", err)
	}
	if !smReady {
		r.markSMSecretsManagerAvailable(obj, metav1.ConditionFalse, "SecretsManagerNotReady",
			"SecretsManager 'cluster' is not yet Ready; waiting")
		r.markSMReady(obj, metav1.ConditionFalse, "WaitingForSecretsManager",
			"SecretsManager 'cluster' must reach Ready before session-manager can install")
		r.markSMPhase(obj, platformv1alpha1.ComponentPhasePending)
		return ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}
	r.markSMSecretsManagerAvailable(obj, metav1.ConditionTrue, "SecretsManagerReady",
		"SecretsManager 'cluster' is Ready")

	// Reserved-but-unsupported spec surface is rejected explicitly,
	// never silently discarded — same convention as the cluster
	// config's "not yet supported in v1alpha1" providers. A spec
	// change re-triggers reconcile via the generation watch.
	if err := validateSessionManagerSpec(obj); err != nil {
		r.markSMReady(obj, metav1.ConditionFalse, "ValidationFailed", err.Error())
		r.markSMPhase(obj, platformv1alpha1.ComponentPhaseDegraded)
		obj.Status.ObservedGeneration = obj.Generation
		return ctrl.Result{}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}

	r.markSMPhase(obj, platformv1alpha1.ComponentPhaseInstalling)
	res, err := r.installOrUpgradeSM(ctx, obj, cfg)
	if err != nil {
		r.markSMDeployed(obj, metav1.ConditionFalse, "InstallFailed", err.Error())
		r.markSMReady(obj, metav1.ConditionFalse, "InstallFailed", err.Error())
		_ = r.updateSMStatusWithTransitionLog(ctx, obj)
		return ctrl.Result{}, fmt.Errorf("helm install session-manager: %w", err)
	}
	if proceed, result, err := handlePlatformReleaseResult("session-manager", res,
		func(reason, message string) {
			r.markSMDeployed(obj, metav1.ConditionFalse, reason, message)
			r.markSMReady(obj, metav1.ConditionFalse, reason, message)
		},
		func(phase platformv1alpha1.ComponentPhase) { r.markSMPhase(obj, phase) },
		func() error { return r.updateSMStatusWithTransitionLog(ctx, obj) },
	); !proceed {
		return result, err
	}
	r.markSMDeployed(obj, metav1.ConditionTrue, "ChartInstalled",
		fmt.Sprintf("session-manager chart %s installed in namespace %s",
			vendoredcharts.SessionManagerChartVersion, platformNamespace))

	avail, err := r.deploymentAvailableSM(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("read session-manager Deployment: %w", err)
	}
	if !avail {
		r.markSMReady(obj, metav1.ConditionFalse, "WaitingForDeployment",
			"session-manager Deployment not yet Available")
		r.markSMPhase(obj, platformv1alpha1.ComponentPhaseInstalling)
		return ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}

	obj.Status.InstalledVersion = vendoredcharts.SessionManagerChartVersion
	obj.Status.DeploymentRef = &platformv1alpha1.NamespacedRef{
		Namespace: platformNamespace,
		Name:      sessionManagerDeploymentName,
	}

	// Optional extras: node-ca-injector and remote-access. These ride
	// on the main install lifecycle but each has its own tri-state
	// (Auto|Enabled|Disabled) and prerequisite check. Refuse outcomes
	// (Enabled + missing prerequisite) demote the aggregate Ready to
	// False so the user notices their misconfiguration; Skip /
	// Install outcomes leave Ready=True.
	nctIntent, nctReason, nctMessage := resolveNodeCATrust(obj, cfg)
	nctOut, err := r.reconcileExtra(ctx, obj,
		conditionNodeCATrustDeployed,
		nodeCAInjectorReleaseName,
		vendoredcharts.NodeCAInjector,
		renderNodeCAInjectorValues,
		cfg, nctIntent, nctReason, nctMessage,
	)
	if err != nil {
		r.markSMReady(obj, metav1.ConditionFalse, "ExtrasFailed", err.Error())
		r.markSMPhase(obj, platformv1alpha1.ComponentPhaseDegraded)
		_ = r.updateSMStatusWithTransitionLog(ctx, obj)
		return ctrl.Result{}, err
	}

	raIntent, raReason, raMessage, err := r.resolveRemoteAccess(ctx, obj)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve remoteAccess intent: %w", err)
	}
	raOut, err := r.reconcileExtra(ctx, obj,
		conditionRemoteAccessDeployed,
		remoteAccessReleaseName,
		vendoredcharts.RemoteAccess,
		renderRemoteAccessValues,
		cfg, raIntent, raReason, raMessage,
	)
	if err != nil {
		r.markSMReady(obj, metav1.ConditionFalse, "ExtrasFailed", err.Error())
		r.markSMPhase(obj, platformv1alpha1.ComponentPhaseDegraded)
		_ = r.updateSMStatusWithTransitionLog(ctx, obj)
		return ctrl.Result{}, err
	}

	// A mid-repair rollback on either extra needs a requeue to drive its
	// follow-up upgrade — neither the CR nor the component Deployment emits
	// a watch event for it.
	requeueAfter := max(nctOut.requeueAfter, raOut.requeueAfter)

	// Refuse outcomes (user wrote Mode=Enabled but the prerequisite is
	// missing) and not-ready outcomes (an extra is wanted but its release is
	// failed or mid-repair) both downgrade the aggregate Ready so the problem
	// is surfaced even though the main install succeeded. A Refuse keeps the
	// existing reason; a failed/repairing release reports ExtraNotReady.
	refused := nctIntent == intentRefuse || raIntent == intentRefuse
	if refused || nctOut.notReady || raOut.notReady {
		reason, message := "ExtraNotReady",
			"one or more optional extras is not ready (failed or mid-repair release); see per-component conditions"
		if refused {
			reason, message = "ExtraRefused",
				"one or more optional extras is Mode=Enabled with a missing prerequisite; see per-component conditions"
		}
		r.markSMReady(obj, metav1.ConditionFalse, reason, message)
		r.markSMPhase(obj, platformv1alpha1.ComponentPhaseDegraded)
		obj.Status.ObservedGeneration = obj.Generation
		return ctrl.Result{RequeueAfter: requeueAfter}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}

	r.markSMReady(obj, metav1.ConditionTrue, "SessionManagerReady",
		"session-manager is installed and Available")
	r.markSMPhase(obj, platformv1alpha1.ComponentPhaseReady)
	obj.Status.ObservedGeneration = obj.Generation
	return ctrl.Result{RequeueAfter: requeueAfter}, r.updateSMStatusWithTransitionLog(ctx, obj)
}

func (r *SessionManagerReconciler) clusterConfigReadySM(ctx context.Context) (*configv1alpha1.EducatesClusterConfig, bool, error) {
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

// secretsManagerReady fetches the SecretsManager singleton and reports
// whether its aggregate Ready condition is True. NotFound is treated
// as "not ready, may appear later" — same shape as the cluster config
// gate.
func (r *SessionManagerReconciler) secretsManagerReady(ctx context.Context) (bool, error) {
	sm := &platformv1alpha1.SecretsManager{}
	if err := r.Get(ctx, types.NamespacedName{Name: singletonName}, sm); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}
	cond := meta.FindStatusCondition(sm.Status.Conditions, conditionReady)
	return cond != nil && cond.Status == metav1.ConditionTrue, nil
}

func (r *SessionManagerReconciler) installOrUpgradeSM(ctx context.Context, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) (helm.Result, error) {
	if err := ensurePlatformNamespace(ctx, r.Client); err != nil {
		return helm.Result{}, err
	}
	chrt, err := vendoredcharts.SessionManager()
	if err != nil {
		return helm.Result{}, fmt.Errorf("load embedded chart: %w", err)
	}
	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		return helm.Result{}, fmt.Errorf("build helm client: %w", err)
	}
	return hc.EnsureRelease(ctx, sessionManagerReleaseName, chrt, renderSessionManagerValues(obj, cfg))
}

// renderSessionManagerValues maps SessionManagerSpec + cluster config
// status into the session-manager subchart's values shape.
//
// Scoped to fields the v1alpha1 CRD exposes today. The richer surface
// the subchart accepts (themes content, image-cache wiring, registry
// mirrors, default access credentials, image puller daemonset) is
// captured as follow-ups so the gaps don't get lost.
//
// Stable across `helm upgrade`: the session-manager chart's own
// `resolvedTrainingPortal` helper (see installer/charts/.../charts/
// session-manager/templates/_helpers.tpl) reads back any prior
// generated credentials from the live `educates-config` Secret via
// `helm lookup`, so passwords don't rotate on every reconcile.
func renderSessionManagerValues(obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) map[string]any {
	values := map[string]any{}

	applySMImageValues(values, obj, cfg)
	applySMIngressValues(values, obj, cfg)
	applySMSecurityValues(values, obj, cfg)
	applySMPackageDeliveryValues(values, cfg)
	applySMSessionValues(values, obj)
	applySMAnalyticsValues(values, obj)
	applySMStylingValues(values, obj)

	// imagePrePuller — toggle only. When enabled with no explicit
	// image list, the chart derives the v3-equivalent default
	// (training-portal + base-environment) from its imageVersions
	// inventory, so relocation and per-name overrides are honoured.
	// Per-image control stays a chart-level concern; the CRD exposes
	// just the switch.
	if obj.Spec.ImagePrePuller != nil {
		imagePrePullerValues(values)["enabled"] = obj.Spec.ImagePrePuller.Enabled
	}

	// DefaultAccessCredentials and RegistryMirrors are reserved in the
	// CRD; validateSessionManagerSpec rejects them as "not yet
	// supported in v1alpha1" until the chart grows their values. See
	// follow-ups.

	// logLevel doesn't have a typed top-level chart value; the runtime
	// reads it from the rendered operator-config Secret. Route through
	// the chart's `config` escape hatch so it lands in the right place
	// without burning a typed field for it pre-v1.
	if obj.Spec.LogLevel != "" {
		values["config"] = map[string]any{
			"logLevel": strings.ToLower(string(obj.Spec.LogLevel)),
		}
	}

	return values
}

// applySMImageValues maps the image-related inputs: the cluster
// config's registry prefix and pull secrets plus the CR's per-image
// overrides.
func applySMImageValues(values map[string]any, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) {
	// development.imageRegistry — split prefix into host + namespace.
	if cfg.Status.ImageRegistry != nil && cfg.Status.ImageRegistry.Prefix != "" {
		host, ns := splitImageRegistryPrefix(cfg.Status.ImageRegistry.Prefix)
		values["development"] = map[string]any{
			"imageRegistry": map[string]any{
				"host":      host,
				"namespace": ns,
			},
		}
	}

	// imagePullSecrets — propagate from cluster config.
	if cfg.Status.ImageRegistry != nil && len(cfg.Status.ImageRegistry.PullSecrets) > 0 {
		refs := make([]any, 0, len(cfg.Status.ImageRegistry.PullSecrets))
		for _, ref := range cfg.Status.ImageRegistry.PullSecrets {
			refs = append(refs, map[string]any{"name": ref.Name})
		}
		values["imagePullSecrets"] = refs
		// secretPropagation.imagePullSecretNames mirrors the same list
		// so the chart's SecretCopier renders fan-out into workshop
		// namespaces. The CRD doesn't distinguish "namespace-local" vs
		// "pre-existing-to-propagate" pull secrets, so we assume all
		// configured pull secrets should propagate. This matches v3
		// carvel behaviour.
		names := make([]any, 0, len(cfg.Status.ImageRegistry.PullSecrets))
		for _, ref := range cfg.Status.ImageRegistry.PullSecrets {
			names = append(names, ref.Name)
		}
		values["secretPropagation"] = map[string]any{
			"imagePullSecretNames": names,
		}
	}

	// imageVersions — per-image overrides from CR spec. Three names
	// are not part of the chart's imageVersions inventory and are
	// routed to the dedicated chart values that actually control
	// them: the session-manager chart-pod image (`image`), the pause
	// container (`imagePrePuller.pauseImage`), and node-ca-injector
	// (its own subchart's `image`, handled in
	// renderNodeCAInjectorValues). Everything else flows through the
	// inventory and merges by name on the chart side.
	if obj.Spec.Images != nil && len(obj.Spec.Images.Overrides) > 0 {
		entries := make([]any, 0, len(obj.Spec.Images.Overrides))
		for _, o := range obj.Spec.Images.Overrides {
			switch o.Name {
			case "session-manager":
				repo, tag := splitImageRef(o.Image)
				values["image"] = map[string]any{
					"repository": repo,
					"tag":        tag,
				}
			case "pause-container":
				repo, tag := splitImageRef(o.Image)
				imagePrePullerValues(values)["pauseImage"] = map[string]any{
					"repository": repo,
					"tag":        tag,
				}
			case "node-ca-injector":
				// Consumed by renderNodeCAInjectorValues; meaningless
				// to the session-manager chart's inventory.
			default:
				entries = append(entries, map[string]any{
					"name":  o.Name,
					"image": o.Image,
				})
			}
		}
		if len(entries) > 0 {
			values["imageVersions"] = entries
		}
	}
}

// imagePrePullerValues returns the imagePrePuller map inside values,
// creating it when absent, so the pause-image override router and the
// enabled-toggle writer compose instead of clobbering each other.
func imagePrePullerValues(values map[string]any) map[string]any {
	if m, ok := values["imagePrePuller"].(map[string]any); ok {
		return m
	}
	m := map[string]any{}
	values["imagePrePuller"] = m
	return m
}

// applySMIngressValues maps the cluster ingress contract (TLS + CA
// refs from cluster config status) with optional per-SessionManager
// override of the Secret name. Overrides resolve against the cluster-
// config-published namespace; the chart's auto-SecretCopier handles
// cross-namespace placement.
func applySMIngressValues(values map[string]any, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) {
	// wildcardNS is the cluster-config-published namespace for ingress
	// secrets; both it and tlsName are empty when there is no in-cluster
	// wildcard certificate (None provider), which the chart reads as
	// "render plain ingresses, no TLS block".
	wildcardNS := ""
	tlsName := ""
	if wc := cfg.Status.Ingress.WildcardCertificateSecretRef; wc != nil {
		wildcardNS = wc.Namespace
		tlsName = wc.Name
	}
	if obj.Spec.IngressOverrides != nil && obj.Spec.IngressOverrides.TLSSecretRef != nil {
		tlsName = obj.Spec.IngressOverrides.TLSSecretRef.Name
	}
	clusterIngress := map[string]any{
		"domain":            cfg.Status.Ingress.Domain,
		"class":             cfg.Status.Ingress.IngressClassName,
		"tlsCertificateRef": map[string]any{"name": tlsName, "namespace": wildcardNS},
	}
	caRef := map[string]any{"name": "", "namespace": ""}
	if cfg.Status.Ingress.CACertificateSecretRef != nil {
		caRef = map[string]any{
			"name":      cfg.Status.Ingress.CACertificateSecretRef.Name,
			"namespace": cfg.Status.Ingress.CACertificateSecretRef.Namespace,
		}
	}
	if obj.Spec.IngressOverrides != nil && obj.Spec.IngressOverrides.CACertificateSecretRef != nil {
		caRef = map[string]any{
			"name":      obj.Spec.IngressOverrides.CACertificateSecretRef.Name,
			"namespace": wildcardNS,
		}
	}
	clusterIngress["caCertificateRef"] = caRef
	// Publish the resolved scheme from the cluster-config contract; a
	// per-SessionManager override still wins when set.
	protocol := string(cfg.Status.Ingress.Protocol)
	if obj.Spec.IngressOverrides != nil && obj.Spec.IngressOverrides.Protocol != "" {
		protocol = obj.Spec.IngressOverrides.Protocol
	}
	if protocol != "" {
		clusterIngress["protocol"] = protocol
	}
	values["clusterIngress"] = clusterIngress
}

// applySMSecurityValues maps the policy engines from cluster config
// status, with the CR's optional workshop-engine override.
func applySMSecurityValues(values map[string]any, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) {
	if cfg.Status.PolicyEnforcement == nil {
		return
	}
	if cfg.Status.PolicyEnforcement.ClusterPolicyEngine != "" {
		values["clusterSecurity"] = map[string]any{
			"policyEngine": string(cfg.Status.PolicyEnforcement.ClusterPolicyEngine),
		}
	}
	if cfg.Status.PolicyEnforcement.WorkshopPolicyEngine != "" {
		engine := string(cfg.Status.PolicyEnforcement.WorkshopPolicyEngine)
		if obj.Spec.WorkshopPolicyOverride != nil && obj.Spec.WorkshopPolicyOverride.Engine != "" {
			engine = string(obj.Spec.WorkshopPolicyOverride.Engine)
		}
		values["workshopSecurity"] = map[string]any{
			"rulesEngine": engine,
		}
	}
}

// applySMPackageDeliveryValues maps the delivery the cluster config
// resolved for extension packages. The session manager reads the
// resolved value rather than deciding for itself, so a cluster which
// cannot mount package images fetches their contents instead.
func applySMPackageDeliveryValues(values map[string]any, cfg *configv1alpha1.EducatesClusterConfig) {
	if cfg.Status.PackageDelivery == nil || cfg.Status.PackageDelivery.ImageMount == "" {
		return
	}

	values["packageDelivery"] = map[string]any{
		"imageMount": strings.ToLower(string(cfg.Status.PackageDelivery.ImageMount)),
	}
}

// applySMSessionValues maps the per-session runtime knobs: cookies,
// storage, network blocks, and the docker daemon MTU.
func applySMSessionValues(values map[string]any, obj *platformv1alpha1.SessionManager) {
	// sessionCookies.domain — empty defaults to the ingress domain in
	// the runtime (handled inside the chart helpers).
	if obj.Spec.SessionCookieDomain != "" {
		values["sessionCookies"] = map[string]any{
			"domain": obj.Spec.SessionCookieDomain,
		}
	}

	// clusterStorage — pass through directly.
	if obj.Spec.Storage != nil {
		storage := map[string]any{}
		if obj.Spec.Storage.StorageClass != "" {
			storage["class"] = obj.Spec.Storage.StorageClass
		}
		if obj.Spec.Storage.StorageGroup != nil {
			storage["group"] = *obj.Spec.Storage.StorageGroup
		}
		if obj.Spec.Storage.StorageUser != nil {
			storage["user"] = *obj.Spec.Storage.StorageUser
		}
		if len(storage) > 0 {
			values["clusterStorage"] = storage
		}
	}

	// clusterNetwork.blockCIDRs — from CR (empty means "leave chart
	// defaults" which is the AWS IMDS block).
	if obj.Spec.Network != nil && len(obj.Spec.Network.BlockedCIDRs) > 0 {
		entries := make([]any, 0, len(obj.Spec.Network.BlockedCIDRs))
		for _, c := range obj.Spec.Network.BlockedCIDRs {
			entries = append(entries, c)
		}
		values["clusterNetwork"] = map[string]any{
			"blockCIDRs": entries,
		}
	}

	// dockerDaemon.networkMTU — from spec.network.packetSize (the same
	// concept, named differently).
	if obj.Spec.Network != nil && obj.Spec.Network.PacketSize != nil {
		values["dockerDaemon"] = map[string]any{
			"networkMTU": *obj.Spec.Network.PacketSize,
		}
	}
}

// applySMAnalyticsValues maps the three named analytics providers and
// the webhook receiver.
func applySMAnalyticsValues(values map[string]any, obj *platformv1alpha1.SessionManager) {
	if obj.Spec.Tracking == nil {
		return
	}
	analytics := map[string]any{}
	if obj.Spec.Tracking.GoogleAnalytics != nil {
		analytics["google"] = map[string]any{
			"trackingId": obj.Spec.Tracking.GoogleAnalytics.TrackingID,
		}
	}
	if obj.Spec.Tracking.Clarity != nil {
		analytics["clarity"] = map[string]any{
			"trackingId": obj.Spec.Tracking.Clarity.TrackingID,
		}
	}
	if obj.Spec.Tracking.Amplitude != nil {
		analytics["amplitude"] = map[string]any{
			"trackingId": obj.Spec.Tracking.Amplitude.TrackingID,
		}
	}
	if obj.Spec.Tracking.Webhook != nil {
		analytics["webhook"] = map[string]any{
			"url": obj.Spec.Tracking.Webhook.URL,
		}
	}
	if len(analytics) > 0 {
		values["workshopAnalytics"] = analytics
	}
}

// applySMStylingValues maps the CSP frame-ancestors allow-list plus
// Secret-sourced themes. validateSessionManagerSpec has already
// rejected non-Secret theme sources and unknown defaultTheme names,
// so this mapping only sees the supported shape.
func applySMStylingValues(values map[string]any, obj *platformv1alpha1.SessionManager) {
	styling := map[string]any{}
	if len(obj.Spec.AllowedEmbeddingHosts) > 0 {
		hosts := make([]any, 0, len(obj.Spec.AllowedEmbeddingHosts))
		for _, h := range obj.Spec.AllowedEmbeddingHosts {
			hosts = append(hosts, h)
		}
		styling["frameAncestors"] = hosts
	}
	if len(obj.Spec.Themes) > 0 {
		refs := make([]any, 0, len(obj.Spec.Themes))
		for _, t := range obj.Spec.Themes {
			refs = append(refs, map[string]any{
				"name":      t.Source.SecretRef.Name,
				"namespace": t.Source.SecretRef.Namespace,
			})
		}
		styling["themeDataRefs"] = refs
		// The chart keys themes by Secret name; translate the
		// CR-level theme name to its backing Secret.
		if obj.Spec.DefaultTheme != "" {
			for _, t := range obj.Spec.Themes {
				if t.Name == obj.Spec.DefaultTheme {
					styling["defaultTheme"] = t.Source.SecretRef.Name
					break
				}
			}
		}
	}
	if len(styling) > 0 {
		values["websiteStyling"] = styling
	}
}

// validateSessionManagerSpec enforces the v1alpha1 support envelope on
// reserved spec surface. Anything listed here is shaped in the CRD but
// not yet implemented end-to-end; setting it gets an explicit
// field-specific refusal (Ready=False reason=ValidationFailed) instead
// of a silent discard.
func validateSessionManagerSpec(obj *platformv1alpha1.SessionManager) error {
	if obj.Spec.DefaultAccessCredentials != nil {
		return fmt.Errorf("spec.defaultAccessCredentials: not yet supported in v1alpha1")
	}
	if len(obj.Spec.RegistryMirrors) > 0 {
		return fmt.Errorf("spec.registryMirrors: not yet supported in v1alpha1")
	}
	themeNames := make(map[string]bool, len(obj.Spec.Themes))
	for i, t := range obj.Spec.Themes {
		if themeNames[t.Name] {
			return fmt.Errorf("spec.themes[%d].name: duplicate theme name %q", i, t.Name)
		}
		themeNames[t.Name] = true
		switch t.Source.Type {
		case platformv1alpha1.ThemeSourceTypeSecret:
			if t.Source.SecretRef == nil {
				return fmt.Errorf("spec.themes[%d].source.secretRef: required when type is Secret", i)
			}
		default:
			return fmt.Errorf("spec.themes[%d].source.type: %q is not yet supported in v1alpha1 (Secret only)", i, t.Source.Type)
		}
	}
	if obj.Spec.DefaultTheme != "" && !themeNames[obj.Spec.DefaultTheme] {
		return fmt.Errorf("spec.defaultTheme: no entry named %q in spec.themes", obj.Spec.DefaultTheme)
	}
	return nil
}

func (r *SessionManagerReconciler) deploymentAvailableSM(ctx context.Context) (bool, error) {
	dep := &appsv1.Deployment{}
	key := types.NamespacedName{Namespace: platformNamespace, Name: sessionManagerDeploymentName}
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

func (r *SessionManagerReconciler) cleanupSM(ctx context.Context) error {
	_ = ctx
	log := logf.FromContext(ctx)
	// Drain optional extras first (reverse install order). Quiet
	// uninstalls so a leftover extra release can't block the main
	// release's cleanup — the user's interest on finalizer drain is
	// "session-manager is gone".
	r.uninstallExtraQuietly(log, remoteAccessReleaseName)
	r.uninstallExtraQuietly(log, nodeCAInjectorReleaseName)

	hc, err := r.HelmClientFor(platformNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for cleanup: %w", err)
	}
	if err := hc.Uninstall(sessionManagerReleaseName); err != nil {
		return fmt.Errorf("uninstall release: %w", err)
	}
	return nil
}

// --- Status helpers -------------------------------------------------

func (r *SessionManagerReconciler) markSMReady(obj *platformv1alpha1.SessionManager, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *SessionManagerReconciler) markSMClusterConfigAvailable(obj *platformv1alpha1.SessionManager, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionClusterConfigAvailable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *SessionManagerReconciler) markSMSecretsManagerAvailable(obj *platformv1alpha1.SessionManager, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionSecretsManagerAvailable,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *SessionManagerReconciler) markSMDeployed(obj *platformv1alpha1.SessionManager, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionDeployed,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
}

func (r *SessionManagerReconciler) markSMPhase(obj *platformv1alpha1.SessionManager, phase platformv1alpha1.ComponentPhase) {
	obj.Status.Phase = phase
}

func (r *SessionManagerReconciler) updateSMStatusWithTransitionLog(ctx context.Context, obj *platformv1alpha1.SessionManager) error {
	log := logf.FromContext(ctx)
	desiredReady := meta.FindStatusCondition(obj.Status.Conditions, conditionReady)
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		live := &platformv1alpha1.SessionManager{}
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
			log.Info("SessionManager Ready transition",
				"status", desiredReady.Status, "reason", desiredReady.Reason,
				"message", desiredReady.Message)
		}
		return nil
	})
}

// --- Watch wiring ---------------------------------------------------

// SetupWithManager configures the SessionManager controller. Watches:
//   - SessionManager (For target, GenerationChangedPredicate).
//   - EducatesClusterConfig (cross-CR gate 1).
//   - SecretsManager (cross-CR gate 2).
//   - LookupService (Auto-mode signal for remoteAccess: presence
//     of a LookupService CR causes Auto to install remote-access).
//   - apps/v1 Deployment, narrowed to platform-ns + session-manager.
func (r *SessionManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&platformv1alpha1.SessionManager{},
			builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&configv1alpha1.EducatesClusterConfig{},
			handler.EnqueueRequestsFromMapFunc(mapClusterConfigToSessionManager)).
		Watches(&platformv1alpha1.SecretsManager{},
			handler.EnqueueRequestsFromMapFunc(mapSecretsManagerToSessionManager)).
		Watches(&platformv1alpha1.LookupService{},
			handler.EnqueueRequestsFromMapFunc(mapLookupServiceToSessionManager)).
		Watches(&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(mapSessionManagerDeployment)).
		Named("platform-sessionmanager").
		Complete(r)
}

func mapClusterConfigToSessionManager(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() != configSingletonName {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
}

func mapSecretsManagerToSessionManager(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() != singletonName {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
}

// mapLookupServiceToSessionManager re-enqueues SessionManager when
// the singleton LookupService CR appears or disappears. The remote-
// access Auto signal tracks LookupService presence, so this is the
// trigger that lets a `kubectl apply -f lookupservice.yaml` or a
// `kubectl delete lookupservice cluster` propagate to remote-access
// install/uninstall without waiting for the SessionManager's own
// periodic resync.
func mapLookupServiceToSessionManager(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetName() != singletonName {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
}

func mapSessionManagerDeployment(_ context.Context, obj client.Object) []reconcile.Request {
	if obj.GetNamespace() != platformNamespace || obj.GetName() != sessionManagerDeploymentName {
		return nil
	}
	return []reconcile.Request{{NamespacedName: types.NamespacedName{Name: singletonName}}}
}
