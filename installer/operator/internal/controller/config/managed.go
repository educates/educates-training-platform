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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// Managed-mode condition types. Each cluster service contributes its
// own condition; the aggregate `Ready` condition flips True only once
// every required one is True. Conditions are only set once their
// producing phase runs — absent != False.
//
// The `*Ready` condition vocabulary matches the CRD design's intent:
// component CRs and humans can read a single condition per concern
// (certificates, ingress, DNS, policy) without scanning a free-form
// reason field on a single aggregate condition.
const (
	conditionCertificatesReady      = "CertificatesReady"
	conditionIngressReady           = "IngressReady"
	conditionDNSReady               = "DNSReady"
	conditionPolicyEnforcementReady = "PolicyEnforcementReady"
)

// Cluster-service install constants. Cert-manager is conventionally
// installed in its own namespace; the operator does not give users a
// knob to relocate it because all known upstream tooling (kubectl
// plugins, dashboards, RBAC defaults) assumes the canonical name.
const (
	certManagerNamespace   = "cert-manager"
	certManagerReleaseName = "cert-manager"
)

// reconcileManaged drives Managed-mode reconciliation as a sequence
// of independent cluster-service phases, each contributing one
// `*Ready` condition. The phases run in install-order; each phase
// gates the next via its return value:
//
//   - done=true   → phase complete; orchestrator proceeds to the next.
//   - done=false  → phase incomplete; orchestrator returns the
//     Result+error verbatim. This covers both
//     "silently waiting on a watch event" (zero Result, nil error)
//     and "explicit RequeueAfter while a transient state clears"
//     (non-zero Result, nil error) and "real error to surface to
//     controller-runtime" (any Result, non-nil error). All three
//     stop the pipeline at the same point.
//
// Phases that aren't required by the spec (e.g., user picked
// ExternalCertManager) return done=true immediately so the
// orchestrator proceeds. Validation that requires the provider mix
// to be supported lives in validateManaged.
//
// Install order:
//
//  1. cert-manager + wildcard Certificate + ClusterIssuer
//     (CertificatesReady). Wildcard Cert placement may shift to
//     post-Contour when ACME-HTTP01 issuer types are added later.
//  2. Contour + IngressClass (IngressReady).
//  3. external-dns (DNSReady).
//  4. Kyverno (PolicyEnforcementReady).
//
// Cleanup is the strict reverse.
func (r *EducatesClusterConfigReconciler) reconcileManaged(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) (ctrl.Result, error) {
	if err := r.validateManaged(ctx, obj); err != nil {
		if verr, ok := errors.AsType[*validationError](err); ok {
			r.markDegraded(obj, verr.Field, verr.Reason)
			return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, obj)
		}
		return ctrl.Result{}, err
	}

	// cert-manager + wildcard certificate.
	if done, res, err := r.reconcileCertManagerPhase(ctx, obj); !done {
		return res, err
	}

	// Ingress controller (Contour).
	if done, res, err := r.reconcileContourPhase(ctx, obj); !done {
		return res, err
	}

	// DNS (external-dns).
	if done, res, err := r.reconcileExternalDNSPhase(ctx, obj); !done {
		return res, err
	}

	// Policy enforcement (Kyverno).
	if done, res, err := r.reconcileKyvernoPhase(ctx, obj); !done {
		return res, err
	}

	r.markManagedReady(obj)
	return ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, obj)
}

