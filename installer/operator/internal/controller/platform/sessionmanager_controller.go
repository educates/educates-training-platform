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
	"k8s.io/client-go/discovery"
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
	"github.com/educates/educates-training-platform/installer/operator/internal/kyverno"
	"github.com/educates/educates-training-platform/installer/operator/internal/platformnamespace"
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

	// conditionKyvernoAvailable is the third gate. Status=True with
	// reason `KyvernoServed`, or `NotRequired` when no effective policy
	// engine is Kyverno; Status=False with reason `KyvernoNotFound` while
	// a Kyverno engine has no Kyverno to enforce it.
	conditionKyvernoAvailable = "KyvernoAvailable"
)

// SessionManagerReconciler drives the SessionManager CR. Two cross-CR
// gates (EducatesClusterConfig.Ready + SecretsManager.Ready) plus the
// largest values surface of the three platform reconcilers.
type SessionManagerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	// APIReader is an uncached reader for the CA override's Secret,
	// which may live in a namespace outside the operator's Secret
	// cache. Production wiring uses mgr.GetAPIReader().
	APIReader client.Reader

	// OperatorNamespace is where the reconciler writes the key-free copy
	// of the CA override, and where an override Secret lives when its
	// reference names no namespace. Sourced from the OPERATOR_NAMESPACE
	// env var (downward API).
	OperatorNamespace string

	// HelmClientFor returns a Helm client scoped to the given
	// namespace. Production: REST-config-backed. Envtest: in-memory.
	HelmClientFor func(namespace string) (*helm.Client, error)

	// KyvernoDiscovery answers whether the cluster serves the Kyverno
	// policy kinds session-manager creates. SetupWithManager builds an
	// uncached discovery client when it is unset; tests set it because
	// envtest serves no Kyverno.
	KyvernoDiscovery kyverno.GroupVersionDiscovery
}

// +kubebuilder:rbac:groups=platform.educates.dev,resources=sessionmanagers,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=platform.educates.dev,resources=sessionmanagers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=platform.educates.dev,resources=sessionmanagers/finalizers,verbs=update
// +kubebuilder:rbac:groups=platform.educates.dev,resources=secretsmanagers,verbs=get;list;watch
// +kubebuilder:rbac:groups=platform.educates.dev,resources=lookupservices,verbs=get;list;watch

