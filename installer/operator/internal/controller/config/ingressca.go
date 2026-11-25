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

	"k8s.io/apimachinery/pkg/types"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/pki"
)

// ingressCASecretName is the operator-owned CA trust Secret published as
// status.ingress.caCertificateSecretRef whenever a CA is configured. It
// lives in the operator namespace and holds exactly one key, ca.crt, so
// Educates components receive CA trust and never a CA private key.
const ingressCASecretName = "educates-ingress-ca"

// Spec paths of the CA references, used in condition messages and
// cache-miss warnings.
const (
	customCAKeyPairField = "spec.ingress.certificates.bundledCertManager.customCA.caKeyPairSecretRef"
	customCATrustField   = "spec.ingress.certificates.bundledCertManager.customCA.caCertificateSecretRef"
	staticCATrustField   = "spec.ingress.certificates.staticCertificate.caCertificateSecretRef"
	noneCATrustField     = "spec.ingress.certificates.none.caCertificateSecretRef"
	inlineCATrustField   = "spec.inline.ingress.caCertificateSecretRef"
)

// customCAConfig returns the Managed-mode CustomCA configuration, or nil
// when the spec does not issue the wildcard certificate from a custom CA.
func customCAConfig(obj *configv1alpha1.EducatesClusterConfig) *configv1alpha1.CustomCAConfig {
	if obj.Spec.Ingress == nil {
		return nil
	}
	certs := obj.Spec.Ingress.Certificates
	if certs.Provider != configv1alpha1.CertificatesProviderBundledCertManager ||
		certs.BundledCertManager == nil ||
		certs.BundledCertManager.IssuerType != configv1alpha1.IssuerTypeCustomCA {
		return nil
	}
	return certs.BundledCertManager.CustomCA
}

// noneCertificatesConfig returns the Managed-mode None provider's
// configuration, or nil when the spec selects another provider or sets
// no none block.
func noneCertificatesConfig(obj *configv1alpha1.EducatesClusterConfig) *configv1alpha1.NoneCertificatesConfig {
	if obj.Spec.Ingress == nil {
		return nil
	}
	certs := obj.Spec.Ingress.Certificates
	if certs.Provider != configv1alpha1.CertificatesProviderNone {
		return nil
	}
	return certs.None
}

// secretRefNamespace resolves a SecretReference's namespace, where empty
// means the operator namespace.
func (r *EducatesClusterConfigReconciler) secretRefNamespace(ref configv1alpha1.SecretReference) string {
	if ref.Namespace == "" {
		return r.OperatorNamespace
	}
	return ref.Namespace
}

// ingressCASource returns the Secret the ingress CA trust is derived
// from, the field that names it, and false when no CA is configured. A
// referenced CA trust Secret is the source: in Inline mode, for the
// StaticCertificate and None providers, and for a custom CA, where it
// wins over the CA key pair. A custom CA without one derives the CA
// trust from its CA key pair.
func (r *EducatesClusterConfigReconciler) ingressCASource(obj *configv1alpha1.EducatesClusterConfig) (pki.CASource, string, bool) {
	var keyPair *referencedSecret
	for _, s := range referencedSecrets(obj) {
		switch s.role {
		case caTrustSecretRole:
			return r.caTrustSource(s.ref), s.field, true
		case caKeyPairSecretRole:
			keyPair = &s
		}
	}
	if keyPair == nil {
		return pki.CASource{}, "", false
	}
	return r.caKeyPairSource(keyPair.ref), keyPair.field, true
}

// caTrustSource is the CA source for a reference to a CA trust Secret.
func (r *EducatesClusterConfigReconciler) caTrustSource(ref configv1alpha1.SecretReference) pki.CASource {
	return pki.CASource{
		Namespace: r.secretRefNamespace(ref),
		Name:      ref.Name,
		Rule:      pki.FromCATrustSecret,
	}
}

// caKeyPairSource is the CA source for a reference to a CA key pair.
func (r *EducatesClusterConfigReconciler) caKeyPairSource(ref configv1alpha1.SecretReference) pki.CASource {
	return pki.CASource{
		Namespace: r.secretRefNamespace(ref),
		Name:      ref.Name,
		Rule:      pki.FromCAKeyPair,
	}
}

// ingressCASecretRef is the status contract's reference to the derived
// CA trust Secret.
func (r *EducatesClusterConfigReconciler) ingressCASecretRef() *configv1alpha1.NamespacedSecretRef {
	return &configv1alpha1.NamespacedSecretRef{Namespace: r.OperatorNamespace, Name: ingressCASecretName}
}

// validateCustomCAKeyPair checks the CA key pair cert-manager signs with
// before cert-manager sees it: the Secret exists, holds tls.crt and
// tls.key, and its certificate is a CA. Failures carry a specific
// CertificatesReady reason.
func (r *EducatesClusterConfigReconciler) validateCustomCAKeyPair(ctx context.Context, ca *configv1alpha1.CustomCAConfig) error {
	src := r.caKeyPairSource(ca.CAKeyPairSecretRef)
	r.warnIfCacheMiss(ctx, src.Namespace, customCAKeyPairField)
	// APIReader bypasses the controller-runtime cache, which covers only
	// the namespaces collected at startup.
	_, err := pki.ReadCACertificate(ctx, r.APIReader, src)
	return asCertificatesValidationError(err, customCAKeyPairField)
}

// ensureIngressCATrust derives educates-ingress-ca from the configured CA
// source and server-side applies it, owned by the EducatesClusterConfig.
// It runs on every reconcile, and the Secret watch maps the source to
// the singleton, so the derived Secret follows changes to its source.
// With no CA configured it does nothing.
func (r *EducatesClusterConfigReconciler) ensureIngressCATrust(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	src, field, ok := r.ingressCASource(obj)
	if !ok {
		return nil
	}
	r.warnIfCacheMiss(ctx, src.Namespace, field)
	applier := pki.CATrustApplier{Reader: r.APIReader, Writer: r.Client, Scheme: r.Scheme}
	dst := types.NamespacedName{Namespace: r.OperatorNamespace, Name: ingressCASecretName}
	return asCertificatesValidationError(applier.Apply(ctx, src, dst, obj), field)
}

// asCertificatesValidationError turns a Secret the pki package cannot
// use, a CA source or a wildcard certificate, into a validation failure
// reported on CertificatesReady with the specific reason. Other errors
// pass through for retry.
func asCertificatesValidationError(err error, field string) error {
	if serr, ok := errors.AsType[*pki.CertificateSecretError](err); ok {
		return &validationError{
			Field:           field,
			Reason:          serr.Message,
			ConditionType:   conditionCertificatesReady,
			ConditionReason: serr.Reason,
		}
	}
	return err
}
