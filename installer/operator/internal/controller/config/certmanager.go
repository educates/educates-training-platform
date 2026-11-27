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
	"strings"
	"time"

	cmacme "github.com/cert-manager/cert-manager/pkg/apis/acme/v1"
	cmv1 "github.com/cert-manager/cert-manager/pkg/apis/certmanager/v1"
	cmmeta "github.com/cert-manager/cert-manager/pkg/apis/meta/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/ownership"
	"github.com/educates/educates-training-platform/installer/operator/internal/pki"
)

// Cert-manager Deployment names installed by the upstream chart with
// default values. These are the readiness gate before the operator
// proceeds to ClusterIssuer/Certificate creation.
var certManagerDeployments = []string{
	"cert-manager",
	"cert-manager-webhook",
	"cert-manager-cainjector",
}

// Resource names the operator creates for the wildcard-cert pipeline.
// Constants rather than CR-derived strings so the names are stable
// across reconciles and easy to reference from tests, docs, and the
// finalizer drain path.
const (
	customCASecretName    = "educates-custom-ca"
	wildcardClusterIssuer = "educates-wildcard-issuer"
	wildcardCertificate   = "educates-wildcard"
	wildcardTLSSecretName = "educates-wildcard-tls"
	acmeAccountKeySecret  = "educates-acme-account-key"
	letsEncryptProdServer = "https://acme-v02.api.letsencrypt.org/directory"
)

// errCertManagerNotReady is returned by ensureCertManagerReady when one
// or more cert-manager Deployments has not rolled out yet.
// It is a typed sentinel so the reconciler can map it to a
// CertificatesReady=False/WaitingForWebhook condition without retrying
// the underlying API call.
var errCertManagerNotReady = errors.New("cert-manager Deployments have not rolled out yet")

// isWebhookNotReadyErr reports whether err is the apiserver's "couldn't
// reach the cert-manager webhook" error. The webhook is fronted by a
// Service whose endpoints take a beat after Deployment.Available to
// route, and the caBundle on the ValidatingWebhookConfiguration is
// injected asynchronously by cert-manager's cainjector. During that
// window, any SSA against a cert-manager.io kind comes back wrapped
// with these strings; the operator should retry rather than treating
// the failure as a hard error.
//
// This is a substring match because controller-runtime wraps the
// apiserver response without preserving the typed error path. The
// strings are stable across cert-manager versions: "failed calling
// webhook" is the apiserver's wrapper; the inner cause is one of TLS
// handshake (x509), TCP refusal (connection refused), or read timeout
// (i/o timeout / context deadline). Matching the wrapper substring is
// enough to classify the failure mode — broader than necessary, but
// the false-positive rate is zero in practice (the apiserver only
// uses that phrase for webhook failures).
//
// Captured as a follow-up (harden cert-manager readiness with a
// synthetic admission probe) — the proper fix is to gate
// ClusterIssuer/Certificate SSA on a dry-run admission probe so this
// state is detected proactively rather than observed as a failure.
// Until that lands, the operator just reclassifies the error so it
// stops looking like a real fault in the logs.
func isWebhookNotReadyErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "failed calling webhook") &&
		strings.Contains(msg, "cert-manager")
}

// isCertManagerCRDMissingErr reports whether err indicates the
// cert-manager.io CRDs are no longer present in the cluster (helm
// uninstall, or `kubectl delete crd`). Two error shapes both mean
// the same thing depending on when the operator's discovery
// information was cached:
//
//  1. *meta.NoKindMatchError — the RESTMapper has no record of the
//     GVK. Happens when the operator was started without the CRDs
//     ever being available, or after the mapper was invalidated.
//  2. 404 StatusError on the resource path — "the server could not
//     find the requested resource (post clusterissuers.cert-manager.io)"
//     with `Reason=NotFound` and `Details.Group=cert-manager.io`,
//     plus a `CauseTypeUnexpectedServerResponse` cause. Happens
//     when the operator's mapper still has the GVK cached from
//     before the CRD deletion, so the request reaches the apiserver
//     but hits no URL handler.
//
// We classify both as "CRDs gone" so the reconciler can surface a
// clean CertManagerCRDsMissing condition + Degraded phase instead of
// retrying tightly on a state only user action can fix.
//
// Note: this only quiets the operator's *own* error paths — the
// underlying controller-runtime Kind source's polling-retry loop
// continues logging at the controller-runtime layer, because there
// is no API to remove a source from a running controller. Captured
// as a follow-up (quiet the controller-runtime Kind source after
// cert-manager CRDs are removed).
func isCertManagerCRDMissingErr(err error) bool {
	if err == nil {
		return false
	}
	if meta.IsNoMatchError(err) {
		return true
	}
	// Pre-cached GVK + deleted CRD: apiserver returns 404 with a
	// URL-not-found-style detail. errors.AsType walks the fmt.Errorf
	// wrap chain that the helper functions construct.
	if status, ok := errors.AsType[*apierrors.StatusError](err); ok {
		s := status.Status()
		if s.Code == 404 && s.Details != nil && s.Details.Group == "cert-manager.io" {
			for _, c := range s.Details.Causes {
				if c.Type == metav1.CauseTypeUnexpectedServerResponse {
					return true
				}
			}
		}
	}
	return false
}