// Secrets: read the CA override's Secret in any namespace and write its
// key-free copy, educates-session-manager-ingress-ca, in the operator
// namespace.
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch;create;update;patch;delete

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

	// Gate 2: SecretsManager.Ready, running the secrets-manager chart this
	// operator embeds.
	if proceed, result, err := r.gateOnSecretsManager(ctx, obj); !proceed {
		return result, err
	}

	// Gate 3: Kyverno, whenever an effective policy engine is Kyverno.
	if proceed, result, err := r.gateOnKyverno(ctx, obj, cfg); !proceed {
		return result, err
	}

	if proceed, result, err := r.prepareInstall(ctx, obj); !proceed {
		return result, err
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
		func(sm *platformv1alpha1.SessionManager, cc *configv1alpha1.EducatesClusterConfig, imageRegistryNamespace string) map[string]any {
			return renderNodeCAInjectorValues(sm, cc, r.OperatorNamespace, imageRegistryNamespace)
		},
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

func (r *SessionManagerReconciler) installOrUpgradeSM(ctx context.Context, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) (helm.Result, error) {
	if err := platformnamespace.Ensure(ctx, r.Client); err != nil {
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
	return convergeRelease(ctx, r.Client, hc, sessionManagerReleaseName, chrt,
		renderSessionManagerValues(obj, cfg, r.OperatorNamespace, chartImageRegistryNamespace(chrt)))
}

// renderSessionManagerValues maps SessionManagerSpec + cluster config
// status into the session-manager subchart's values shape.
// operatorNamespace locates the key-free copy of a CA override.
//
// Maps only the fields the v1alpha1 CRD exposes. A chart value no CRD
// field maps to, such as the inline website styling content, the
// training portal credentials or the Docker daemon's proxy cache, keeps
// the chart's default.
//
// Stable across `helm upgrade`: the session-manager chart's own
// `resolvedTrainingPortal` helper (see installer/charts/.../charts/
// session-manager/templates/_helpers.tpl) reads back any prior
// generated credentials from the live `educates-config` Secret via
// `helm lookup`, so passwords don't rotate on every reconcile.
//
// imageRegistryNamespace is the chart's own image registry namespace, kept
// under the cluster config's image registry prefix.
func renderSessionManagerValues(obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig, operatorNamespace, imageRegistryNamespace string) map[string]any {
	values := map[string]any{}

	applySMImageValues(values, obj, cfg, imageRegistryNamespace)
	applySMIngressValues(values, obj, cfg, operatorNamespace)
	applySMSecurityValues(values, obj, cfg)
	applySMPackageDeliveryValues(values, cfg)
	applySMSessionValues(values, obj)
	applySMAnalyticsValues(values, obj)
	applySMStylingValues(values, obj)

	if obj.Spec.ImagePrePuller != nil {
		applySMImagePrePullerValues(imagePrePullerValues(values), obj.Spec.ImagePrePuller)
	}

	// DefaultAccessCredentials and RegistryMirrors are reserved in the
	// CRD; validateSessionManagerSpec rejects them as "not yet
	// supported in v1alpha1", since the chart has no values for them.

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

// imagePullSecretsReachSessionNamespaces reports whether the session
// manager passes the image pull Secrets on to workshop session namespaces,
// which it does unless spec.imagePullSecretPropagation.sessionNamespaces
// is false.
func imagePullSecretsReachSessionNamespaces(obj *platformv1alpha1.SessionManager) bool {
	if p := obj.Spec.ImagePullSecretPropagation; p != nil && p.SessionNamespaces != nil {
		return *p.SessionNamespaces
	}
	return true
}

// applySMImageValues maps the image-related inputs: the cluster
// config's registry prefix and pull secrets plus the CR's per-image
// overrides. imageRegistryNamespace is the chart's own image registry
// namespace, kept under the prefix.
func applySMImageValues(values map[string]any, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig, imageRegistryNamespace string) {
	if dev := developmentImageRegistryValues(cfg, imageRegistryNamespace); dev != nil {
		values["development"] = dev
		// The prefix itself, for the images a session's vcluster picks,
		// such as CoreDNS, which the image inventory does not pin and
		// which keep their upstream repository path under the prefix.
		values["imageRegistry"] = map[string]any{
			"prefix": cfg.Status.ImageRegistry.Prefix,
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
			"sessionNamespaces":    imagePullSecretsReachSessionNamespaces(obj),
		}
	}

	// imageVersions — per-image overrides from CR spec. Three names
	// are not part of the chart's imageVersions inventory and are
	// routed to the dedicated chart values that actually control
	// them: the session-manager chart-pod image (`image`), the image
	// pre-puller's helper image-puller (`imagePrePuller.image`), and
	// node-ca-injector (its own subchart's `image`, handled in
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
			case "image-puller":
				repo, tag := splitImageRef(o.Image)
				imagePrePullerValues(values)["image"] = map[string]any{
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

// applySMImagePrePullerValues maps the image pre-puller's fields onto the
// chart's imagePrePuller block unchanged. Entries keep their short names:
// the chart resolves them through its image inventory, and fails the
// render on a name it does not hold (ADR 0008), so the operator never
// computes an image reference.
func applySMImagePrePullerValues(prePuller map[string]any, spec *platformv1alpha1.ImagePrePuller) {
	prePuller["enabled"] = spec.Enabled
	if spec.IncludeDefaultImages != nil {
		prePuller["includeDefaultImages"] = *spec.IncludeDefaultImages
	}
	if len(spec.Images) > 0 {
		entries := make([]any, 0, len(spec.Images))
		for _, e := range spec.Images {
			entry := map[string]any{}
			if e.Name != "" {
				entry["name"] = e.Name
			}
			if e.Image != "" {
				entry["image"] = e.Image
			}
			entries = append(entries, entry)
		}
		prePuller["images"] = entries
	}
}

// imagePrePullerValues returns the imagePrePuller map inside values,
// creating it when absent, so the image-puller override router and
// applySMImagePrePullerValues compose instead of clobbering each other.
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
// overrides. A TLS override names a Secret in the cluster-config-
// published wildcard namespace. A CA override is never passed on as
// it is: the chart receives the key-free copy the reconciler derives
// from it, educates-session-manager-ingress-ca in operatorNamespace.
// The chart's auto-SecretCopier handles cross-namespace placement.
func applySMIngressValues(values map[string]any, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig, operatorNamespace string) {
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
	if ref := effectiveIngressCARef(obj, cfg, operatorNamespace); ref != nil {
		caRef = map[string]any{"name": ref.Name, "namespace": ref.Namespace}
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

// applySMSecurityValues gives the chart the effective policy engines.
func applySMSecurityValues(values map[string]any, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) {
	engines := effectivePolicyEngines(obj, cfg)
	values["clusterSecurity"] = map[string]any{
		"policyEngine": string(engines.cluster),
	}
	values["workshopSecurity"] = map[string]any{
		"rulesEngine": string(engines.workshop),
	}
}

// policyEngines are the engines session-manager enforces: the cluster
// engine for the bundled cluster-wide policies the chart installs, and
// the workshop engine for the per-workshop policies the runtime creates.
type policyEngines struct {
	cluster  configv1alpha1.ClusterPolicyEngine
	workshop platformv1alpha1.WorkshopPolicyEngine
}

// usesKyverno reports whether session-manager creates Kyverno policies.
func (e policyEngines) usesKyverno() bool {
	return e.cluster == configv1alpha1.ClusterPolicyEngineKyverno ||
		e.workshop == platformv1alpha1.WorkshopPolicyEngineKyverno
}

// effectivePolicyEngines resolves the engines from the cluster config's
// published status, the only input components read, with the workshop
// engine replaced by this SessionManager's override when it sets one.
// An engine the cluster config does not publish takes Kyverno, the
// chart's default and the CRD's.
func effectivePolicyEngines(obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) policyEngines {
	engines := policyEngines{
		cluster:  configv1alpha1.ClusterPolicyEngineKyverno,
		workshop: platformv1alpha1.WorkshopPolicyEngineKyverno,
	}
	if pe := cfg.Status.PolicyEnforcement; pe != nil {
		if pe.ClusterPolicyEngine != "" {
			engines.cluster = pe.ClusterPolicyEngine
		}
		if pe.WorkshopPolicyEngine != "" {
			engines.workshop = platformv1alpha1.WorkshopPolicyEngine(pe.WorkshopPolicyEngine)
		}
	}
	if o := obj.Spec.WorkshopPolicyOverride; o != nil && o.Engine != "" {
		engines.workshop = o.Engine
	}
	return engines
}

// applySMPackageDeliveryValues maps the delivery the cluster config
// resolved for extension packages. The session manager reads the
// resolved value rather than deciding for itself, so a cluster which
// cannot mount package images fetches their contents instead. The
// cluster config settles the mode to Enabled or Disabled; the session
// manager chart takes it as an enabled flag, as its other toggles are.
func applySMPackageDeliveryValues(values map[string]any, cfg *configv1alpha1.EducatesClusterConfig) {
	if cfg.Status.PackageDelivery == nil || cfg.Status.PackageDelivery.ImageMount == "" {
		return
	}

	values["packageDelivery"] = map[string]any{
		"imageMount": map[string]any{
			"enabled": cfg.Status.PackageDelivery.ImageMount == configv1alpha1.EffectiveDeliveryModeEnabled,
		},
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

// gateOnSecretsManager reports whether session-manager may install: the
// SecretsManager singleton must be Ready and run the secrets-manager chart
// this operator embeds. session-manager relies on secrets-manager's
// SecretCopier+SecretInjector controllers to propagate pull secrets and TLS
// into workshop namespaces, and its chart creates resources of those kinds,
// whose CRDs are current only then (see secretsManagerStatus). When it may
// not install, the status is already written.
func (r *SessionManagerReconciler) gateOnSecretsManager(ctx context.Context, obj *platformv1alpha1.SessionManager) (bool, ctrl.Result, error) {
	smReady, smVersion, err := secretsManagerStatus(ctx, r.Client)
	if err != nil {
		return false, ctrl.Result{}, fmt.Errorf("read SecretsManager: %w", err)
	}
	if !smReady {
		r.markSMSecretsManagerAvailable(obj, metav1.ConditionFalse, "SecretsManagerNotReady",
			"SecretsManager 'cluster' is not yet Ready; waiting")
		r.markSMReady(obj, metav1.ConditionFalse, "WaitingForSecretsManager",
			"SecretsManager 'cluster' must reach Ready before session-manager can install")
		r.markSMPhase(obj, platformv1alpha1.ComponentPhasePending)
		return false, ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}
	if smVersion != vendoredcharts.SecretsManagerChartVersion {
		r.markSMSecretsManagerAvailable(obj, metav1.ConditionFalse, "SecretsManagerNotUpgraded",
			fmt.Sprintf("SecretsManager 'cluster' runs secrets-manager chart %q, not %q; waiting for its release to converge",
				smVersion, vendoredcharts.SecretsManagerChartVersion))
		r.markSMReady(obj, metav1.ConditionFalse, "WaitingForSecretsManager",
			"SecretsManager 'cluster' must run the bundled secrets-manager chart before session-manager can install, since session-manager's SecretCopier and SecretInjector resources use the CRDs it ships")
		r.markSMPhase(obj, platformv1alpha1.ComponentPhasePending)
		return false, ctrl.Result{RequeueAfter: 30 * time.Second}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}
	r.markSMSecretsManagerAvailable(obj, metav1.ConditionTrue, "SecretsManagerReady",
		"SecretsManager 'cluster' is Ready")
	return true, ctrl.Result{}, nil
}

// gateOnKyverno refuses to install while an effective policy engine is
// Kyverno and the cluster does not serve the Kyverno policy kinds: the
// chart's bundled cluster-wide policies would fail the install, and the
// runtime's per-workshop policies would fail every workshop environment.
// The cluster config checks its own engines; this gate also covers a
// workshopPolicyOverride, which can select Kyverno on a cluster whose
// workshop engine is None. Nothing the reconciler watches changes when
// Kyverno is installed, so it polls.
func (r *SessionManagerReconciler) gateOnKyverno(ctx context.Context, obj *platformv1alpha1.SessionManager, cfg *configv1alpha1.EducatesClusterConfig) (bool, ctrl.Result, error) {
	if !effectivePolicyEngines(obj, cfg).usesKyverno() {
		r.markSMKyvernoAvailable(obj, metav1.ConditionTrue, "NotRequired",
			"no effective policy engine is Kyverno")
		return true, ctrl.Result{}, nil
	}
	missing, err := kyverno.MissingPolicyKinds(r.KyvernoDiscovery)
	if err != nil {
		return false, ctrl.Result{}, err
	}
	if len(missing) > 0 {
		message := kyverno.NotServedMessage(missing)
		r.markSMKyvernoAvailable(obj, metav1.ConditionFalse, "KyvernoNotFound", message)
		r.markSMReady(obj, metav1.ConditionFalse, "WaitingForKyverno", message)
		r.markSMPhase(obj, platformv1alpha1.ComponentPhasePending)
		return false, ctrl.Result{RequeueAfter: kyverno.PresencePollInterval}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}
	r.markSMKyvernoAvailable(obj, metav1.ConditionTrue, "KyvernoServed",
		"Kyverno serves the policy kinds session-manager creates")
	return true, ctrl.Result{}, nil
}

// prepareInstall runs the steps that must succeed before the chart is
// installed or upgraded, and reports whether the install may proceed.
// When it may not, the status is already written.
func (r *SessionManagerReconciler) prepareInstall(ctx context.Context, obj *platformv1alpha1.SessionManager) (bool, ctrl.Result, error) {
	// Reserved-but-unsupported spec surface is rejected explicitly,
	// never silently discarded — same convention as the cluster
	// config's "not yet supported in v1alpha1" providers. A spec
	// change re-triggers reconcile via the generation watch.
	if err := validateSessionManagerSpec(obj); err != nil {
		r.markSMReady(obj, metav1.ConditionFalse, "ValidationFailed", err.Error())
		r.markSMPhase(obj, platformv1alpha1.ComponentPhaseDegraded)
		obj.Status.ObservedGeneration = obj.Generation
		return false, ctrl.Result{}, r.updateSMStatusWithTransitionLog(ctx, obj)
	}

	// A CA override reaches session-manager only as its key-free copy,
	// derived before every install or upgrade that references it.
	return r.reconcileIngressCAOverride(ctx, obj)
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

func (r *SessionManagerReconciler) markSMKyvernoAvailable(obj *platformv1alpha1.SessionManager, status metav1.ConditionStatus, reason, message string) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionKyvernoAvailable,
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
//   - Secret, narrowed to the CA override's source and its key-free
//     copy. Events arrive only from the namespaces the operator's
//     Secret cache covers.
func (r *SessionManagerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if r.KyvernoDiscovery == nil {
		disc, err := discovery.NewDiscoveryClientForConfig(mgr.GetConfig())
		if err != nil {
			return fmt.Errorf("build discovery client: %w", err)
		}
		r.KyvernoDiscovery = disc
	}
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
		Watches(&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapSecretToSessionManager)).
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