// handleCertManagerCRDsMissing handles a NoMatchError (or 404
// "kind not found") on a cert-manager.io kind. The error has two
// possible root causes and we must distinguish them via a fresh
// discovery probe — the operator's local RESTMapper alone can't
// tell us which:
//
//  1. **CRDs really missing.** End-of-life teardown (helm uninstall
//     just removed them) or a user out-of-band `kubectl delete crd`.
//     Surface as a clean Degraded condition with a 60s requeue.
//
//  2. **CRDs present but the operator's RESTMapper is stale.** Most
//     common during Managed-mode install bootstrap: the operator
//     pod was started before cert-manager existed, so its mapper
//     cached "no cert-manager.io group". After helm install lands
//     the CRDs, the mapper doesn't auto-refresh — every typed-client
//     call to a cert-manager kind returns NoMatchError until
//     something invalidates the cache. CRDWatcher.registerWatch
//     does that whenever it activates a watch, but there's a
//     window of up to PollInterval where the SSA path can race
//     ahead. Recovery: Reset the mapper and retry shortly. The
//     condition stays at its prior state (typically Progressing
//     /WaitingForCertManager), so the user doesn't see a
//     spurious Degraded blip.
//
// Why not just always Reset+retry: in the genuinely-missing case,
// resetting the mapper does nothing useful (the next call still
// returns NoMatchError after re-discovery), and the user needs
// the Degraded signal to know to reinstall. Discovery is the
// authoritative test.
//
// Note: this only quiets the operator's *own* error paths. The
// underlying controller-runtime Kind source (registered by
// CRDWatcher when the CRDs were still present) keeps logging a
// retry-loop error at 10s intervals because controller-runtime has
// no public API to remove a registered Source. Captured as a
// follow-up.
func (r *EducatesClusterConfigReconciler) handleCertManagerCRDsMissing(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig, cause error) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	if r.certManagerCRDsActuallyPresent() {
		// Mapper-staleness path. Reset and retry shortly; the user
		// shouldn't see Degraded for a transient bootstrap race.
		log.Info("RESTMapper cache is stale; cert-manager CRDs are present in discovery — resetting mapper and retrying",
			"cause", cause.Error())
		if resetter, ok := r.RESTMapper().(interface{ Reset() }); ok {
			resetter.Reset()
		}
		return ctrl.Result{RequeueAfter: 2 * time.Second}, nil
	}

	log.Info("cert-manager.io CRDs are no longer present in the cluster; operator state is Degraded",
		"cause", cause.Error())
	r.markCertificatesProgressing(obj, "CertManagerCRDsMissing",
		"cert-manager.io CRDs are no longer present in the cluster; reinstall cert-manager or delete this EducatesClusterConfig")
	r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseDegraded)
	if err := r.updateStatusWithTransitionLog(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 60 * time.Second}, nil
}

// certManagerCRDsActuallyPresent does a fresh discovery probe (not
// going through the operator's potentially-stale RESTMapper) to
// check whether cert-manager.io/v1 carries the ClusterIssuer and
// Certificate kinds we'd otherwise mark Degraded for. If the
// Discovery client isn't set (tests that don't wire it), defaults
// to "not present" — the test envtest can register CRDs through
// envtest infra and the local mapper sees them, so this path only
// fires when something actually went wrong.
func (r *EducatesClusterConfigReconciler) certManagerCRDsActuallyPresent() bool {
	if r.Discovery == nil {
		return false
	}
	rl, err := r.Discovery.ServerResourcesForGroupVersion("cert-manager.io/v1")
	if err != nil || rl == nil {
		return false
	}
	var sawClusterIssuer, sawCertificate bool
	for _, res := range rl.APIResources {
		switch res.Kind {
		case "ClusterIssuer":
			sawClusterIssuer = true
		case "Certificate":
			sawCertificate = true
		}
	}
	return sawClusterIssuer && sawCertificate
}

// handleWebhookNotReady surfaces the "cert-manager webhook isn't
// answering yet" failure mode as a clean progressing condition with a
// friendly INFO log line and a short RequeueAfter, suppressing the
// error-return path that would otherwise dump a stack trace at ERROR.
// kind is the resource the operator was trying to apply
// ("ClusterIssuer" or "Certificate") and shows up in the log line so
// the cause is obvious. See certmanager.go::isWebhookNotReadyErr for
// the substring rationale; the proper fix is the synthetic admission
// probe, captured as a follow-up.
func (r *EducatesClusterConfigReconciler) handleWebhookNotReady(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig, kind string, cause error) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	log.Info("cert-manager webhook not yet routable; will retry shortly",
		"kind", kind,
		"cause", cause.Error())
	r.markCertificatesProgressing(obj, "WaitingForWebhook",
		fmt.Sprintf("apply of %s blocked: cert-manager admission webhook not yet serving (cainjector caBundle propagation in flight)", kind))
	r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
	if err := r.updateStatusWithTransitionLog(ctx, obj); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 15 * time.Second}, nil
}

