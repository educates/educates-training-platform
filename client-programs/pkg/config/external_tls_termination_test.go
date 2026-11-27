package config

import (
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// An EducatesLocalConfig may set ingress.externalTLSTermination, as the
// cloud scenario kinds set externalTLSTermination.
func TestLoad_LocalExternalTLSTermination_Loads(t *testing.T) {
	cfg, err := LoadBytes([]byte("apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"ingress:\n  domain: educates.test\n  externalTLSTermination: true\n"), "config.yaml")
	if err != nil {
		t.Fatalf("LoadBytes: %v", err)
	}
	local := cfg.(*v1alpha1.EducatesLocalConfig)
	if !local.Ingress.ExternalTLSTermination {
		t.Errorf("Ingress.ExternalTLSTermination = false, want true")
	}
	if local.Ingress.Insecure {
		t.Errorf("Ingress.Insecure = true, want false")
	}
}

// A config can't declare a cluster both served over plain HTTP and served
// over https with TLS ended outside it: the error names both fields, in
// place of the schema's generic report.
func TestLoad_LocalExternalTLSTerminationWithInsecure_NamesBoth(t *testing.T) {
	_, err := LoadBytes([]byte("apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
		"ingress:\n  insecure: true\n  externalTLSTermination: true\n"), "config.yaml")
	if err == nil {
		t.Fatal("LoadBytes: expected error, got nil")
	}
	want := "config.yaml: schema validation failed:\n" +
		"  - ingress: insecure and externalTLSTermination can't both be true: insecure serves the cluster " +
		"over plain HTTP, and externalTLSTermination serves it over https with TLS ended at the router; set one of them"
	if err.Error() != want {
		t.Errorf("error = %q, want %q", err, want)
	}
}

// Either one set to false leaves the other free.
func TestLoad_LocalExternalTLSTerminationFalseWithInsecure_Loads(t *testing.T) {
	for _, body := range []string{
		"ingress:\n  insecure: true\n  externalTLSTermination: false\n",
		"ingress:\n  insecure: false\n  externalTLSTermination: true\n",
	} {
		if _, err := LoadBytes([]byte("apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+body), "config.yaml"); err != nil {
			t.Errorf("LoadBytes(%q): %v", body, err)
		}
	}
}
