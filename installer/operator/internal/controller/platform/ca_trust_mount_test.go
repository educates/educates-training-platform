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

package platform

import (
	"context"
	"iter"
	"reflect"
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
	chart "helm.sh/helm/v4/pkg/chart/v2"
)

// Plain unit tests: each runtime chart is installed into an in-memory
// Helm store with the values the operator renders, and the resulting
// manifest is inspected. No envtest needed.

// renderedWorkload is the slice of a rendered Deployment or DaemonSet
// these tests look at.
type renderedWorkload struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name string `json:"name"`
	} `json:"metadata"`
	Spec struct {
		Template struct {
			Spec corev1.PodSpec `json:"spec"`
		} `json:"template"`
	} `json:"spec"`
}

// renderManifests installs the chart loadChart returns with vals into a
// memory-backed Helm client and yields the documents of its manifest.
func renderManifests(t *testing.T, loadChart func() (*chart.Chart, error), vals map[string]any) iter.Seq[string] {
	t.Helper()

	chrt, err := loadChart()
	if err != nil {
		t.Fatalf("load chart: %v", err)
	}
	hc, err := helm.NewMemoryClient(platformNamespace)
	if err != nil {
		t.Fatalf("NewMemoryClient: %v", err)
	}
	rel, err := hc.Install(context.Background(), chrt.Metadata.Name, chrt, vals)
	if err != nil {
		t.Fatalf("install %s: %v", chrt.Metadata.Name, err)
	}
	return strings.SplitSeq(rel.Manifest, "\n---")
}

// renderWorkloads returns the Deployments and DaemonSets among the
// documents renderManifests yields.
func renderWorkloads(t *testing.T, loadChart func() (*chart.Chart, error), vals map[string]any) []renderedWorkload {
	t.Helper()

	var workloads []renderedWorkload
	for doc := range renderManifests(t, loadChart, vals) {
		var w renderedWorkload
		if err := yaml.Unmarshal([]byte(doc), &w); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		if w.Kind == "Deployment" || w.Kind == "DaemonSet" {
			workloads = append(workloads, w)
		}
	}
	return workloads
}

// assertMountsOnlyCACrt fails unless at least one workload mounts the
// CA trust Secret, and every volume sourced from it projects only the
// ca.crt key.
func assertMountsOnlyCACrt(t *testing.T, workloads []renderedWorkload, caSecretName string) {
	t.Helper()

	want := []corev1.KeyToPath{{Key: "ca.crt", Path: "ca.crt"}}
	found := false
	for _, w := range workloads {
		for _, v := range w.Spec.Template.Spec.Volumes {
			if v.Secret == nil || v.Secret.SecretName != caSecretName {
				continue
			}
			found = true
			if !reflect.DeepEqual(v.Secret.Items, want) {
				t.Errorf("%s/%s volume %q mounts Secret %q with items %v, want only %v",
					w.Kind, w.Metadata.Name, v.Name, caSecretName, v.Secret.Items, want)
			}
		}
	}
	if !found {
		t.Fatalf("no rendered workload mounts the CA trust Secret %q", caSecretName)
	}
}

func TestSessionManagerChart_MountsOnlyCACrtFromCATrustSecret(t *testing.T) {
	cfg := valuesTestClusterConfig()
	values := renderSessionManagerValues(&platformv1alpha1.SessionManager{}, cfg, "educates-installer", testImageNamespace)

	workloads := renderWorkloads(t, vendoredcharts.SessionManager, values)

	assertMountsOnlyCACrt(t, workloads, cfg.Status.Ingress.CACertificateSecretRef.Name)
}

func TestLookupServiceChart_MountsOnlyCACrtFromCATrustSecret(t *testing.T) {
	cfg := valuesTestClusterConfig()
	values := renderLookupServiceValues(&platformv1alpha1.LookupService{}, cfg, testImageNamespace)

	workloads := renderWorkloads(t, vendoredcharts.LookupService, values)

	assertMountsOnlyCACrt(t, workloads, cfg.Status.Ingress.CACertificateSecretRef.Name)
}

func TestNodeCAInjectorChart_MountsOnlyCACrtFromCATrustSecret(t *testing.T) {
	cfg := valuesTestClusterConfig()
	values := renderNodeCAInjectorValues(&platformv1alpha1.SessionManager{}, cfg, testOperatorNamespace, testImageNamespace)

	workloads := renderWorkloads(t, vendoredcharts.NodeCAInjector, values)

	assertMountsOnlyCACrt(t, workloads, cfg.Status.Ingress.CACertificateSecretRef.Name)
}