// cleanupManaged tears down installed cluster services in **reverse
// install order**: each per-service cleanup is self-contained and
// no-ops when its corresponding install was skipped (External
// provider variants). Adding a new cluster service means
// appending its cleanup* call at the top of this function (since
// it'll have been the *last* to install).
//
// Cleanups are idempotent — retried reconciles after partial drain
// failure re-attempt only what's still present.
func (r *EducatesClusterConfigReconciler) cleanupManaged(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	// Reverse install order.
	if err := r.cleanupKyverno(ctx, obj); err != nil {
		return err
	}
	if err := r.cleanupExternalDNS(ctx, obj); err != nil {
		return err
	}
	if err := r.cleanupContour(ctx, obj); err != nil {
		return err
	}
	if err := r.cleanupCertManager(ctx, obj); err != nil {
		return err
	}
	return nil
}

// deleteIfPresent issues a Delete and swallows the two error
// classes that mean "already gone from the operator's perspective":
//   - IsNotFound: the named object no longer exists.
//   - IsNoMatchError: the kind itself no longer exists (CRD removed,
//     e.g., after helm uninstall earlier in this same drain pass).
//
// Both states are functionally terminal for finalizer drain — the
// resource we wanted to delete is gone. Returning an error from
// either would block the rest of the drain on something that's
// already in the desired state.
func (r *EducatesClusterConfigReconciler) deleteIfPresent(ctx context.Context, obj client.Object) error {
	if err := r.Delete(ctx, obj); err != nil {
		if apierrors.IsNotFound(err) || isCertManagerCRDMissingErr(err) {
			return nil
		}
		return err
	}
	return nil
}

// markManagedReady publishes the inter-CR ingress contract and flips
// the aggregate Ready condition to True. Called once *every* phase
// (cert-manager, Contour, external-dns, Kyverno) has
// signed off. Each phase is responsible for flipping its own
// per-service condition True before this runs — markManagedReady
// does not touch CertificatesReady/IngressReady/etc., it only sets
// the aggregate.
//
// Mirrors markReady (Inline) but sources the contract from
// cert-manager-issued resources rather than user-declared
// references.
func (r *EducatesClusterConfigReconciler) markManagedReady(obj *configv1alpha1.EducatesClusterConfig) {
	obj.Status.ObservedGeneration = obj.Generation
	obj.Status.Phase = configv1alpha1.ClusterConfigPhaseReady
	obj.Status.Mode = obj.Spec.Mode

	status := &configv1alpha1.StatusIngress{
		Domain:           obj.Spec.Ingress.Domain,
		IngressClassName: obj.Spec.Ingress.IngressClassName,
		Protocol:         resolveIngressProtocol(obj.Spec.Ingress),
	}
	// With a None provider there is no operator-issued wildcard cert or
	// ClusterIssuer to publish; components read the absent ref as "no
	// in-cluster TLS" and render plain ingresses.
	if obj.Spec.Ingress.Certificates.Provider != configv1alpha1.CertificatesProviderNone {
		status.WildcardCertificateSecretRef = &configv1alpha1.NamespacedSecretRef{
			Namespace: r.OperatorNamespace,
			Name:      wildcardTLSSecretName,
		}
		status.ClusterIssuerRef = &configv1alpha1.LocalObjectReference{
			Name: wildcardClusterIssuer,
		}
	}
	// Publish the CustomCA Secret so components can trust ingress
	// endpoints signed by a private CA: the platform reconcilers pass
	// this ref to the component charts, which mount it, and the
	// session-manager hands it on to the training portals and workshop
	// sessions it creates. Mirrors what the Inline path publishes from
	// the user's caCertificateSecretRef. ACME issuers chain to public
	// roots, so there is nothing to publish for them. The namespace
	// defaults the same way checkCustomCASecret resolves it.
	if bcm := obj.Spec.Ingress.Certificates.BundledCertManager; bcm != nil &&
		bcm.IssuerType == configv1alpha1.IssuerTypeCustomCA && bcm.CustomCA != nil {
		ns := bcm.CustomCA.CACertificateRef.Namespace
		if ns == "" {
			ns = r.OperatorNamespace
		}
		status.CACertificateSecretRef = &configv1alpha1.NamespacedSecretRef{
			Namespace: ns,
			Name:      bcm.CustomCA.CACertificateRef.Name,
		}
	}
	obj.Status.Ingress = status

	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionTrue,
		Reason:             "ManagedServicesReady",
		Message:            "Managed-mode cluster services are ready",
		ObservedGeneration: obj.Generation,
	})
}

