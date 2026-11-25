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
	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// referencedSecretRole says what a Secret the spec names by
// SecretReference is used for.
type referencedSecretRole int

const (
	// caKeyPairSecretRole is the CA key pair cert-manager signs the
	// wildcard certificate with.
	caKeyPairSecretRole referencedSecretRole = iota

	// caTrustSecretRole is a CA trust Secret, holding the CA
	// certificate Educates components trust under ca.crt.
	caTrustSecretRole

	// wildcardCertificateSecretRole is a wildcard certificate Secret
	// served as it is.
	wildcardCertificateSecretRole
)

// referencedSecret is a Secret the spec names by SecretReference, with
// the spec path that names it, for condition messages and cache-miss
// logs.
type referencedSecret struct {
	ref   configv1alpha1.SecretReference
	field string
	role  referencedSecretRole
}

// referencedSecrets lists the Secrets the spec names by SecretReference
// and the reconciler reads for the spec's mode and certificate provider:
// the wildcard certificate and CA trust Secrets in Inline mode, the CA
// key pair and CA trust Secret of a custom CA, the wildcard certificate
// and CA trust Secret of the StaticCertificate provider, and the CA
// trust Secret of the None provider. A reference under a provider or
// issuer type the spec does not select is left out. It is the one list
// the Secret watch, the operator's Secret cache scope and the CA trust
// source all read.
func referencedSecrets(obj *configv1alpha1.EducatesClusterConfig) []referencedSecret {
	var out []referencedSecret
	add := func(ref *configv1alpha1.SecretReference, field string, role referencedSecretRole) {
		if ref != nil {
			out = append(out, referencedSecret{ref: *ref, field: field, role: role})
		}
	}
	switch obj.Spec.Mode {
	case configv1alpha1.ClusterConfigModeInline:
		if obj.Spec.Inline != nil {
			add(obj.Spec.Inline.Ingress.WildcardCertificateSecretRef, inlineWildcardField, wildcardCertificateSecretRole)
			add(obj.Spec.Inline.Ingress.CACertificateSecretRef, inlineCATrustField, caTrustSecretRole)
		}
	case configv1alpha1.ClusterConfigModeManaged:
		if ca := customCAConfig(obj); ca != nil {
			add(&ca.CAKeyPairSecretRef, customCAKeyPairField, caKeyPairSecretRole)
			add(ca.CACertificateSecretRef, customCATrustField, caTrustSecretRole)
		}
		if static := staticCertificateConfig(obj); static != nil {
			add(&static.TLSSecretRef, staticTLSField, wildcardCertificateSecretRole)
			add(static.CACertificateSecretRef, staticCATrustField, caTrustSecretRole)
		}
		if none := noneCertificatesConfig(obj); none != nil {
			add(none.CACertificateSecretRef, noneCATrustField, caTrustSecretRole)
		}
	}
	return out
}

// ReferencedSecretNamespaces returns the namespaces the
// EducatesClusterConfig's SecretReference fields name, for the
// operator's Secret cache scope. A reference without a namespace means
// the operator namespace and adds nothing.
func ReferencedSecretNamespaces(obj *configv1alpha1.EducatesClusterConfig) []string {
	var out []string
	for _, s := range referencedSecrets(obj) {
		if s.ref.Namespace != "" {
			out = append(out, s.ref.Namespace)
		}
	}
	return out
}