// cleanupCertManager unwinds the cert-manager phase in reverse
// install order: wildcard Certificate → ClusterIssuer → copied
// CustomCA Secret → helm uninstall cert-manager → cert-manager
// namespace. Each step is idempotent (deleteIfPresent swallows
// NotFound + NoMatchError), so a retried reconcile after partial
// drain re-attempts only what's still present.
//
// The cleanup runs only when the provider is BundledCertManager. For
// any other provider the operator installed nothing here, and a
// cert-manager release or namespace under the same names belongs to
// the administrator, so it must survive the teardown. The provider is
// immutable, so the spec seen at deletion is the one the install ran
// with.
func (r *EducatesClusterConfigReconciler) cleanupCertManager(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	if !shouldInstallCertManager(obj) {
		return nil
	}
	if err := r.deleteIfPresent(ctx, &cmv1.Certificate{
		ObjectMeta: metav1.ObjectMeta{Namespace: r.OperatorNamespace, Name: wildcardCertificate},
	}); err != nil {
		return fmt.Errorf("delete wildcard Certificate: %w", err)
	}
	if err := r.deleteIfPresent(ctx, &cmv1.ClusterIssuer{
		ObjectMeta: metav1.ObjectMeta{Name: wildcardClusterIssuer},
	}); err != nil {
		return fmt.Errorf("delete ClusterIssuer: %w", err)
	}
	if err := r.deleteIfPresent(ctx, &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: certManagerNamespace, Name: customCASecretName},
	}); err != nil {
		return fmt.Errorf("delete copied CustomCA Secret: %w", err)
	}

	// Helm uninstall is idempotent in the wrapper (IgnoreNotFound on
	// the action). Safe to call even when the release was never
	// created (validation failed before reconcileCertManager ran).
	hc, err := r.HelmClientFor(certManagerNamespace)
	if err != nil {
		return fmt.Errorf("build helm client for cleanup: %w", err)
	}
	if err := hc.Uninstall(certManagerReleaseName); err != nil {
		return fmt.Errorf("uninstall cert-manager release: %w", err)
	}

	if err := r.deleteIfPresent(ctx, &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: certManagerNamespace},
	}); err != nil {
		return fmt.Errorf("delete cert-manager namespace: %w", err)
	}
	return nil
}

