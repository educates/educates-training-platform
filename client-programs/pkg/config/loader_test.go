package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

func TestLoad_EmptyLocalConfig_AppliesDefaults(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "local-empty.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	local, ok := cfg.(*v1alpha1.EducatesLocalConfig)
	if !ok {
		t.Fatalf("expected *EducatesLocalConfig, got %T", cfg)
	}

	if got, want := local.Cluster.ListenAddress, "127.0.0.1"; got != want {
		t.Errorf("ListenAddress = %q, want %q", got, want)
	}
	if local.ClusterAdmin == nil || *local.ClusterAdmin != true {
		t.Errorf("ClusterAdmin = %v, want true", local.ClusterAdmin)
	}
	if local.LookupService == nil || *local.LookupService != true {
		t.Errorf("LookupService = %v, want true", local.LookupService)
	}
	if local.ImagePrePuller.Enabled == nil || *local.ImagePrePuller.Enabled != true {
		t.Errorf("ImagePrePuller.Enabled = %v, want true", local.ImagePrePuller.Enabled)
	}
	if local.ImagePrePuller.IncludeDefaultImages != nil {
		t.Errorf("ImagePrePuller.IncludeDefaultImages = %v, want unset", *local.ImagePrePuller.IncludeDefaultImages)
	}
	if got, want := local.Operator.LogLevel, "info"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q", got, want)
	}
}

func TestLoad_FullLocalConfig_RoundTripsAllFields(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "local-full.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	local := cfg.(*v1alpha1.EducatesLocalConfig)

	if got, want := local.Cluster.ListenAddress, "192.168.1.10"; got != want {
		t.Errorf("ListenAddress = %q, want %q", got, want)
	}
	if got, want := local.Cluster.ApiServer.Port, 6443; got != want {
		t.Errorf("ApiServer.Port = %d, want %d", got, want)
	}
	if got, want := len(local.Cluster.VolumeMounts), 1; got != want {
		t.Fatalf("VolumeMounts len = %d, want %d", got, want)
	}
	if got, want := local.Cluster.VolumeMounts[0].HostPath, "/tmp/data"; got != want {
		t.Errorf("VolumeMounts[0].HostPath = %q, want %q", got, want)
	}
	if local.ClusterAdmin == nil || *local.ClusterAdmin != false {
		t.Errorf("ClusterAdmin = %v, want false (explicit override)", local.ClusterAdmin)
	}
	if got, want := local.Operator.LogLevel, "debug"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q", got, want)
	}
	if got, want := local.Ingress.Domain, "workshop.test"; got != want {
		t.Errorf("Ingress.Domain = %q, want %q", got, want)
	}
	if got, want := len(local.Resolver.ExtraDomains), 2; got != want {
		t.Errorf("ExtraDomains len = %d, want %d", got, want)
	}
	if got, want := local.WebsiteStyling.DefaultTheme, "my-theme-data"; got != want {
		t.Errorf("WebsiteStyling.DefaultTheme = %q, want %q", got, want)
	}
	assertImagePrePuller(t, local.ImagePrePuller, false, false, []v1alpha1.PrePulledImage{
		{Name: "jdk21-environment"},
		{Image: "ghcr.io/educates/example:1.0"},
	})
}

// assertImagePrePuller checks an imagePrePuller block whose enabled and
// includeDefaultImages are both set.
func assertImagePrePuller(t *testing.T, got v1alpha1.ImagePrePullerConfig, enabled, includeDefaultImages bool, images []v1alpha1.PrePulledImage) {
	t.Helper()
	if got.Enabled == nil || *got.Enabled != enabled {
		t.Errorf("ImagePrePuller.Enabled = %v, want %v", got.Enabled, enabled)
	}
	if got.IncludeDefaultImages == nil || *got.IncludeDefaultImages != includeDefaultImages {
		t.Errorf("ImagePrePuller.IncludeDefaultImages = %v, want %v", got.IncludeDefaultImages, includeDefaultImages)
	}
	if !reflect.DeepEqual(got.Images, images) {
		t.Errorf("ImagePrePuller.Images = %+v, want %+v", got.Images, images)
	}
}