// resolveIngressProtocol returns the public-URL scheme to publish in
// status.ingress.protocol. An explicit spec.ingress.protocol wins;
// otherwise it derives from the certificates provider: http when the
// provider is None, https when a certificate is provisioned.
func resolveIngressProtocol(ingress *configv1alpha1.Ingress) configv1alpha1.IngressProtocol {
	if ingress.Protocol != "" {
		return ingress.Protocol
	}
	if ingress.Certificates.Provider == configv1alpha1.CertificatesProviderNone {
		return configv1alpha1.IngressProtocolHTTP
	}
	return configv1alpha1.IngressProtocolHTTPS
}

// reconcileCertManager ensures the cert-manager release exists,
// installing from the vendored tarball on first sight. Upgrades on
// chart-version drift are handled here too (a vendored bump produces
// a different chart.Metadata.Version, the Status path notices, and
// Upgrade runs). Resource-level readiness checks (Deployment +
// webhook discovery) are done separately by the phase wrapper.
func (r *EducatesClusterConfigReconciler) reconcileCertManager(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig) (helm.Result, error) {
	chrt, err := vendoredcharts.CertManager()
	if err != nil {
		return helm.Result{}, fmt.Errorf("load embedded cert-manager chart: %w", err)
	}

	if err := r.ensureNamespace(ctx, certManagerNamespace, owner); err != nil {
		return helm.Result{}, err
	}

	hc, err := r.HelmClientFor(certManagerNamespace)
	if err != nil {
		return helm.Result{}, fmt.Errorf("build helm client for %q: %w", certManagerNamespace, err)
	}

	res, err := hc.EnsureRelease(ctx, certManagerReleaseName, chrt, renderCertManagerValues(owner))
	if err != nil {
		return helm.Result{}, err
	}

	if obj := owner; obj != nil {
		if obj.Status.BundledChartVersions == nil {
			obj.Status.BundledChartVersions = map[string]string{}
		}
		obj.Status.BundledChartVersions["cert-manager"] = vendoredcharts.CertManagerVersion
	}
	return res, nil
}

// renderCertManagerValues builds the values map passed to the
// cert-manager chart. Image-registry-prefix rewriting lands alongside
// the rest of the Managed-mode CR fields in later commits; today the
// function only sets values that are needed to make the Helm install
// behave well under operator-driven reconciliation. (cert-manager has
// no operational block: it spans controller + webhook + cainjector
// Deployments, which a single shared shape can't tune — see
// OperationalBlock in the API package.)
//
// crds.enabled=true: cert-manager v1.18+ defaults its CRDs to OFF
// (`crds.enabled: false` in the vendored chart's values.yaml).
// Without this override, helm-install
// succeeds, cert-manager pods come up, the operator's deployment
// readiness gate passes — and then every typed SSA call against
// ClusterIssuer/Certificate returns NoMatchError forever because
// the CRDs were never applied. Opt-in.
//
// crds.keep=false: by default the chart annotates CRDs with
// "helm.sh/resource-policy: keep" so `helm uninstall` leaves them
// in the cluster. For the operator's Managed-mode lifecycle that
// inverts what we want — the EducatesClusterConfig owns the
// cert-manager install end-to-end, and on teardown the user
// expects everything to go away. Setting keep=false makes
// `helm uninstall` cascade-delete the CRDs (which in turn
// cascades to any remaining cert-manager.io resources).
//
// startupapicheck is a post-install Helm hook that pings the
// cert-manager webhook to confirm the API is serving. We disable it
// for two reasons:
//
//  1. The hook's "is the webhook routable?" check duplicates the
//     readiness gate we already run after install
//     (ensureCertManagerReady + the WaitingForWebhook classification
//     on SSA failures). Having both means we pay for the same probe
//     twice on every fresh install.
//
//  2. The hook is wrapped in Helm's WaitStrategy timeout (set
//     explicitly by the helm wrapper — see actionTimeout in
//     internal/helm). If cainjector hasn't injected the caBundle by
//     then, the install returns a hard error and the release is left
//     "failed" — turning a transient bootstrap race into a deadlock
//     that needs a manual `helm uninstall`. With the hook disabled,
//     the install completes fast and the operator's own retry loop
//     converges on its own cadence.
//
// Kept as a standalone function so values-shape changes don't ripple
// through reconcile control flow.
func renderCertManagerValues(obj *configv1alpha1.EducatesClusterConfig) map[string]any {
	values := map[string]any{
		"crds": map[string]any{
			"enabled": true,
			"keep":    false,
		},
		"startupapicheck": map[string]any{
			"enabled": false,
		},
	}

	// ACME DNS01 with identity-based auth needs the cert-manager
	// controller's ServiceAccount to carry a cloud-specific
	// annotation:
	//   - Route53 / IRSA on EKS: `eks.amazonaws.com/role-arn`,
	//     which the kube2iam / IRSA webhook turns into an
	//     AssumeRoleWithWebIdentity flow when cert-manager makes AWS
	//     SDK calls. cert-manager picks up the role from the env
	//     vars the webhook injects.
	//   - CloudDNS / Workload Identity on GKE:
	//     `iam.gke.io/gcp-service-account`, which the metadata
	//     server uses to mint short-lived GCP credentials for the
	//     pod. cert-manager's Google SDK call then uses
	//     Application Default Credentials.
	//
	// We don't ship long-lived static creds in Secrets — the
	// validator rejects credentialsSecretRef as "not yet supported"
	// until a follow-up lands that support.
	if obj != nil && obj.Spec.Ingress != nil &&
		obj.Spec.Ingress.Certificates.BundledCertManager != nil &&
		obj.Spec.Ingress.Certificates.BundledCertManager.IssuerType == configv1alpha1.IssuerTypeACME &&
		obj.Spec.Ingress.Certificates.BundledCertManager.ACME != nil {
		dns01 := obj.Spec.Ingress.Certificates.BundledCertManager.ACME.Solvers.DNS01
		annotations := map[string]any{}
		switch dns01.Provider {
		case configv1alpha1.DNS01ProviderRoute53:
			if dns01.Route53 != nil && dns01.Route53.IAMRoleARN != "" {
				annotations["eks.amazonaws.com/role-arn"] = dns01.Route53.IAMRoleARN
			}
		case configv1alpha1.DNS01ProviderCloudDNS:
			if dns01.CloudDNS != nil && dns01.CloudDNS.WorkloadIdentityServiceAccount != "" {
				annotations["iam.gke.io/gcp-service-account"] = dns01.CloudDNS.WorkloadIdentityServiceAccount
			}
		}
		if len(annotations) > 0 {
			values["serviceAccount"] = map[string]any{
				"annotations": annotations,
			}
		}
	}

	return values
}

