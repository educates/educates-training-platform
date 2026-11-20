package deployer

import (
	"bytes"
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// TestComparableTextDropsServerSetFields checks that a live object and a
// dry run of an apply that leaves it as it is compare equal, although the
// API server sets their status and write metadata differently.
func TestComparableTextDropsServerSetFields(t *testing.T) {
	live := &unstructured.Unstructured{Object: map[string]interface{}{
		"apiVersion": "platform.educates.dev/v1alpha1",
		"kind":       "SecretsManager",
		"metadata": map[string]interface{}{
			"name":              "cluster",
			"resourceVersion":   "642",
			"generation":        int64(3),
			"uid":               "a",
			"creationTimestamp": "2026-11-18T19:23:40Z",
			"managedFields":     []interface{}{map[string]interface{}{"manager": "educates-cli"}},
			"finalizers":        []interface{}{"educates.dev/finalizer"},
		},
		"spec":   map[string]interface{}{"logLevel": "info"},
		"status": map[string]interface{}{"phase": "Ready"},
	}}
	next := live.DeepCopy()
	next.SetResourceVersion("643")
	next.SetGeneration(4)
	unstructured.RemoveNestedField(next.Object, "status")

	before, after, err := comparableTexts(live, next)
	if err != nil {
		t.Fatalf("comparableTexts: %v", err)
	}
	if before != after {
		t.Fatalf("texts differ:\n%s\n---\n%s", before, after)
	}
	for _, dropped := range []string{"resourceVersion", "generation", "uid", "creationTimestamp", "managedFields", "status", "phase"} {
		if strings.Contains(before, dropped) {
			t.Errorf("comparable text holds %q:\n%s", dropped, before)
		}
	}
	for _, kept := range []string{"finalizers", "logLevel: info", "name: cluster"} {
		if !strings.Contains(before, kept) {
			t.Errorf("comparable text lacks %q:\n%s", kept, before)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := []struct{ before, after, want string }{
		{"a: 1\n", "a: 1\n", changeUnchanged},
		{"", "a: 1\n", changeCreated},
		{"a: 1\n", "a: 2\n", changeChanged},
	}
	for _, tc := range cases {
		if got := classify(tc.before, tc.after); got != tc.want {
			t.Errorf("classify(%q, %q) = %q, want %q", tc.before, tc.after, got, tc.want)
		}
	}
}

// TestUnifiedDiff checks the diff of a changed and of a created object:
// headed by the cluster against the deploy, with no line after the last
// one of either text.
func TestUnifiedDiff(t *testing.T) {
	changed := unifiedDiff("a: 1\nb: 2\n", "a: 1\nb: 3\n")
	want := "--- cluster\n+++ deploy\n@@ -1,2 +1,2 @@\n a: 1\n-b: 2\n+b: 3\n"
	if changed != want {
		t.Errorf("changed diff =\n%q\nwant\n%q", changed, want)
	}

	created := unifiedDiff("", "a: 1\nb: 2\n")
	want = "--- cluster\n+++ deploy\n@@ -0,0 +1,2 @@\n+a: 1\n+b: 2\n"
	if created != want {
		t.Errorf("created diff =\n%q\nwant\n%q", created, want)
	}
}

// TestManifestObjects checks that the objects of a release manifest are
// keyed by kind and name, with the namespace a manifest sets, and that a
// document holding only a comment is skipped.
func TestManifestObjects(t *testing.T) {
	manifest := `---
# Source: educates-installer/templates/serviceaccount.yaml
apiVersion: v1
kind: ServiceAccount
metadata:
  name: educates-installer
  namespace: educates-installer
---
# Source: educates-installer/templates/rbac/role.yaml
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: educates:installer:manager
rules: []
`
	objects, err := manifestObjects(manifest)
	if err != nil {
		t.Fatalf("manifestObjects: %v", err)
	}
	if got := sortedKeys(objects); strings.Join(got, ",") != "ClusterRole educates:installer:manager,ServiceAccount educates-installer/educates-installer" {
		t.Errorf("keys = %q", got)
	}

	empty, err := manifestObjects("")
	if err != nil || len(empty) != 0 {
		t.Errorf("manifestObjects(\"\") = %v, %v, want no objects", empty, err)
	}
}

// TestReportRowsAlignChangesAndIndentDetail checks the layout of a
// section: names padded so the changes line up, and the detail of a row
// indented below it.
func TestReportRowsAlignChangesAndIndentDetail(t *testing.T) {
	var out bytes.Buffer
	r := &diffReport{w: &out}

	r.rows([]diffRow{
		{name: "EducatesClusterConfig cluster", change: changeUnchanged},
		{name: "SecretsManager cluster", change: changeChanged, detail: "-a\n+b\n"},
	})

	want := "" +
		"  EducatesClusterConfig cluster  unchanged\n" +
		"  SecretsManager cluster         changed\n" +
		"    -a\n" +
		"    +b\n"
	if got := out.String(); got != want {
		t.Errorf("rows =\n%s\nwant\n%s", got, want)
	}
}
