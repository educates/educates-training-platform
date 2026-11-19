package deployer

import (
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v2"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/translator"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// exportFixture is an EducatesConfig with an operator block, a Secret
// reference in the EducatesClusterConfig and one inside a list.
func exportFixture() *v1alpha1.EducatesConfig {
	return &v1alpha1.EducatesConfig{
		TypeMeta: v1alpha1.TypeMeta{APIVersion: v1alpha1.APIVersion, Kind: v1alpha1.KindEducatesConfig},
		Operator: v1alpha1.LocalOperatorConfig{
			Image:    v1alpha1.OperatorImage{Repository: "localhost:5001/educates-operator", Tag: "latest", PullPolicy: "Always"},
			LogLevel: "info",
		},
		EducatesClusterConfig: map[string]interface{}{
			"mode": "Managed",
			"ingress": map[string]interface{}{
				"domain": "educates.test",
				"certificates": map[string]interface{}{
					"provider": "BundledCertManager",
					"bundledCertManager": map[string]interface{}{
						"customCA": map[string]interface{}{
							"caKeyPairSecretRef": map[string]interface{}{"name": "educates.test-ca-key-pair", "namespace": "educates-secrets"},
						},
					},
				},
			},
		},
		SecretsManager: map[string]interface{}{"logLevel": "info"},
		SessionManager: map[string]interface{}{
			"registries": []interface{}{
				map[string]interface{}{"host": "ghcr.io", "credentialsSecretRef": map[string]interface{}{"name": "ghcr"}},
			},
		},
	}
}

// TestExportOfDeploysBackTheSame checks that the export of what a deploy
// of a config applies, written as YAML and read back, translates to what
// the config translates to.
func TestExportOfDeploysBackTheSame(t *testing.T) {
	want := translator.TranslateEscape(exportFixture())

	data, err := ExportOf(want).YAML(nil)
	if err != nil {
		t.Fatalf("YAML: %v", err)
	}
	var back v1alpha1.EducatesConfig
	if err := yaml.Unmarshal(data, &back); err != nil {
		t.Fatalf("read the export back: %v\n%s", err, data)
	}
	got := translator.TranslateEscape(&back)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("the export translates to\n%+v\nwant\n%+v\nexport:\n%s", got, want, data)
	}
}

// TestExportOfLeavesOutAnAbsentLookupService checks that a deploy without
// the lookup service exports no lookupService section.
func TestExportOfLeavesOutAnAbsentLookupService(t *testing.T) {
	out := translator.TranslateEscape(exportFixture())

	data, err := ExportOf(out).YAML(nil)
	if err != nil {
		t.Fatalf("YAML: %v", err)
	}
	if strings.Contains(string(data), "lookupService:") {
		t.Errorf("export holds a lookupService section:\n%s", data)
	}
}

// TestExportNamesTheSecretsItReferences checks that every Secret
// reference is found, with the field that holds it, inside lists too.
func TestExportNamesTheSecretsItReferences(t *testing.T) {
	e := ExportOf(translator.TranslateEscape(exportFixture()))

	want := []SecretRef{
		{Namespace: "educates-secrets", Name: "educates.test-ca-key-pair", Field: "educatesClusterConfig.ingress.certificates.bundledCertManager.customCA.caKeyPairSecretRef"},
		{Name: "ghcr", Field: "sessionManager.registries[0].credentialsSecretRef"},
	}
	if !reflect.DeepEqual(e.SecretRefs, want) {
		t.Errorf("SecretRefs = %+v, want %+v", e.SecretRefs, want)
	}
}

// TestExportYAMLComments checks the comments an export starts with: the
// header, the Secrets it references, the operator values it cannot
// express and a missing operator release.
func TestExportYAMLComments(t *testing.T) {
	e := ExportOf(translator.TranslateEscape(exportFixture()))
	e.UnmappedValues = []string{"rbac"}
	e.NoOperatorRelease = true

	data, err := e.YAML([]string{"First header line.", ""})
	if err != nil {
		t.Fatalf("YAML: %v", err)
	}
	text := string(data)

	for _, want := range []string{
		"# First header line.\n#\n",
		"#   educates-secrets/educates.test-ca-key-pair (educatesClusterConfig.ingress.certificates.bundledCertManager.customCA.caKeyPairSecretRef)\n",
		"#   ghcr (sessionManager.registries[0].credentialsSecretRef)\n",
		"leaves unset: rbac\n",
		"no operator block.\n",
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesConfig\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("export lacks %q:\n%s", want, text)
		}
	}
}