// validateManaged runs the Managed-mode checks. The CRD's CEL
// rules already enforce field-presence and mutual-exclusion at admission
// time; this validator covers cross-resource concerns (referenced
// Secrets exist with the right keys) and the not-yet-supported feature
// matrix.
//
// It supports BundledCertManager + CustomCA with BundledContour ingress.
// Other providers/issuer types return explicit validation errors with a
// "not yet supported in v1alpha1" message rather than silently no-oping.
func (r *EducatesClusterConfigReconciler) validateManaged(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	if obj.Spec.Ingress == nil {
		return &validationError{
			Field:  "spec.ingress",
			Reason: "Managed mode requires spec.ingress",
		}
	}

	switch obj.Spec.Ingress.Controller.Provider {
	case configv1alpha1.IngressControllerProviderBundledContour:
		// Supported.
	default:
		return &validationError{
			Field:  "spec.ingress.controller.provider",
			Reason: fmt.Sprintf("provider %q is not yet supported in v1alpha1", obj.Spec.Ingress.Controller.Provider),
		}
	}

	certs := obj.Spec.Ingress.Certificates
	switch certs.Provider {
	case configv1alpha1.CertificatesProviderNone:
		// No in-cluster TLS. The operator installs no cert-manager and
		// issues no certificate; ingress.protocol (validated by CEL)
		// decides the scheme of the public URLs.
	case configv1alpha1.CertificatesProviderBundledCertManager:
		if certs.BundledCertManager == nil {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager",
				Reason: "required when certificates.provider is BundledCertManager",
			}
		}
		switch certs.BundledCertManager.IssuerType {
		case configv1alpha1.IssuerTypeCustomCA:
			if certs.BundledCertManager.CustomCA == nil {
				return &validationError{
					Field:  "spec.ingress.certificates.bundledCertManager.customCA",
					Reason: "required when issuerType is CustomCA",
				}
			}
			if err := r.checkCustomCASecret(ctx, certs.BundledCertManager.CustomCA.CACertificateRef); err != nil {
				return err
			}
		case configv1alpha1.IssuerTypeACME:
			if err := r.validateACMEConfig(certs.BundledCertManager.ACME); err != nil {
				return err
			}
		default:
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.issuerType",
				Reason: fmt.Sprintf("issuerType %q is not yet supported in v1alpha1 (only CustomCA and ACME)", certs.BundledCertManager.IssuerType),
			}
		}
	default:
		return &validationError{
			Field:  "spec.ingress.certificates.provider",
			Reason: fmt.Sprintf("provider %q is not yet supported in v1alpha1 (only BundledCertManager and None)", certs.Provider),
		}
	}

	return nil
}