func TestLoad_LocalMultinodeConfig_ParsesNodes(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "local-multinode.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	local := cfg.(*v1alpha1.EducatesLocalConfig)

	nodes := local.Cluster.Nodes
	if got, want := len(nodes), 3; got != want {
		t.Fatalf("Cluster.Nodes len = %d, want %d", got, want)
	}

	if got, want := nodes[0].Role, "control-plane"; got != want {
		t.Errorf("nodes[0].Role = %q, want %q", got, want)
	}
	if got, want := nodes[0].Labels["tier"], "control"; got != want {
		t.Errorf("nodes[0].Labels[tier] = %q, want %q", got, want)
	}

	if got, want := nodes[1].Role, "worker"; got != want {
		t.Errorf("nodes[1].Role = %q, want %q", got, want)
	}
	if got, want := nodes[1].Labels["disktype"], "ssd"; got != want {
		t.Errorf("nodes[1].Labels[disktype] = %q, want %q", got, want)
	}

	if got, want := len(nodes[2].Taints), 1; got != want {
		t.Fatalf("nodes[2].Taints len = %d, want %d", got, want)
	}
	if taint := nodes[2].Taints[0]; taint.Key != "dedicated" || taint.Value != "gpu" || taint.Effect != "NoSchedule" {
		t.Errorf("nodes[2].Taints[0] = %+v, want {dedicated gpu NoSchedule}", taint)
	}
}

func TestLoad_FullGKEConfig_RoundTripsAllFields(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "gke-full.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	gke, ok := cfg.(*v1alpha1.EducatesGKEConfig)
	if !ok {
		t.Fatalf("expected *EducatesGKEConfig, got %T", cfg)
	}

	if got, want := gke.GCP.Project, "my-gcp-project"; got != want {
		t.Errorf("GCP.Project = %q, want %q", got, want)
	}
	// Explicit service accounts must survive WithDefaults (no
	// project-derived overwrite).
	if got, want := gke.GCP.CertManagerServiceAccount, "custom-cert-manager@my-gcp-project.iam.gserviceaccount.com"; got != want {
		t.Errorf("GCP.CertManagerServiceAccount = %q, want %q", got, want)
	}
	if got, want := gke.GCP.ExternalDNSServiceAccount, "custom-external-dns@my-gcp-project.iam.gserviceaccount.com"; got != want {
		t.Errorf("GCP.ExternalDNSServiceAccount = %q, want %q", got, want)
	}
	if got, want := gke.Domain, "academy-01.google.educates.dev"; got != want {
		t.Errorf("Domain = %q, want %q", got, want)
	}
	if got, want := gke.ACME.Email, "ops@example.com"; got != want {
		t.Errorf("ACME.Email = %q, want %q", got, want)
	}
	if got, want := gke.ACME.Server, "https://acme-staging-v02.api.letsencrypt.org/directory"; got != want {
		t.Errorf("ACME.Server = %q, want %q", got, want)
	}
	if !gke.ExternalTLSTermination {
		t.Errorf("ExternalTLSTermination = false, want true")
	}
	// Explicit toggles must override the kind defaults
	// (clusterAdmin=false, lookupService=true, imagePrePuller.enabled=true).
	if gke.ClusterAdmin == nil || *gke.ClusterAdmin != true {
		t.Errorf("ClusterAdmin = %v, want true (explicit override)", gke.ClusterAdmin)
	}
	if gke.LookupService == nil || *gke.LookupService != false {
		t.Errorf("LookupService = %v, want false (explicit override)", gke.LookupService)
	}
	assertImagePrePuller(t, gke.ImagePrePuller, false, false, []v1alpha1.PrePulledImage{
		{Name: "jdk21-environment"},
		{Image: "ghcr.io/example/custom-environment:1.0"},
	})
	if got, want := gke.WebsiteStyling.DefaultTheme, "my-theme-data"; got != want {
		t.Errorf("WebsiteStyling.DefaultTheme = %q, want %q", got, want)
	}
	if got, want := len(gke.WebsiteStyling.ThemeDataRefs), 1; got != want {
		t.Fatalf("ThemeDataRefs len = %d, want %d", got, want)
	}
	if got, want := gke.WebsiteStyling.ThemeDataRefs[0].Namespace, "educates"; got != want {
		t.Errorf("ThemeDataRefs[0].Namespace = %q, want %q", got, want)
	}
	if got, want := len(gke.SecretPropagation.ImagePullSecretNames), 1; got != want {
		t.Errorf("ImagePullSecretNames len = %d, want %d", got, want)
	}
	if got, want := len(gke.ImageVersions), 1; got != want {
		t.Fatalf("ImageVersions len = %d, want %d", got, want)
	}
	if got, want := gke.ImageVersions[0].Image, "ghcr.io/educates/base-environment:4.0.0"; got != want {
		t.Errorf("ImageVersions[0].Image = %q, want %q", got, want)
	}
	if got, want := gke.Operator.Image.PullPolicy, "IfNotPresent"; got != want {
		t.Errorf("Operator.Image.PullPolicy = %q, want %q", got, want)
	}
	if got, want := len(gke.Operator.ImagePullSecrets), 1; got != want {
		t.Errorf("Operator.ImagePullSecrets len = %d, want %d", got, want)
	}
	if got, want := gke.Operator.LogLevel, "debug"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q", got, want)
	}
}