// reconcileCertificatesPhase runs the certificates phase. For the
// BundledCertManager provider that is the full cert-manager + wildcard
// certificate pipeline; the None provider has nothing to do, and the
// StaticCertificate provider has its own phase,
// reconcileStaticCertificatePhase, which serves staticCert, the
// certificate checkStaticCertificate passed. Caller convention follows
// reconcileManaged: done=true means the phase is complete, and its
// Result may still carry a RequeueAfter by which reconcile must run
// again; done=false means stop here and return the Result and error
// verbatim.
//
// BundledCertManager steps:
//
//  1. helm install/upgrade cert-manager from the vendored chart.
//  2. Wait for cert-manager Deployments to roll out.
//  3. Copy the CustomCA Secret into cert-manager's namespace.
//  4. SSA the ClusterIssuer.
//  5. SSA the wildcard Certificate.
//  6. Wait for the Certificate to report Ready.
//
// All cert-manager-specific error classification (CRDs missing,
// webhook not yet routable) is handled here so the orchestrator
// in reconcileManaged stays oblivious to cert-manager internals.
func (r *EducatesClusterConfigReconciler) reconcileCertificatesPhase(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig, staticCert *pki.WildcardCertificate) (bool, ctrl.Result, error) {
	log := logf.FromContext(ctx)
	// phaseStop wraps the (Result, error) returned by helpers like
	// handleCertManagerCRDsMissing into the (done bool, Result,
	// error) shape this phase returns. done is always false at a
	// stop point — the phase is not complete and the orchestrator
	// returns Result+err verbatim.
	phaseStop := func(res ctrl.Result, err error) (bool, ctrl.Result, error) {
		return false, res, err
	}

	if obj.Spec.Ingress.Certificates.Provider == configv1alpha1.CertificatesProviderNone {
		// No in-cluster TLS: install no cert-manager, create no issuer
		// or Certificate. The phase is satisfied immediately so the
		// orchestrator proceeds to Contour.
		r.markCertificatesNotManaged(obj)
		return true, ctrl.Result{}, nil
	}

	if obj.Spec.Ingress.Certificates.Provider == configv1alpha1.CertificatesProviderStaticCertificate {
		return r.reconcileStaticCertificatePhase(ctx, obj, staticCert)
	}

	if !shouldInstallCertManager(obj) {
		// validateManaged rejects every other provider, so nothing
		// reaches here; there is nothing to install or apply.
		return true, ctrl.Result{}, nil
	}

	res, err := r.reconcileCertManager(ctx, obj)
	if err != nil {
		log.Error(err, "cert-manager reconcile failed")
		r.markCertificatesProgressing(obj, "InstallFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, obj)
		return phaseStop(ctrl.Result{}, err)
	}

	if proceed, result, err := r.handleManagedReleaseResult(ctx, obj, "cert-manager", res, r.markCertificatesProgressing); !proceed {
		return false, result, err
	}

	// Gate the rest of the pipeline on cert-manager being live. A
	// not-ready signal is published as a progressing condition; the
	// Deployment watch re-triggers reconcile as the rollout progresses.
	if err := r.ensureCertManagerReady(ctx); err != nil {
		if errors.Is(err, errCertManagerNotReady) {
			r.markCertificatesProgressing(obj, "WaitingForCertManager", "cert-manager Deployments have not rolled out yet")
			r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
			// RequeueAfter as belt-and-suspenders: in practice the
			// 3 cert-manager Deployments roll out in stagger, so the
			// final rollout transition usually has at least one
			// other status event behind it that re-triggers the
			// reconciler. But a missed final event would leave the
			// gate waiting; 15s of self-poll matches Contour's gate.
			return false, ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateStatusWithTransitionLog(ctx, obj)
		}
		return phaseStop(ctrl.Result{}, err)
	}

	// CustomCA-only prerequisite: copy the CA Secret into the
	// cert-manager namespace so the CA-typed ClusterIssuer can read
	// it. ACME issuers don't reference a user-supplied Secret; the
	// account key is generated on first use into a Secret cert-manager
	// owns. Each helper is idempotent (SSA) so re-running converges.
	bcm := obj.Spec.Ingress.Certificates.BundledCertManager
	if bcm.IssuerType == configv1alpha1.IssuerTypeCustomCA {
		if err := r.ensureCustomCASecretCopy(ctx, obj, bcm.CustomCA.CAKeyPairSecretRef); err != nil {
			if isCertManagerCRDMissingErr(err) {
				return phaseStop(r.handleCertManagerCRDsMissing(ctx, obj, err))
			}
			r.markCertificatesProgressing(obj, "CustomCACopyFailed", err.Error())
			_ = r.updateStatusWithTransitionLog(ctx, obj)
			return phaseStop(ctrl.Result{}, err)
		}
	}
	if err := r.ensureClusterIssuer(ctx, obj); err != nil {
		if isCertManagerCRDMissingErr(err) {
			return phaseStop(r.handleCertManagerCRDsMissing(ctx, obj, err))
		}
		if isWebhookNotReadyErr(err) {
			return phaseStop(r.handleWebhookNotReady(ctx, obj, "ClusterIssuer", err))
		}
		r.markCertificatesProgressing(obj, "ClusterIssuerApplyFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, obj)
		return phaseStop(ctrl.Result{}, err)
	}
	if err := r.ensureWildcardCertificate(ctx, obj, obj.Spec.Ingress.Domain); err != nil {
		if isCertManagerCRDMissingErr(err) {
			return phaseStop(r.handleCertManagerCRDsMissing(ctx, obj, err))
		}
		if isWebhookNotReadyErr(err) {
			return phaseStop(r.handleWebhookNotReady(ctx, obj, "Certificate", err))
		}
		r.markCertificatesProgressing(obj, "CertificateApplyFailed", err.Error())
		_ = r.updateStatusWithTransitionLog(ctx, obj)
		return phaseStop(ctrl.Result{}, err)
	}

	ready, err := r.certificateReady(ctx)
	if err != nil {
		if isCertManagerCRDMissingErr(err) {
			return phaseStop(r.handleCertManagerCRDsMissing(ctx, obj, err))
		}
		return phaseStop(ctrl.Result{}, err)
	}
	if !ready {
		r.markCertificatesProgressing(obj, "WaitingForCertificate", "wildcard Certificate not yet Ready")
		r.markManagedPhase(obj, configv1alpha1.ClusterConfigPhaseInstalling)
		// Same belt-and-suspenders RequeueAfter as the other
		// "Waiting" branches — there's exactly one Ready=False→True
		// transition on the Certificate, so missing that watch event
		// would leave us stuck.
		return false, ctrl.Result{RequeueAfter: 15 * time.Second}, r.updateStatusWithTransitionLog(ctx, obj)
	}

	// Phase complete — mark CertificatesReady=True so a reader can
	// observe per-phase progress without waiting for the aggregate
	// Ready. The aggregate Ready flip + status.ingress publication
	// stay in markManagedReady so they only happen once every phase
	// has signed off.
	r.markCertificatesReadyTrue(obj)
	return true, ctrl.Result{}, nil
}

// shouldInstallCertManager reports whether the operator is
// responsible for installing cert-manager based on the spec: only the
// BundledCertManager provider installs it. The StaticCertificate and
// None providers never do, and ExternalCertManager names a cert-manager
// the administrator installed (validateManaged rejects it as not yet
// supported).
func shouldInstallCertManager(obj *configv1alpha1.EducatesClusterConfig) bool {
	if obj.Spec.Ingress == nil {
		return false
	}
	return obj.Spec.Ingress.Certificates.Provider == configv1alpha1.CertificatesProviderBundledCertManager
}

// ensureCertManagerReady gates the rest of the cert-manager pipeline
// on the three upstream Deployments having rolled out their current
// spec. A Deployment that's missing (404) maps to "not ready" rather
// than a hard error — Helm has not yet finished applying the manifests
// in that case.
func (r *EducatesClusterConfigReconciler) ensureCertManagerReady(ctx context.Context) error {
	rolledOut, err := r.deploymentsRolledOut(ctx, certManagerNamespace, certManagerDeployments...)
	if err != nil {
		return err
	}
	if !rolledOut {
		return errCertManagerNotReady
	}
	return nil
}

// ensureCustomCASecretCopy mirrors the user-supplied CustomCA Secret
// from its declared namespace (or the operator namespace when empty)
// into the cert-manager namespace so the CA-typed ClusterIssuer can
// read it.
//
// Background: a `kind: ClusterIssuer` with `spec.ca.secretName` reads
// the Secret from cert-manager's `--cluster-resource-namespace` flag,
// which defaults to the namespace cert-manager is installed in
// (cert-manager). The user-supplied Secret can live in any namespace
// the install pipeline chose, the operator namespace unless its
// reference names another; the operator copies from there into
// cert-manager's namespace.
//
// Implementation is SSA so subsequent reconciles converge labels and
// data without read-modify-write races. Owner reference on the copy
// is the EducatesClusterConfig so `kubectl delete educatesclusterconfig`
// cascades the copy.
func (r *EducatesClusterConfigReconciler) ensureCustomCASecretCopy(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig, src configv1alpha1.SecretReference) error {
	srcNS := src.Namespace
	if srcNS == "" {
		srcNS = r.OperatorNamespace
	}
	secret := &corev1.Secret{}
	// APIReader bypasses the cache; see checkCustomCASecret for rationale.
	if err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: srcNS, Name: src.Name}, secret); err != nil {
		return fmt.Errorf("read source CustomCA Secret %s/%s: %w", srcNS, src.Name, err)
	}

	dst := &corev1.Secret{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "Secret",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      customCASecretName,
			Namespace: certManagerNamespace,
			Labels: map[string]string{
				ownership.ManagedByLabel: ownership.ManagedByValue,
			},
		},
		Type: corev1.SecretTypeTLS,
		Data: copyCASecretData(secret.Data),
	}
	if err := controllerSetOwnerOnCrossNamespaceCopy(owner, dst, r.Scheme); err != nil {
		return err
	}

	return r.applySSA(ctx, dst)
}