// checkCustomCASecret validates the CustomCA Secret reference in
// the namespace specified on the reference (defaulting to the operator
// namespace). Mirrors checkCASecret for Inline mode but expects
// tls.crt + tls.key (cert-manager's CA-issuer needs the private key),
// not ca.crt.
func (r *EducatesClusterConfigReconciler) checkCustomCASecret(ctx context.Context, ref configv1alpha1.CASecretReference) error {
	ns := ref.Namespace
	if ns == "" {
		ns = r.OperatorNamespace
	}
	r.warnIfCacheMiss(ctx, ns, "spec.ingress.certificates.bundledCertManager.customCA.caCertificateRef")
	s := &corev1.Secret{}
	key := types.NamespacedName{Namespace: ns, Name: ref.Name}
	// APIReader bypasses the controller-runtime cache, which is only
	// configured to watch Secrets in the operator namespace. Cross-
	// namespace caCertificateRef (laptop flow uses educates-secrets)
	// would otherwise fail with "unknown namespace for the cache".
	if err := r.APIReader.Get(ctx, key, s); err != nil {
		if apierrors.IsNotFound(err) {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.customCA.caCertificateRef",
				Reason: fmt.Sprintf("Secret %s/%s not found", ns, ref.Name),
			}
		}
		return fmt.Errorf("get CustomCA Secret %s: %w", key, err)
	}
	for _, k := range []string{"tls.crt", "tls.key"} {
		if _, ok := s.Data[k]; !ok {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.customCA.caCertificateRef",
				Reason: fmt.Sprintf("Secret %s/%s is missing required key %q", ns, ref.Name, k),
			}
		}
	}
	return nil
}

// validateACMEConfig validates the ACME ClusterIssuer spec for the
// v1alpha1-supported provider set: Route53 (AWS) and CloudDNS (GCP),
// identity-based auth only (IRSA on EKS / Workload Identity on GKE).
// Cloudflare, AzureDNS, HTTP01, and static-credentials Secrets are
// reserved in the schema but rejected here as "not yet supported".
func (r *EducatesClusterConfigReconciler) validateACMEConfig(acme *configv1alpha1.ACMEConfig) error {
	if acme == nil {
		return &validationError{
			Field:  "spec.ingress.certificates.bundledCertManager.acme",
			Reason: "required when issuerType is ACME",
		}
	}
	if acme.Email == "" {
		return &validationError{
			Field:  "spec.ingress.certificates.bundledCertManager.acme.email",
			Reason: "required",
		}
	}
	if acme.Solvers.HTTP01 != nil {
		return &validationError{
			Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.http01",
			Reason: "HTTP01 solver is not yet supported in v1alpha1 (DNS01 only)",
		}
	}
	dns01 := acme.Solvers.DNS01
	switch dns01.Provider {
	case configv1alpha1.DNS01ProviderRoute53:
		if dns01.Route53 == nil {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.route53",
				Reason: "required when provider is Route53",
			}
		}
		if dns01.Route53.HostedZoneID == "" {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.route53.hostedZoneID",
				Reason: "required",
			}
		}
		if dns01.Route53.CredentialsSecretRef != nil {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.route53.credentialsSecretRef",
				Reason: "static-credentials Secret is not yet supported in v1alpha1 (use iamRoleARN with IRSA)",
			}
		}
		if dns01.Route53.IAMRoleARN == "" {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.route53.iamRoleARN",
				Reason: "required (v1alpha1 supports IRSA only)",
			}
		}
	case configv1alpha1.DNS01ProviderCloudDNS:
		if dns01.CloudDNS == nil {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.cloudDNS",
				Reason: "required when provider is CloudDNS",
			}
		}
		if dns01.CloudDNS.Project == "" {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.cloudDNS.project",
				Reason: "required",
			}
		}
		if dns01.CloudDNS.CredentialsSecretRef != nil {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.cloudDNS.credentialsSecretRef",
				Reason: "static-credentials Secret is not yet supported in v1alpha1 (use workloadIdentityServiceAccount)",
			}
		}
		if dns01.CloudDNS.WorkloadIdentityServiceAccount == "" {
			return &validationError{
				Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.cloudDNS.workloadIdentityServiceAccount",
				Reason: "required (v1alpha1 supports Workload Identity only)",
			}
		}
	case "":
		return &validationError{
			Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.provider",
			Reason: "required",
		}
	default:
		return &validationError{
			Field:  "spec.ingress.certificates.bundledCertManager.acme.solvers.dns01.provider",
			Reason: fmt.Sprintf("DNS01 provider %q is not yet supported in v1alpha1 (only Route53 and CloudDNS)", dns01.Provider),
		}
	}
	return nil
}