func TestLoad_FullEKSConfig_RoundTripsAllFields(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "eks-full.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	eks, ok := cfg.(*v1alpha1.EducatesEKSConfig)
	if !ok {
		t.Fatalf("expected *EducatesEKSConfig, got %T", cfg)
	}

	if got, want := eks.AWS.AccountId, "123456789012"; got != want {
		t.Errorf("AWS.AccountId = %q, want %q", got, want)
	}
	if got, want := eks.AWS.Region, "us-east-1"; got != want {
		t.Errorf("AWS.Region = %q, want %q", got, want)
	}
	if got, want := eks.AWS.Route53HostedZoneId, "Z0123456789ABCDEF"; got != want {
		t.Errorf("AWS.Route53HostedZoneId = %q, want %q", got, want)
	}
	// Explicit role ARNs must survive WithDefaults (no account-derived
	// overwrite).
	if got, want := eks.AWS.CertManagerRoleARN, "arn:aws:iam::123456789012:role/custom-cert-manager"; got != want {
		t.Errorf("AWS.CertManagerRoleARN = %q, want %q", got, want)
	}
	if got, want := eks.AWS.ExternalDNSRoleARN, "arn:aws:iam::123456789012:role/custom-external-dns"; got != want {
		t.Errorf("AWS.ExternalDNSRoleARN = %q, want %q", got, want)
	}
	if got, want := eks.Domain, "academy-01.workshops.example.com"; got != want {
		t.Errorf("Domain = %q, want %q", got, want)
	}
	if got, want := eks.ACME.Server, "https://acme-staging-v02.api.letsencrypt.org/directory"; got != want {
		t.Errorf("ACME.Server = %q, want %q", got, want)
	}
	if !eks.ExternalTLSTermination {
		t.Errorf("ExternalTLSTermination = false, want true")
	}
	if eks.ClusterAdmin == nil || *eks.ClusterAdmin != true {
		t.Errorf("ClusterAdmin = %v, want true (explicit override)", eks.ClusterAdmin)
	}
	if eks.LookupService == nil || *eks.LookupService != false {
		t.Errorf("LookupService = %v, want false (explicit override)", eks.LookupService)
	}
	assertImagePrePuller(t, eks.ImagePrePuller, false, false, []v1alpha1.PrePulledImage{
		{Name: "jdk21-environment"},
		{Image: "ghcr.io/example/custom-environment:1.0"},
	})
	if got, want := eks.WebsiteStyling.DefaultTheme, "my-theme-data"; got != want {
		t.Errorf("WebsiteStyling.DefaultTheme = %q, want %q", got, want)
	}
	if got, want := len(eks.SecretPropagation.ImagePullSecretNames), 1; got != want {
		t.Errorf("ImagePullSecretNames len = %d, want %d", got, want)
	}
	if got, want := len(eks.ImageVersions), 1; got != want {
		t.Errorf("ImageVersions len = %d, want %d", got, want)
	}
	if got, want := eks.Operator.Image.PullPolicy, "IfNotPresent"; got != want {
		t.Errorf("Operator.Image.PullPolicy = %q, want %q", got, want)
	}
	if got, want := eks.Operator.LogLevel, "debug"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q", got, want)
	}
}

