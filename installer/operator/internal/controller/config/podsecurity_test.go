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
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"helm.sh/helm/v4/pkg/chart/loader"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	psaapi "k8s.io/pod-security-admission/api"
	psapolicy "k8s.io/pod-security-admission/policy"
	"sigs.k8s.io/yaml"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/helm"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// The operator leaves the cert-manager, Kyverno and external-dns
// namespaces at the cluster's default Pod Security level, which can be
// as strict as restricted, so every pod those charts render with the
// operator's values must meet the restricted level. A chart upgrade that
// adds a hostPath volume or drops a securityContext fails here rather
// than on a hardened cluster.
func TestUnlabelledClusterServiceNamespacesMeetRestricted(t *testing.T) {
	spec := validManagedSpec()
	spec.DNS = &configv1alpha1.DNS{
		Provider: configv1alpha1.DNSProviderBundledExternalDNS,
		BundledExternalDNS: &configv1alpha1.BundledExternalDNSConfig{
			Provider: configv1alpha1.DNS01ProviderRoute53,
			Route53: &configv1alpha1.ExternalDNSRoute53Config{
				HostedZoneID: "Z0123456789ABCDEF",
				IAMRoleARN:   "arn:aws:iam::123456789012:role/external-dns",
			},
		},
	}
	spec.PolicyEnforcement = &configv1alpha1.PolicyEnforcement{
		ClusterPolicy:  configv1alpha1.ClusterPolicyConfig{Engine: configv1alpha1.ClusterPolicyEngineKyverno},
		WorkshopPolicy: configv1alpha1.WorkshopPolicyConfig{Engine: configv1alpha1.WorkshopPolicyEngineKyverno},
		Kyverno:        &configv1alpha1.KyvernoConfig{Provider: configv1alpha1.KyvernoProviderBundled},
	}
	obj := &configv1alpha1.EducatesClusterConfig{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		Spec:       spec,
	}

	for _, tc := range []struct {
		namespace, release string
		load               func() (*chart.Chart, error)
		values             map[string]any
	}{
		{certManagerNamespace, certManagerReleaseName, vendoredcharts.CertManager, renderCertManagerValues(obj)},
		{kyvernoNamespace, kyvernoReleaseName, vendoredcharts.Kyverno, renderKyvernoValues(obj)},
		{externalDNSNamespace, externalDNSReleaseName, vendoredcharts.ExternalDNS, renderExternalDNSValues(obj)},
	} {
		t.Run(tc.namespace, func(t *testing.T) {
			chrt, err := tc.load()
			if err != nil {
				t.Fatalf("load chart: %v", err)
			}
			expectPodsMeetRestricted(t, renderManifests(t, tc.namespace, tc.release, chrt, tc.values))
		})
	}
}

// The operator's own namespace is created by Helm or the CLI, which
// give it no Pod Security level, so the operator's pod must meet the
// restricted level for the install to start on a cluster that defaults
// to it.
func TestOperatorChartMeetsRestricted(t *testing.T) {
	loaded, err := loader.LoadDir(filepath.Join("..", "..", "..", "..", "charts", "educates-installer"))
	if err != nil {
		t.Fatalf("load operator chart: %v", err)
	}
	chrt, ok := loaded.(*chart.Chart)
	if !ok {
		t.Fatalf("load operator chart: unexpected chart type %T", loaded)
	}
	expectPodsMeetRestricted(t, renderManifests(t, "educates-installer", "educates-installer", chrt, map[string]any{}))
}

// renderManifests installs chrt into an in-memory release store and
// returns the manifests Helm rendered for it, hooks included.
func renderManifests(t *testing.T, namespace, release string, chrt *chart.Chart, values map[string]any) string {
	t.Helper()
	hc, err := helm.NewMemoryClient(namespace)
	if err != nil {
		t.Fatalf("memory helm client: %v", err)
	}
	if _, err := hc.EnsureRelease(context.Background(), release, chrt, values); err != nil {
		t.Fatalf("render %s: %v", release, err)
	}
	rel, err := hc.Status(release)
	if err != nil {
		t.Fatalf("status %s: %v", release, err)
	}
	manifests := make([]string, 0, 1+len(rel.Hooks))
	manifests = append(manifests, rel.Manifest)
	for _, hook := range rel.Hooks {
		manifests = append(manifests, hook.Manifest)
	}
	return strings.Join(manifests, "\n---\n")
}

// expectPodsMeetRestricted fails t for each pod template in manifests
// that the latest restricted Pod Security level refuses, and when
// manifests hold no pod template at all, so a render that lost its
// workloads can't pass.
func expectPodsMeetRestricted(t *testing.T, manifests string) {
	t.Helper()
	evaluator, err := psapolicy.NewEvaluator(psapolicy.DefaultChecks(), nil)
	if err != nil {
		t.Fatalf("pod security evaluator: %v", err)
	}
	restricted := psaapi.LevelVersion{Level: psaapi.LevelRestricted, Version: psaapi.LatestVersion()}

	pods := 0
	reader := utilyaml.NewYAMLReader(bufio.NewReader(strings.NewReader(manifests)))
	for {
		doc, err := reader.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("read manifest: %v", err)
		}
		if len(bytes.TrimSpace(doc)) == 0 {
			continue
		}
		name, template, err := podTemplate(doc)
		if err != nil {
			t.Fatalf("decode manifest: %v", err)
		}
		if template == nil {
			continue
		}
		pods++
		result := psapolicy.AggregateCheckResults(evaluator.EvaluatePod(restricted, &template.ObjectMeta, &template.Spec))
		if !result.Allowed {
			t.Errorf("%s is refused by the restricted level: %s", name, strings.Join(result.ForbiddenReasons, "; "))
		}
	}
	if pods == 0 {
		t.Fatal("rendered manifests hold no pod template")
	}
}

// podTemplate returns the kind/name and pod template of a workload
// manifest, or a nil template for a manifest that runs no pods.
func podTemplate(doc []byte) (string, *corev1.PodTemplateSpec, error) {
	var object metav1.PartialObjectMetadata
	if err := yaml.Unmarshal(doc, &object); err != nil {
		return "", nil, err
	}
	var (
		template corev1.PodTemplateSpec
		err      error
	)
	switch object.Kind {
	case "Pod":
		var o corev1.Pod
		err = yaml.Unmarshal(doc, &o)
		template = corev1.PodTemplateSpec{ObjectMeta: o.ObjectMeta, Spec: o.Spec}
	case "Deployment":
		var o appsv1.Deployment
		err = yaml.Unmarshal(doc, &o)
		template = o.Spec.Template
	case "DaemonSet":
		var o appsv1.DaemonSet
		err = yaml.Unmarshal(doc, &o)
		template = o.Spec.Template
	case "StatefulSet":
		var o appsv1.StatefulSet
		err = yaml.Unmarshal(doc, &o)
		template = o.Spec.Template
	case "Job":
		var o batchv1.Job
		err = yaml.Unmarshal(doc, &o)
		template = o.Spec.Template
	case "CronJob":
		var o batchv1.CronJob
		err = yaml.Unmarshal(doc, &o)
		template = o.Spec.JobTemplate.Spec.Template
	default:
		return "", nil, nil
	}
	if err != nil {
		return "", nil, fmt.Errorf("%s: %w", object.Kind, err)
	}
	return object.Kind + "/" + object.Name, &template, nil
}
