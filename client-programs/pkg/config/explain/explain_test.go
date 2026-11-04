package explain

import (
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1/schemas"
)

func TestExplain_Root_ListsTopLevelFields(t *testing.T) {
	out, err := Explain(schemas.EducatesLocalConfig, "")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !strings.Contains(out, "KIND:     EducatesLocalConfig") {
		t.Errorf("root explain should name the kind:\n%s", out)
	}
	for _, want := range []string{"apiVersion", "cluster", "ingress", "operator"} {
		if !strings.Contains(out, want) {
			t.Errorf("root explain missing field %q:\n%s", want, out)
		}
	}
}

func TestExplain_NestedObject_ListsSubFields(t *testing.T) {
	out, err := Explain(schemas.EducatesLocalConfig, "ingress")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !strings.Contains(out, "FIELD:    ingress <Object>") {
		t.Errorf("expected ingress to render as an Object field:\n%s", out)
	}
	for _, want := range []string{"domain", "insecure"} {
		if !strings.Contains(out, want) {
			t.Errorf("ingress explain missing sub-field %q:\n%s", want, out)
		}
	}
}

func TestExplain_LeafScalar_ShowsTypeDefaultAndDescription(t *testing.T) {
	out, err := Explain(schemas.EducatesLocalConfig, "ingress.insecure")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !strings.Contains(out, "FIELD:    ingress.insecure <boolean>") {
		t.Errorf("expected a boolean leaf:\n%s", out)
	}
	if !strings.Contains(out, "DEFAULT:") {
		t.Errorf("insecure has a schema default that should be shown:\n%s", out)
	}
	if !strings.Contains(out, "plain HTTP") {
		t.Errorf("insecure description should be surfaced:\n%s", out)
	}
}

func TestExplain_ArrayField_DrillsIntoItems(t *testing.T) {
	out, err := Explain(schemas.EducatesLocalConfig, "cluster.registryMirrors")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !strings.Contains(out, "[]Object") {
		t.Errorf("registryMirrors should render as a list of objects:\n%s", out)
	}
	if !strings.Contains(out, "mirror") {
		t.Errorf("registryMirrors items should expose their fields:\n%s", out)
	}
}

func TestExplain_LeadingKindPrefix_Tolerated(t *testing.T) {
	withPrefix, err := Explain(schemas.EducatesLocalConfig, "EducatesLocalConfig.ingress")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !strings.Contains(withPrefix, "FIELD:    ingress <Object>") {
		t.Errorf("a leading <Kind>. should be tolerated:\n%s", withPrefix)
	}
}

func TestExplain_UnknownField_Errors(t *testing.T) {
	_, err := Explain(schemas.EducatesLocalConfig, "ingress.bogus")
	if err == nil {
		t.Fatal("expected an error for an unknown field")
	}
	if !strings.Contains(err.Error(), "available fields") {
		t.Errorf("error should list available fields, got: %v", err)
	}
}

// The scenario kinds are now documented; spot-check that each explains a
// kind-specific field with a real description rather than a placeholder.
func TestExplain_ScenarioKinds_Documented(t *testing.T) {
	cases := []struct {
		schema     []byte
		path       string
		wantSubstr string
	}{
		{schemas.EducatesGKEConfig, "gcp.project", "Cloud DNS"},
		{schemas.EducatesEKSConfig, "aws.route53HostedZoneId", "hosted zone"},
		{schemas.EducatesInlineConfig, "ingressClassName", "IngressClass"},
	}
	for _, tc := range cases {
		out, err := Explain(tc.schema, tc.path)
		if err != nil {
			t.Fatalf("Explain(%s): %v", tc.path, err)
		}
		if strings.Contains(out, "<no description in schema>") {
			t.Errorf("%s should be documented now:\n%s", tc.path, out)
		}
		if !strings.Contains(out, tc.wantSubstr) {
			t.Errorf("%s explain missing %q:\n%s", tc.path, tc.wantSubstr, out)
		}
	}
}

// Every narrow kind explains the imagePrePuller block's fields, down to
// the fields of an images entry.
func TestExplain_ImagePrePuller_DescribesBlock(t *testing.T) {
	kinds := map[string][]byte{
		"local":  schemas.EducatesLocalConfig,
		"gke":    schemas.EducatesGKEConfig,
		"eks":    schemas.EducatesEKSConfig,
		"inline": schemas.EducatesInlineConfig,
	}
	for name, schema := range kinds {
		t.Run(name, func(t *testing.T) {
			out, err := Explain(schema, "imagePrePuller")
			if err != nil {
				t.Fatalf("Explain: %v", err)
			}
			for _, want := range []string{
				"FIELD:    imagePrePuller <Object>",
				"enabled <boolean>",
				"includeDefaultImages <boolean>",
				"images <[]Object>",
			} {
				if !strings.Contains(out, want) {
					t.Errorf("imagePrePuller explain missing %q:\n%s", want, out)
				}
			}

			out, err = Explain(schema, "imagePrePuller.images")
			if err != nil {
				t.Fatalf("Explain: %v", err)
			}
			for _, want := range []string{"name <string>", "image <string>", "exactly one of name and image"} {
				if !strings.Contains(out, want) {
					t.Errorf("imagePrePuller.images explain missing %q:\n%s", want, out)
				}
			}

			out, err = Explain(schema, "imagePrePuller.enabled")
			if err != nil {
				t.Fatalf("Explain: %v", err)
			}
			if !strings.Contains(out, "DEFAULT:  true") {
				t.Errorf("imagePrePuller.enabled should show its default of true:\n%s", out)
			}
		})
	}
}

// The escape-hatch schema is CRD-derived and uses $ref/$defs; exercise
// that the walker resolves refs and surfaces the rich descriptions.
func TestExplain_EscapeHatch_ResolvesRefs(t *testing.T) {
	out, err := Explain(schemas.EducatesConfig, "educatesClusterConfig")
	if err != nil {
		t.Fatalf("Explain: %v", err)
	}
	if !strings.Contains(out, "FIELD:    educatesClusterConfig <Object>") {
		t.Errorf("a $ref-typed field should resolve to its Object target:\n%s", out)
	}
	if !strings.Contains(out, "FIELDS:") {
		t.Errorf("resolved $ref target should list its fields:\n%s", out)
	}
}
