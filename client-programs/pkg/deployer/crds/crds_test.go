package crds

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/chart"
)

func TestCRDObjects_AllFourParseAsCRDs(t *testing.T) {
	chrt, err := chart.Load()
	if err != nil {
		t.Fatalf("chart.Load: %v", err)
	}
	objs := chrt.CRDObjects()
	if len(objs) != 4 {
		t.Fatalf("chart CRDObjects len = %d, want 4 (ECC + SecretsManager + LookupService + SessionManager)", len(objs))
	}

	wantKinds := map[string]bool{
		"EducatesClusterConfig": false,
		"SecretsManager":        false,
		"LookupService":         false,
		"SessionManager":        false,
	}
	for _, c := range objs {
		docs, err := splitYAMLDocs(c.File.Data)
		if err != nil {
			t.Fatalf("split %s: %v", c.Filename, err)
		}
		if len(docs) != 1 {
			t.Errorf("%s: %d docs, want 1", c.Filename, len(docs))
		}
		u := &unstructured.Unstructured{}
		if err := yaml.Unmarshal(docs[0], &u.Object); err != nil {
			t.Fatalf("parse %s: %v", c.Filename, err)
		}
		if got, want := u.GetKind(), "CustomResourceDefinition"; got != want {
			t.Errorf("%s: kind = %q, want %q", c.Filename, got, want)
		}
		// CRD name format is <plural>.<group> e.g. "educatesclusterconfigs.config.educates.dev"
		// We assert spec.names.kind is one of the four we expect.
		kind, found, err := unstructured.NestedString(u.Object, "spec", "names", "kind")
		if err != nil || !found {
			t.Errorf("%s: spec.names.kind not found: %v", c.Filename, err)
			continue
		}
		if _, ok := wantKinds[kind]; !ok {
			t.Errorf("%s: unexpected kind %q", c.Filename, kind)
			continue
		}
		wantKinds[kind] = true
	}
	for kind, found := range wantKinds {
		if !found {
			t.Errorf("CRD for kind %q not found in embedded chart", kind)
		}
	}
}

func TestSplitYAMLDocs_MultiDocFile(t *testing.T) {
	doc := []byte(`apiVersion: v1
kind: A
---
apiVersion: v1
kind: B
`)
	parts, err := splitYAMLDocs(doc)
	if err != nil {
		t.Fatal(err)
	}
	if len(parts) != 2 {
		t.Fatalf("len = %d, want 2", len(parts))
	}
	if !strings.Contains(string(parts[0]), "kind: A") {
		t.Errorf("first doc missing 'kind: A'")
	}
	if !strings.Contains(string(parts[1]), "kind: B") {
		t.Errorf("second doc missing 'kind: B'")
	}
}
