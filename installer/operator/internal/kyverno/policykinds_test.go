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

package kyverno

import (
	"errors"
	"strings"
	"testing"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"

	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// fakeDiscovery answers ServerResourcesForGroupVersion from a fixed set
// of served group versions, each listing the kinds it serves. A group
// version it does not hold answers NotFound, as the API server does.
type fakeDiscovery struct {
	served map[string][]string
	err    error
}

func (f fakeDiscovery) ServerResourcesForGroupVersion(groupVersion string) (*metav1.APIResourceList, error) {
	if f.err != nil {
		return nil, f.err
	}
	kinds, ok := f.served[groupVersion]
	if !ok {
		return nil, apierrors.NewNotFound(schema.GroupResource{}, groupVersion)
	}
	list := &metav1.APIResourceList{GroupVersion: groupVersion}
	for _, kind := range kinds {
		list.APIResources = append(list.APIResources, metav1.APIResource{Kind: kind})
	}
	return list, nil
}

func TestMissingPolicyKindsNoneWhenKyvernoServesThem(t *testing.T) {
	d := fakeDiscovery{served: map[string][]string{
		"policies.kyverno.io/v1alpha1": {"ValidatingPolicy", "MutatingPolicy"},
		"policies.kyverno.io/v1":       {"ValidatingPolicy"},
	}}

	missing, err := MissingPolicyKinds(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(missing) != 0 {
		t.Errorf("expected no missing kinds, got %v", missing)
	}
}

func TestMissingPolicyKindsAllWhenKyvernoIsNotInstalled(t *testing.T) {
	missing, err := MissingPolicyKinds(fakeDiscovery{})
	if err != nil {
		t.Fatalf("a group version the server does not serve is not an error, got: %v", err)
	}
	want := []schema.GroupVersionKind{
		{Group: "policies.kyverno.io", Version: "v1alpha1", Kind: "ValidatingPolicy"},
		{Group: "policies.kyverno.io", Version: "v1", Kind: "ValidatingPolicy"},
	}
	if len(missing) != len(want) {
		t.Fatalf("expected %v, got %v", want, missing)
	}
	for i := range want {
		if missing[i] != want[i] {
			t.Errorf("missing[%d] = %v, want %v", i, missing[i], want[i])
		}
	}
}

func TestMissingPolicyKindsReportsAKindAbsentFromAServedVersion(t *testing.T) {
	d := fakeDiscovery{served: map[string][]string{
		"policies.kyverno.io/v1alpha1": {"ValidatingPolicy"},
		"policies.kyverno.io/v1":       {"MutatingPolicy"},
	}}

	missing, err := MissingPolicyKinds(d)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := schema.GroupVersionKind{Group: "policies.kyverno.io", Version: "v1", Kind: "ValidatingPolicy"}
	if len(missing) != 1 || missing[0] != want {
		t.Errorf("expected [%v], got %v", want, missing)
	}
}

func TestMissingPolicyKindsReturnsOtherDiscoveryErrors(t *testing.T) {
	cause := errors.New("connection refused")

	if _, err := MissingPolicyKinds(fakeDiscovery{err: cause}); !errors.Is(err, cause) {
		t.Errorf("expected the discovery error to be returned, got %v", err)
	}
}

func TestMissingPolicyKindsRequiresADiscoveryClient(t *testing.T) {
	if _, err := MissingPolicyKinds(nil); err == nil {
		t.Error("expected an error for a nil discovery client")
	}
}

func TestNotServedMessageNamesEachMissingKind(t *testing.T) {
	msg := NotServedMessage([]schema.GroupVersionKind{
		{Group: "policies.kyverno.io", Version: "v1alpha1", Kind: "ValidatingPolicy"},
		{Group: "policies.kyverno.io", Version: "v1", Kind: "ValidatingPolicy"},
	})

	for _, want := range []string{
		"the API server does not serve",
		"ValidatingPolicy (policies.kyverno.io/v1alpha1)",
		"ValidatingPolicy (policies.kyverno.io/v1)",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message %q does not contain %q", msg, want)
		}
	}
}

// The session-manager chart installs its bundled cluster-wide policies,
// and the runtime creates the bundled per-workshop policies, so every
// kind those files use must be one the presence check asks for. A chart
// change that adds a kind or an API version fails here rather than on a
// cluster whose Kyverno does not serve it.
func TestPolicyKindsCoverTheSessionManagerChartPolicies(t *testing.T) {
	chrt, err := vendoredcharts.SessionManager()
	if err != nil {
		t.Fatalf("load embedded session-manager chart: %v", err)
	}

	required := map[schema.GroupVersionKind]bool{}
	for _, gvk := range PolicyKinds {
		required[gvk] = true
	}

	var policyFiles int
	for _, f := range chrt.Files {
		if !strings.HasPrefix(f.Name, "files/kyverno-policies/") || !strings.HasSuffix(f.Name, ".yaml") {
			continue
		}
		policyFiles++
		var doc struct {
			APIVersion string `json:"apiVersion"`
			Kind       string `json:"kind"`
		}
		if err := yaml.Unmarshal(f.Data, &doc); err != nil {
			t.Fatalf("parse %s: %v", f.Name, err)
		}
		gv, err := schema.ParseGroupVersion(doc.APIVersion)
		if err != nil {
			t.Fatalf("parse apiVersion of %s: %v", f.Name, err)
		}
		if gvk := gv.WithKind(doc.Kind); !required[gvk] {
			t.Errorf("%s uses %v, which PolicyKinds does not require", f.Name, gvk)
		}
	}
	if policyFiles == 0 {
		t.Fatal("the embedded session-manager chart holds no files/kyverno-policies/*.yaml; has the chart layout changed?")
	}
}