// applySSA server-side-applies a fully-specified typed object via the
// non-deprecated Client.Apply API (the client.Apply patch type is
// deprecated as of controller-runtime v0.23). Typed structs aren't
// runtime.ApplyConfigurations, so the object is converted to
// unstructured with its scheme-resolved GVK stamped (our constructed
// objects leave TypeMeta empty). The conversion artifacts SSA must
// not assert ownership of — empty status, null creationTimestamp —
// are stripped before the apply.
func (r *EducatesClusterConfigReconciler) applySSA(ctx context.Context, obj client.Object) error {
	gvk, err := apiutil.GVKForObject(obj, r.Scheme)
	if err != nil {
		return fmt.Errorf("resolve GVK for SSA apply: %w", err)
	}
	m, err := runtime.DefaultUnstructuredConverter.ToUnstructured(obj)
	if err != nil {
		return fmt.Errorf("convert %s to unstructured: %w", gvk.Kind, err)
	}
	u := &unstructured.Unstructured{Object: m}
	u.SetGroupVersionKind(gvk)
	unstructured.RemoveNestedField(u.Object, "status")
	unstructured.RemoveNestedField(u.Object, "metadata", "creationTimestamp")
	return r.Apply(ctx, client.ApplyConfigurationFromUnstructured(u),
		client.FieldOwner(ownership.FieldManager), client.ForceOwnership)
}