func TestLoad_FullInlineConfig_RoundTripsAllFields(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "inline-full.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	inline, ok := cfg.(*v1alpha1.EducatesInlineConfig)
	if !ok {
		t.Fatalf("expected *EducatesInlineConfig, got %T", cfg)
	}

	if got, want := inline.Domain, "workshops.example.com"; got != want {
		t.Errorf("Domain = %q, want %q", got, want)
	}
	if got, want := inline.IngressClassName, "openshift-default"; got != want {
		t.Errorf("IngressClassName = %q, want %q", got, want)
	}
	if got, want := inline.WildcardCertificateSecret, "educates-wildcard-tls"; got != want {
		t.Errorf("WildcardCertificateSecret = %q, want %q", got, want)
	}
	if got, want := inline.CACertificateSecret, "educates-wildcard-ca"; got != want {
		t.Errorf("CACertificateSecret = %q, want %q", got, want)
	}
	if got, want := inline.ClusterIssuerName, "corp-ca-issuer"; got != want {
		t.Errorf("ClusterIssuerName = %q, want %q", got, want)
	}
	if got, want := inline.ImageRegistry.Prefix, "registry.internal.example.com/educates"; got != want {
		t.Errorf("ImageRegistry.Prefix = %q, want %q", got, want)
	}
	if got, want := len(inline.ImageRegistry.PullSecrets), 1; got != want {
		t.Errorf("ImageRegistry.PullSecrets len = %d, want %d", got, want)
	}
	// Explicit engines must override the Kyverno/Kyverno defaults.
	if got, want := inline.PolicyEnforcement.ClusterEngine, "PodSecurityStandards"; got != want {
		t.Errorf("PolicyEnforcement.ClusterEngine = %q, want %q", got, want)
	}
	if got, want := inline.PolicyEnforcement.WorkshopEngine, "None"; got != want {
		t.Errorf("PolicyEnforcement.WorkshopEngine = %q, want %q", got, want)
	}
	if !inline.ExternalTLSTermination {
		t.Errorf("ExternalTLSTermination = false, want true")
	}
	if inline.ClusterAdmin == nil || *inline.ClusterAdmin != true {
		t.Errorf("ClusterAdmin = %v, want true (explicit override)", inline.ClusterAdmin)
	}
	if inline.LookupService == nil || *inline.LookupService != false {
		t.Errorf("LookupService = %v, want false (explicit override)", inline.LookupService)
	}
	assertImagePrePuller(t, inline.ImagePrePuller, false, false, []v1alpha1.PrePulledImage{
		{Name: "jdk21-environment"},
		{Image: "ghcr.io/example/custom-environment:1.0"},
	})
	if got, want := inline.WebsiteStyling.DefaultTheme, "my-theme-data"; got != want {
		t.Errorf("WebsiteStyling.DefaultTheme = %q, want %q", got, want)
	}
	if got, want := len(inline.SecretPropagation.ImagePullSecretNames), 1; got != want {
		t.Errorf("ImagePullSecretNames len = %d, want %d", got, want)
	}
	if got, want := len(inline.ImageVersions), 1; got != want {
		t.Errorf("ImageVersions len = %d, want %d", got, want)
	}
	if got, want := inline.Operator.Image.PullPolicy, "IfNotPresent"; got != want {
		t.Errorf("Operator.Image.PullPolicy = %q, want %q", got, want)
	}
	if got, want := inline.Operator.LogLevel, "debug"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q", got, want)
	}
}

func TestLoad_Errors(t *testing.T) {
	cases := []struct {
		name     string
		file     string
		contains string
	}{
		{"unknown-field",   "local-unknown-field.yaml",  "bogusField"},
		{"bad-enum",        "local-bad-loglevel.yaml",   "logLevel"},
		{"wrong-apiVersion","wrong-apiversion.yaml",     "unsupported apiVersion"},
		{"unknown-kind",    "unknown-kind.yaml",         "unknown kind"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Load(filepath.Join("testdata", tc.file))
			if err == nil {
				t.Fatalf("Load: expected error, got nil")
			}
			if !strings.Contains(err.Error(), tc.contains) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.contains)
			}
		})
	}
}

// narrowKindFixtures are a minimal valid file of each narrow kind, to which
// a test appends the fields it exercises.
var narrowKindFixtures = []string{"local-empty.yaml", "gke-minimal.yaml", "eks-minimal.yaml", "inline-minimal.yaml"}

// withYAML returns the fixture's contents with extra appended.
func withYAML(t *testing.T, fixture, extra string) []byte {
	t.Helper()
	base, err := os.ReadFile(filepath.Join("testdata", fixture))
	if err != nil {
		t.Fatal(err)
	}
	return append(append(base, '\n'), extra...)
}

// A file holding the boolean form of imagePrePuller fails to load with an
// error naming the object form to write in its place.
func TestLoad_ImagePrePullerBooleanForm_NamesObjectForm(t *testing.T) {
	for _, fixture := range narrowKindFixtures {
		for _, value := range []string{"true", "false"} {
			t.Run(fixture+"/"+value, func(t *testing.T) {
				_, err := LoadBytes(withYAML(t, fixture, "imagePrePuller: "+value+"\n"), fixture)
				if err == nil {
					t.Fatal("LoadBytes: expected error, got nil")
				}
				for _, want := range []string{"imagePrePuller.enabled", "imagePrePuller: {enabled: " + value + "}"} {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("error %q does not contain %q", err, want)
					}
				}
				if strings.Contains(err.Error(), "Invalid type") {
					t.Errorf("error %q still reports only the type mismatch", err)
				}
			})
		}
	}
}