// markCertificatesProgressing publishes a CertificatesReady=False
// condition while the cert-manager install pipeline is still
// converging. Reason is the kebab-case-ish PascalCase the rest of the
// reconciler uses; message is free-form.
func (r *EducatesClusterConfigReconciler) markCertificatesProgressing(obj *configv1alpha1.EducatesClusterConfig, reason, message string) {
	obj.Status.ObservedGeneration = obj.Generation
	obj.Status.Mode = obj.Spec.Mode
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionCertificatesReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
	// Aggregate Ready also stays False while any sub-condition is False.
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Progressing",
		Message:            "Managed-mode reconciliation in progress",
		ObservedGeneration: obj.Generation,
	})
}

// markCertificatesReadyTrue flips CertificatesReady to True. Called
// once the cert-manager phase has confirmed the wildcard Certificate
// is Ready. Does NOT touch the aggregate Ready condition — that's
// only flipped True in markManagedReady once *every* phase has
// signed off.
func (r *EducatesClusterConfigReconciler) markCertificatesReadyTrue(obj *configv1alpha1.EducatesClusterConfig) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionCertificatesReady,
		Status:             metav1.ConditionTrue,
		Reason:             "CertificateIssued",
		Message:            "wildcard Certificate is Ready",
		ObservedGeneration: obj.Generation,
	})
}

// markCertificatesNotManaged flips CertificatesReady to True for the
// None provider, where Educates serves no in-cluster TLS and there is
// no wildcard Certificate to wait on.
func (r *EducatesClusterConfigReconciler) markCertificatesNotManaged(obj *configv1alpha1.EducatesClusterConfig) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionCertificatesReady,
		Status:             metav1.ConditionTrue,
		Reason:             "CertificatesNotManaged",
		Message:            "certificates provider is None; no in-cluster TLS",
		ObservedGeneration: obj.Generation,
	})
}

// markIngressProgressing publishes an IngressReady=False condition
// while the Contour install pipeline is still converging. Mirrors
// markCertificatesProgressing's shape for the ingress phase.
func (r *EducatesClusterConfigReconciler) markIngressProgressing(obj *configv1alpha1.EducatesClusterConfig, reason, message string) {
	obj.Status.ObservedGeneration = obj.Generation
	obj.Status.Mode = obj.Spec.Mode
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionIngressReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Progressing",
		Message:            "Managed-mode reconciliation in progress",
		ObservedGeneration: obj.Generation,
	})
}

// markIngressReadyTrue flips IngressReady to True. Called once the
// Contour phase has confirmed the Deployment + DaemonSet are
// serving. Aggregate Ready stays False until markManagedReady.
func (r *EducatesClusterConfigReconciler) markIngressReadyTrue(obj *configv1alpha1.EducatesClusterConfig) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionIngressReady,
		Status:             metav1.ConditionTrue,
		Reason:             "BundledContourReady",
		Message:            "Bundled Contour ingress controller is Ready",
		ObservedGeneration: obj.Generation,
	})
}

// markDNSProgressing publishes a DNSReady=False condition while the
// external-dns install is converging. Same shape as the cert-manager
// and ingress equivalents.
func (r *EducatesClusterConfigReconciler) markDNSProgressing(obj *configv1alpha1.EducatesClusterConfig, reason, message string) {
	obj.Status.ObservedGeneration = obj.Generation
	obj.Status.Mode = obj.Spec.Mode
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionDNSReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Progressing",
		Message:            "Managed-mode reconciliation in progress",
		ObservedGeneration: obj.Generation,
	})
}