// copyCASecretData picks the keys cert-manager's CA issuer reads from
// the source Secret and preserves any ca.crt the user included (a
// common shape when the CA is itself signed by an upstream root —
// downstream consumers expect the chain). The previous implementation
// hardcoded only tls.crt + tls.key, silently dropping ca.crt and any
// other auxiliary keys.
func copyCASecretData(src map[string][]byte) map[string][]byte {
	out := map[string][]byte{
		"tls.crt": src["tls.crt"],
		"tls.key": src["tls.key"],
	}
	if v, ok := src["ca.crt"]; ok && len(v) > 0 {
		out["ca.crt"] = v
	}
	return out
}

// controllerSetOwnerOnCrossNamespaceCopy attaches the
// EducatesClusterConfig as a controller owner of a cluster-service
// resource. Owner references can target cluster-scoped owners from
// namespaced dependents directly; controllerutil.SetControllerReference
// happens to enforce a same-namespace check that's wrong for our case
// (cluster-scoped owner → namespaced dependent), so we set the
// reference by hand using metav1.OwnerReference for clarity.
func controllerSetOwnerOnCrossNamespaceCopy(owner *configv1alpha1.EducatesClusterConfig, dst client.Object, scheme *runtime.Scheme) error {
	gvk, err := apiutil.GVKForObject(owner, scheme)
	if err != nil {
		return fmt.Errorf("resolve owner GVK: %w", err)
	}
	ref := metav1.OwnerReference{
		APIVersion:         gvk.GroupVersion().String(),
		Kind:               gvk.Kind,
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		Controller:         new(true),
		BlockOwnerDeletion: new(true),
	}
	dst.SetOwnerReferences([]metav1.OwnerReference{ref})
	return nil
}

// ensureClusterIssuer applies the cluster-wide ClusterIssuer that
// signs the wildcard Certificate. The Issuer spec is built per
// issuer type — CA-typed for CustomCA, ACME-typed (DNS01 solver)
// for ACME. SSA so re-running the reconciler converges drift
// without explicit version tracking.
func (r *EducatesClusterConfigReconciler) ensureClusterIssuer(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig) error {
	bcm := owner.Spec.Ingress.Certificates.BundledCertManager

	var issuerCfg cmv1.IssuerConfig
	switch bcm.IssuerType {
	case configv1alpha1.IssuerTypeCustomCA:
		issuerCfg = cmv1.IssuerConfig{
			CA: &cmv1.CAIssuer{SecretName: customCASecretName},
		}
	case configv1alpha1.IssuerTypeACME:
		acme, err := buildACMEIssuer(bcm.ACME)
		if err != nil {
			return err
		}
		issuerCfg = cmv1.IssuerConfig{ACME: acme}
	default:
		return fmt.Errorf("unsupported issuerType %q", bcm.IssuerType)
	}

	ci := &cmv1.ClusterIssuer{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "ClusterIssuer",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: wildcardClusterIssuer,
			Labels: map[string]string{
				ownership.ManagedByLabel: ownership.ManagedByValue,
			},
		},
		Spec: cmv1.IssuerSpec{IssuerConfig: issuerCfg},
	}
	if err := controllerSetOwnerOnCrossNamespaceCopy(owner, ci, r.Scheme); err != nil {
		return err
	}
	return r.applySSA(ctx, ci)
}

