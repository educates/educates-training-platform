/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"context"
	"reflect"
	"testing"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
)

// We unit-test the namespace-collection logic by factoring through a
// helper that takes a pre-built ECC rather than going through
// client.New + a real REST config (which would need envtest). The
// production entry point discoverCachedSecretNamespaces is exercised
// indirectly by the operator envtest suite when manager.Start sees the
// configured cache namespaces apply.

func TestCollectFromECC_NoCR_DefaultsOnly(t *testing.T) {
	scheme := testScheme(t)
	c := fake.NewClientBuilder().WithScheme(scheme).Build()
	got, err := collectFromClient(context.Background(), c, "educates-installer")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"educates-installer"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("no-CR set = %v, want %v", got, want)
	}
}

func TestCollectFromECC_ManagedCustomCANS_Added(t *testing.T) {
	scheme := testScheme(t)
	ecc := &configv1alpha1.EducatesClusterConfig{}
	ecc.Name = singletonName
	ecc.Spec.Mode = configv1alpha1.ClusterConfigModeManaged
	ecc.Spec.Ingress = &configv1alpha1.Ingress{
		Certificates: configv1alpha1.Certificates{
			Provider: configv1alpha1.CertificatesProviderBundledCertManager,
			BundledCertManager: &configv1alpha1.BundledCertManagerConfig{
				IssuerType: configv1alpha1.IssuerTypeCustomCA,
				CustomCA: &configv1alpha1.CustomCAConfig{
					CAKeyPairSecretRef: configv1alpha1.SecretReference{
						Name:      "workshop-ca",
						Namespace: "team-alpha-secrets",
					},
					CACertificateSecretRef: &configv1alpha1.SecretReference{
						Name:      "workshop-ca-trust",
						Namespace: "team-alpha-trust",
					},
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ecc).Build()

	got, err := collectFromClient(context.Background(), c, "educates-installer")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"educates-installer", "team-alpha-secrets", "team-alpha-trust"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Managed CustomCA namespaces not folded in: got %v, want %v", got, want)
	}
}

func TestCollectFromECC_ManagedStaticCertificateNS_Added(t *testing.T) {
	scheme := testScheme(t)
	ecc := &configv1alpha1.EducatesClusterConfig{}
	ecc.Name = singletonName
	ecc.Spec.Mode = configv1alpha1.ClusterConfigModeManaged
	ecc.Spec.Ingress = &configv1alpha1.Ingress{
		Certificates: configv1alpha1.Certificates{
			Provider: configv1alpha1.CertificatesProviderStaticCertificate,
			StaticCertificate: &configv1alpha1.StaticCertificateConfig{
				TLSSecretRef: configv1alpha1.SecretReference{
					Name:      "wildcard",
					Namespace: "static-tls",
				},
				CACertificateSecretRef: &configv1alpha1.SecretReference{
					Name:      "wildcard-ca",
					Namespace: "static-ca",
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ecc).Build()

	got, err := collectFromClient(context.Background(), c, "educates-installer")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"educates-installer", "static-ca", "static-tls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Managed StaticCertificate namespaces not folded in: got %v, want %v", got, want)
	}
}

func TestCollectFromECC_ManagedNoneCATrustNS_Added(t *testing.T) {
	scheme := testScheme(t)
	ecc := &configv1alpha1.EducatesClusterConfig{}
	ecc.Name = singletonName
	ecc.Spec.Mode = configv1alpha1.ClusterConfigModeManaged
	ecc.Spec.Ingress = &configv1alpha1.Ingress{
		Certificates: configv1alpha1.Certificates{
			Provider: configv1alpha1.CertificatesProviderNone,
			None: &configv1alpha1.NoneCertificatesConfig{
				CACertificateSecretRef: &configv1alpha1.SecretReference{
					Name:      "terminator-ca",
					Namespace: "terminator-trust",
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ecc).Build()

	got, err := collectFromClient(context.Background(), c, "educates-installer")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"educates-installer", "terminator-trust"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Managed None CA trust namespace not folded in: got %v, want %v", got, want)
	}
}

func TestCollectFromECC_InlineCANS_Added(t *testing.T) {
	scheme := testScheme(t)
	ecc := &configv1alpha1.EducatesClusterConfig{}
	ecc.Name = singletonName
	ecc.Spec.Mode = configv1alpha1.ClusterConfigModeInline
	ecc.Spec.Inline = &configv1alpha1.InlineConfig{
		Ingress: configv1alpha1.InlineIngress{
			Domain:           "workshop.example.com",
			IngressClassName: "contour",
			WildcardCertificateSecretRef: &configv1alpha1.SecretReference{
				Name:      "wildcard",
				Namespace: "shared-tls",
			},
			CACertificateSecretRef: &configv1alpha1.SecretReference{
				Name:      "byo-ca",
				Namespace: "shared-ca",
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ecc).Build()

	got, err := collectFromClient(context.Background(), c, "educates-installer")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"educates-installer", "shared-ca", "shared-tls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Inline wildcard and CA namespaces not folded in: got %v, want %v", got, want)
	}
}

func TestCollectFromECC_SessionManagerCAOverrideNS_Added(t *testing.T) {
	scheme := testScheme(t)
	sm := &platformv1alpha1.SessionManager{}
	sm.Name = singletonName
	sm.Spec.IngressOverrides = &platformv1alpha1.IngressOverrides{
		CACertificateSecretRef: &platformv1alpha1.SecretReference{
			Name:      "portal-ca",
			Namespace: "portal-ca-secrets",
		},
	}
	// No EducatesClusterConfig yet: the SessionManager is read on its own.
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(sm).Build()

	got, err := collectFromClient(context.Background(), c, "educates-installer")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"educates-installer", "portal-ca-secrets"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SessionManager CA override namespace not folded in: got %v, want %v", got, want)
	}
}

func TestCollectFromECC_ReferenceTheProviderDoesNotUse_Ignored(t *testing.T) {
	// A CustomCA block left under the StaticCertificate provider names
	// no Secret the reconciler reads or watches, so its namespace stays
	// out of the cache scope, as the Secret watch ignores it too.
	scheme := testScheme(t)
	ecc := &configv1alpha1.EducatesClusterConfig{}
	ecc.Name = singletonName
	ecc.Spec.Mode = configv1alpha1.ClusterConfigModeManaged
	ecc.Spec.Ingress = &configv1alpha1.Ingress{
		Certificates: configv1alpha1.Certificates{
			Provider: configv1alpha1.CertificatesProviderStaticCertificate,
			StaticCertificate: &configv1alpha1.StaticCertificateConfig{
				TLSSecretRef: configv1alpha1.SecretReference{
					Name:      "wildcard",
					Namespace: "static-tls",
				},
			},
			BundledCertManager: &configv1alpha1.BundledCertManagerConfig{
				IssuerType: configv1alpha1.IssuerTypeCustomCA,
				CustomCA: &configv1alpha1.CustomCAConfig{
					CAKeyPairSecretRef: configv1alpha1.SecretReference{
						Name:      "workshop-ca",
						Namespace: "unused-ca",
					},
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ecc).Build()

	got, err := collectFromClient(context.Background(), c, "educates-installer")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"educates-installer", "static-tls"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("a reference the provider does not use widened the cache scope: got %v, want %v", got, want)
	}
}

func TestCollectFromECC_EmptyNamespaceOnRef_Ignored(t *testing.T) {
	// ref.Namespace="" means "use operator namespace" — already in the
	// default set, so no new entry should be added.
	scheme := testScheme(t)
	ecc := &configv1alpha1.EducatesClusterConfig{}
	ecc.Name = singletonName
	ecc.Spec.Mode = configv1alpha1.ClusterConfigModeManaged
	ecc.Spec.Ingress = &configv1alpha1.Ingress{
		Certificates: configv1alpha1.Certificates{
			Provider: configv1alpha1.CertificatesProviderBundledCertManager,
			BundledCertManager: &configv1alpha1.BundledCertManagerConfig{
				IssuerType: configv1alpha1.IssuerTypeCustomCA,
				CustomCA: &configv1alpha1.CustomCAConfig{
					CAKeyPairSecretRef: configv1alpha1.SecretReference{
						Name: "workshop-ca",
						// Namespace intentionally empty
					},
				},
			},
		},
	}
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(ecc).Build()

	got, err := collectFromClient(context.Background(), c, "educates-installer")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"educates-installer"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("empty ref.Namespace should not add anything new: got %v, want %v", got, want)
	}
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(s))
	utilruntime.Must(configv1alpha1.AddToScheme(s))
	utilruntime.Must(platformv1alpha1.AddToScheme(s))
	return s
}