// An imagePrePuller.images entry giving both or neither of name and image
// fails to load, the error naming the entry and the rule it breaks.
func TestLoad_ImagePrePullerEntryNotExactlyOne_Rejected(t *testing.T) {
	cases := []struct {
		name  string
		entry string
	}{
		{"both", "{name: jdk21-environment, image: ghcr.io/example/custom:1.0}"},
		{"neither", "{}"},
	}
	for _, fixture := range narrowKindFixtures {
		for _, tc := range cases {
			t.Run(fixture+"/"+tc.name, func(t *testing.T) {
				extra := "imagePrePuller:\n  images:\n    - name: base-environment\n    - " + tc.entry + "\n"
				_, err := LoadBytes(withYAML(t, fixture, extra), fixture)
				if err == nil {
					t.Fatal("LoadBytes: expected error, got nil")
				}
				want := "imagePrePuller.images.1: must set exactly one of name and image"
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not contain %q", err, want)
				}
				for _, noise := range []string{"oneOf", "is required"} {
					if strings.Contains(err.Error(), noise) {
						t.Errorf("error %q still carries the schema's own report %q", err, noise)
					}
				}
			})
		}
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "does-not-exist.yaml"))
	if err == nil {
		t.Fatal("Load: expected error for missing file")
	}
}

func TestLoadLocal_RejectsNonLocalKind(t *testing.T) {
	_, err := LoadLocal(filepath.Join("testdata", "escape-minimal.yaml"))
	if err == nil {
		t.Fatal("LoadLocal: expected error for EducatesConfig kind")
	}
	if !strings.Contains(err.Error(), "expected kind") {
		t.Errorf("error %q does not mention expected kind", err.Error())
	}
}

func TestLoad_EducatesConfig_Minimal(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "escape-minimal.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	esc, ok := cfg.(*v1alpha1.EducatesConfig)
	if !ok {
		t.Fatalf("expected *EducatesConfig, got %T", cfg)
	}
	if esc.Target != nil {
		t.Errorf("Target = %+v, want nil", esc.Target)
	}
}

func TestLoad_EducatesConfig_WithTarget(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "escape-with-target.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	esc := cfg.(*v1alpha1.EducatesConfig)
	if esc.Target == nil {
		t.Fatal("Target = nil, want populated")
	}
	if got, want := esc.Target.Provider, "kind"; got != want {
		t.Errorf("Target.Provider = %q, want %q", got, want)
	}
	if got, want := esc.Operator.LogLevel, "debug"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q (no defaulting for escape kind)", got, want)
	}
	if esc.SecretsManager == nil {
		t.Errorf("SecretsManager = nil, want empty map")
	}
}

func TestLoad_EducatesInlineConfig_Minimal(t *testing.T) {
	cfg, err := Load(filepath.Join("testdata", "inline-minimal.yaml"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	inline, ok := cfg.(*v1alpha1.EducatesInlineConfig)
	if !ok {
		t.Fatalf("expected *EducatesInlineConfig, got %T", cfg)
	}
	if got, want := inline.Domain, "workshop.test"; got != want {
		t.Errorf("Domain = %q, want %q", got, want)
	}
	// Defaults applied.
	if got, want := inline.Operator.LogLevel, "info"; got != want {
		t.Errorf("Operator.LogLevel = %q, want %q", got, want)
	}
	if got, want := inline.PolicyEnforcement.ClusterEngine, "Kyverno"; got != want {
		t.Errorf("PolicyEnforcement.ClusterEngine default = %q, want %q", got, want)
	}
}

func TestLoad_EducatesInlineConfig_MissingRequired(t *testing.T) {
	cfg := []byte("apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesInlineConfig\n")
	_, err := LoadBytes(cfg, "test")
	if err == nil {
		t.Fatal("expected error for missing required fields")
	}
	// Schema should call out one of the required fields.
	for _, want := range []string{"domain", "ingressClassName", "wildcardCertificateSecret"} {
		if strings.Contains(err.Error(), want) {
			return
		}
	}
	t.Errorf("error %q does not mention any required Inline field", err)
}

func TestLoad_EducatesConfig_BogusEnvelopeField(t *testing.T) {
	_, err := Load(filepath.Join("testdata", "escape-bogus-envelope-field.yaml"))
	if err == nil {
		t.Fatal("Load: expected error for unknown envelope field")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("error %q does not mention bogus field", err.Error())
	}
}
