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
	"fmt"
	"slices"
	"testing"

	chart "helm.sh/helm/v4/pkg/chart/v2"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

// testPullSecretNames are the image pull Secrets the chart render tests
// name in spec.imageRegistry.pullSecrets.
var testPullSecretNames = []string{"mirror-pull-secret", "mirror-pull-secret-2"}

// TestClusterServicePodsCarryImagePullSecrets renders each vendored
// cluster-service chart with its reconciler's values for a spec naming
// pull Secrets, and requires every pod the chart can run, hooks and
// chart tests included, to be able to use them: named in the pod's own
// imagePullSecrets or in those of the ServiceAccount the chart renders
// for it. The operator copies the Secrets into each of these
// namespaces, so a pod that names neither fails to pull from a mirror
// that requires authentication, and a chart bump that drops the global
// value for one of its pods fails here instead of on a mirrored cluster.
func TestClusterServicePodsCarryImagePullSecrets(t *testing.T) {
	services := []struct {
		name      string
		load      func() (*chart.Chart, error)
		release   string
		namespace string
		values    func(*configv1alpha1.EducatesClusterConfig) map[string]any
	}{
		{"cert-manager", vendoredcharts.CertManager, certManagerReleaseName, certManagerNamespace, renderCertManagerValues},
		{"contour", vendoredcharts.Contour, contourReleaseName, contourNamespace, renderContourValues},
		{"external-dns", vendoredcharts.ExternalDNS, externalDNSReleaseName, externalDNSNamespace, renderExternalDNSValues},
		{"kyverno", vendoredcharts.Kyverno, kyvernoReleaseName, kyvernoNamespace, renderKyvernoValues},
	}

	obj := imageRegistryTestConfig(testImageRegistryPrefix)
	for _, name := range testPullSecretNames {
		obj.Spec.ImageRegistry.PullSecrets = append(obj.Spec.ImageRegistry.PullSecrets,
			configv1alpha1.LocalObjectReference{Name: name})
	}

	for _, svc := range services {
		t.Run(svc.name, func(t *testing.T) {
			docs := renderChartDocuments(t, svc.load, svc.release, svc.namespace, svc.values(obj))

			serviceAccountSecrets := map[string][]string{}
			for _, doc := range docs {
				m, ok := doc.(map[string]any)
				if !ok || m["kind"] != "ServiceAccount" {
					continue
				}
				name := nestedString(m, "metadata", "name")
				serviceAccountSecrets[name] = pullSecretNames(m["imagePullSecrets"])
			}

			pods := renderedPodSpecs(docs)
			if len(pods) == 0 {
				t.Fatal("chart rendered no pods")
			}
			for _, pod := range pods {
				serviceAccount := nestedString(pod.spec, "serviceAccountName")
				if serviceAccount == "" {
					serviceAccount = "default"
				}
				usable := append(pullSecretNames(pod.spec["imagePullSecrets"]), serviceAccountSecrets[serviceAccount]...)
				for _, want := range testPullSecretNames {
					if !slices.Contains(usable, want) {
						t.Errorf("%s cannot use pull Secret %q: its pod names %v and its ServiceAccount %q names %v",
							pod.owner, want, pullSecretNames(pod.spec["imagePullSecrets"]), serviceAccount, serviceAccountSecrets[serviceAccount])
					}
				}
			}
		})
	}
}

// renderedPod is a pod spec a chart renders, with the workload it
// belongs to for failure messages.
type renderedPod struct {
	owner string
	spec  map[string]any
}

// renderedPodSpecs returns the pod specs of the workloads and bare pods
// in the rendered documents.
func renderedPodSpecs(docs []any) []renderedPod {
	var pods []renderedPod
	for _, doc := range docs {
		m, ok := doc.(map[string]any)
		if !ok {
			continue
		}
		kind, _ := m["kind"].(string)
		var path []string
		switch kind {
		case "Deployment", "DaemonSet", "StatefulSet", "ReplicaSet", "Job":
			path = []string{"spec", "template", "spec"}
		case "CronJob":
			path = []string{"spec", "jobTemplate", "spec", "template", "spec"}
		case "Pod":
			path = []string{"spec"}
		default:
			continue
		}
		spec, ok := nestedMap(m, path...)
		if !ok {
			continue
		}
		pods = append(pods, renderedPod{
			owner: fmt.Sprintf("%s %s", kind, nestedString(m, "metadata", "name")),
			spec:  spec,
		})
	}
	return pods
}

// pullSecretNames returns the names in an imagePullSecrets list.
func pullSecretNames(list any) []string {
	items, _ := list.([]any)
	var names []string
	for _, item := range items {
		if m, ok := item.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

func nestedMap(m map[string]any, path ...string) (map[string]any, bool) {
	current := m
	for _, key := range path {
		next, ok := current[key].(map[string]any)
		if !ok {
			return nil, false
		}
		current = next
	}
	return current, true
}

func nestedString(m map[string]any, path ...string) string {
	parent, ok := nestedMap(m, path[:len(path)-1]...)
	if !ok {
		return ""
	}
	s, _ := parent[path[len(path)-1]].(string)
	return s
}