// markDNSReadyTrue flips DNSReady to True. Aggregate Ready stays
// False until markManagedReady.
func (r *EducatesClusterConfigReconciler) markDNSReadyTrue(obj *configv1alpha1.EducatesClusterConfig) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionDNSReady,
		Status:             metav1.ConditionTrue,
		Reason:             "BundledExternalDNSReady",
		Message:            "Bundled external-dns is Ready",
		ObservedGeneration: obj.Generation,
	})
}

// markPolicyEnforcementProgressing publishes a
// PolicyEnforcementReady=False condition while the Kyverno install
// is converging. Same shape as the other progressing markers.
func (r *EducatesClusterConfigReconciler) markPolicyEnforcementProgressing(obj *configv1alpha1.EducatesClusterConfig, reason, message string) {
	obj.Status.ObservedGeneration = obj.Generation
	obj.Status.Mode = obj.Spec.Mode
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionPolicyEnforcementReady,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: obj.Generation,
	})
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionReady,
		Status:             metav1.ConditionFalse,
		Reason:             "Progressing",
		Message:            "Managed-mode reconciliation in progress",
		ObservedGeneration: obj.Generation,
	})
}

// markPolicyEnforcementReadyTrue flips PolicyEnforcementReady to
// True. Aggregate Ready stays False until markManagedReady.
func (r *EducatesClusterConfigReconciler) markPolicyEnforcementReadyTrue(obj *configv1alpha1.EducatesClusterConfig) {
	meta.SetStatusCondition(&obj.Status.Conditions, metav1.Condition{
		Type:               conditionPolicyEnforcementReady,
		Status:             metav1.ConditionTrue,
		Reason:             "BundledKyvernoReady",
		Message:            "Bundled Kyverno is Ready",
		ObservedGeneration: obj.Generation,
	})
}

// markManagedPhase sets status.phase without touching conditions. The
// helper exists so reconcileManaged can advance the phase without
// duplicating the boilerplate from markReady/markDegraded — those are
// terminal-state writers.
func (r *EducatesClusterConfigReconciler) markManagedPhase(obj *configv1alpha1.EducatesClusterConfig, phase configv1alpha1.ClusterConfigPhase) {
	obj.Status.Phase = phase
}

// handleManagedReleaseResult maps a helm.EnsureRelease outcome for a
// cluster-service install to a phase result, shared by every cluster-service
// phase (cert-manager, contour, kyverno, external-dns). It returns
// proceed=true when the release is converged enough to continue to the
// service's readiness checks. For the two non-proceeding outcomes it sets the
// service's progressing condition (via mark) plus the aggregate phase and
// returns a stop result:
//
//   - ActionHeldFailed: the release is failed and its inputs are unchanged,
//     so a retry would fail identically. Surface the Helm failure and go
//     Degraded rather than reporting Ready off a partial install.
//   - ActionRepairedRollback: a lock-stuck release was rolled back to its
//     last good revision; requeue so the follow-up upgrade applies desired.
//   - ActionWaitingUninstall: the release is transiently mid-uninstall (a
//     reconcile raced an in-flight teardown); requeue until it settles
//     rather than failing an install against a name still in use.
func (r *EducatesClusterConfigReconciler) handleManagedReleaseResult(
	ctx context.Context,
	obj *configv1alpha1.EducatesClusterConfig,
	service string,
	res helm.Result,
	mark func(obj *configv1alpha1.EducatesClusterConfig, reason, message string),
) (proceed bool, result ctrl.Result, err error) {
	switch res.Action {
	case helm.ActionHeldFailed:
		mark(obj, "ReleaseFailed", helm.FailureMessage(res.Release, fmt.Sprintf("%s Helm release is in a failed state", service)))
		r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseDegraded)
		return false, ctrl.Result{}, r.updateStatusWithTransitionLog(ctx, obj)
	case helm.ActionRepairedRollback:
		mark(obj, "RepairingRelease", fmt.Sprintf("rolled %s release back to its last deployed revision; re-applying desired configuration", service))
		r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
		return false, ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateStatusWithTransitionLog(ctx, obj)
	case helm.ActionWaitingUninstall:
		mark(obj, "ReleaseUninstalling", fmt.Sprintf("%s Helm release is being uninstalled; waiting for teardown to complete before re-converging", service))
		r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
		return false, ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateStatusWithTransitionLog(ctx, obj)
	default:
		return true, ctrl.Result{}, nil
	}
}