// buildACMEIssuer translates the operator's ACMEConfig into a
// cert-manager ACMEIssuer spec. Authentication is identity-based:
// Route53 sets Role (assumed via IRSA) and CloudDNS sets only the
// project (cert-manager picks up Application Default Credentials
// from the SA's Workload Identity annotation). Static credential
// Secrets are rejected by the validator until follow-up adds them.
func buildACMEIssuer(acme *configv1alpha1.ACMEConfig) (*cmacme.ACMEIssuer, error) {
	server := acme.Server
	if server == "" {
		server = letsEncryptProdServer
	}
	dns01 := acme.Solvers.DNS01
	solver := cmacme.ACMEChallengeSolver{DNS01: &cmacme.ACMEChallengeSolverDNS01{}}
	switch dns01.Provider {
	case configv1alpha1.DNS01ProviderRoute53:
		solver.DNS01.Route53 = &cmacme.ACMEIssuerDNS01ProviderRoute53{
			HostedZoneID: dns01.Route53.HostedZoneID,
			Region:       dns01.Route53.Region,
			Role:         dns01.Route53.IAMRoleARN,
		}
	case configv1alpha1.DNS01ProviderCloudDNS:
		solver.DNS01.CloudDNS = &cmacme.ACMEIssuerDNS01ProviderCloudDNS{
			Project:        dns01.CloudDNS.Project,
			HostedZoneName: dns01.CloudDNS.Zone,
		}
	default:
		return nil, fmt.Errorf("unsupported DNS01 provider %q", dns01.Provider)
	}
	return &cmacme.ACMEIssuer{
		Email:      acme.Email,
		Server:     server,
		PrivateKey: cmmeta.SecretKeySelector{LocalObjectReference: cmmeta.LocalObjectReference{Name: acmeAccountKeySecret}},
		Solvers:    []cmacme.ACMEChallengeSolver{solver},
	}, nil
}

// ensureWildcardCertificate applies the wildcard Certificate in the
// operator namespace. cert-manager writes the resulting tls Secret
// alongside it in the same namespace, which is where the published
// status.ingress.wildcardCertificateSecretRef points. dnsNames cover
// both `<domain>` and `*.<domain>` so the same cert handles the
// portal hostname and per-session hostnames.
func (r *EducatesClusterConfigReconciler) ensureWildcardCertificate(ctx context.Context, owner *configv1alpha1.EducatesClusterConfig, domain string) error {
	cert := &cmv1.Certificate{
		TypeMeta: metav1.TypeMeta{
			APIVersion: cmv1.SchemeGroupVersion.String(),
			Kind:       "Certificate",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      wildcardCertificate,
			Namespace: r.OperatorNamespace,
			Labels: map[string]string{
				ownership.ManagedByLabel: ownership.ManagedByValue,
			},
		},
		Spec: cmv1.CertificateSpec{
			SecretName: wildcardTLSSecretName,
			DNSNames:   []string{domain, "*." + domain},
			IssuerRef: cmmeta.IssuerReference{
				Kind: "ClusterIssuer",
				Name: wildcardClusterIssuer,
			},
		},
	}
	if err := controllerSetOwnerOnCrossNamespaceCopy(owner, cert, r.Scheme); err != nil {
		return err
	}
	return r.applySSA(ctx, cert)
}

// certificateReady reports whether the wildcard Certificate carries
// Ready=True. Returns (false, nil) when the Certificate or its Ready
// condition is missing (still being issued).
func (r *EducatesClusterConfigReconciler) certificateReady(ctx context.Context) (bool, error) {
	cert := &cmv1.Certificate{}
	key := types.NamespacedName{Namespace: r.OperatorNamespace, Name: wildcardCertificate}
	if err := r.Get(ctx, key, cert); err != nil {
		if apierrors.IsNotFound(err) {
			return false, nil
		}
		return false, fmt.Errorf("get wildcard Certificate %s: %w", key, err)
	}
	for _, c := range cert.Status.Conditions {
		if c.Type == cmv1.CertificateConditionReady {
			return c.Status == cmmeta.ConditionTrue, nil
		}
	}
	return false, nil
}
