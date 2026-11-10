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
	"slices"
	"strings"
	"testing"

	chart "helm.sh/helm/v4/pkg/chart/v2"
	"sigs.k8s.io/yaml"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

const testRuntimeImageRegistryPrefix = "mirror.example.com/educates"

// testImageRegistryNamespace is the image registry namespace the embedded
// runtime charts compose their Educates images under, which the
// reconcilers read from the chart they install. The render tests below
// read it from each chart instead.
const testImageRegistryNamespace = "educates"

// prefixedClusterConfig is the test cluster config publishing the given
// image registry prefix in status, as the cluster config reconciler does
// from spec.imageRegistry or spec.inline.imageRegistry.
func prefixedClusterConfig(prefix string) *configv1alpha1.EducatesClusterConfig {
	cfg := valuesTestClusterConfig()
	if prefix != "" {
		cfg.Status.ImageRegistry = &configv1alpha1.ImageRegistry{Prefix: prefix}
	}
	return cfg
}

// TestRuntimeImagesHonourImageRegistryPrefix renders each runtime chart
// with its reconciler's values, once without and once with an image
// registry prefix in the cluster config's status, and requires every
// Educates image the chart can run to move to its upstream repository
// path under the prefix, the layout the cluster-service images follow and
// the air-gapped guide tells users to mirror into. That includes the
// session manager's image inventory, from which training portals and
// workshop sessions are created. Upstream images the inventory pins, such
// as docker-in-docker and the vcluster images, are not relocated by the
// prefix and must not move.
func TestRuntimeImagesHonourImageRegistryPrefix(t *testing.T) {
	sessionManager := &platformv1alpha1.SessionManager{
		Spec: platformv1alpha1.SessionManagerSpec{
			ImagePrePuller: &platformv1alpha1.ImagePrePuller{Enabled: true},
		},
	}
	charts := []struct {
		name   string
		load   func() (*chart.Chart, error)
		values func(cfg *configv1alpha1.EducatesClusterConfig, imageRegistryNamespace string) map[string]any
	}{
		{"secrets-manager", vendoredcharts.SecretsManager, func(cfg *configv1alpha1.EducatesClusterConfig, imageRegistryNamespace string) map[string]any {
			return renderSecretsManagerValues(&platformv1alpha1.SecretsManager{}, cfg, imageRegistryNamespace)
		}},
		{"lookup-service", vendoredcharts.LookupService, func(cfg *configv1alpha1.EducatesClusterConfig, imageRegistryNamespace string) map[string]any {
			return renderLookupServiceValues(&platformv1alpha1.LookupService{
				Spec: platformv1alpha1.LookupServiceSpec{
					Ingress: platformv1alpha1.LookupServiceIngress{Prefix: "lookup"},
				},
			}, cfg, imageRegistryNamespace)
		}},
		{"session-manager", vendoredcharts.SessionManager, func(cfg *configv1alpha1.EducatesClusterConfig, imageRegistryNamespace string) map[string]any {
			return renderSessionManagerValues(sessionManager, cfg, testOperatorNamespace, imageRegistryNamespace)
		}},
		{"node-ca-injector", vendoredcharts.NodeCAInjector, func(cfg *configv1alpha1.EducatesClusterConfig, imageRegistryNamespace string) map[string]any {
			return renderNodeCAInjectorValues(sessionManager, cfg, testOperatorNamespace, imageRegistryNamespace)
		}},
	}

	for _, c := range charts {
		t.Run(c.name, func(t *testing.T) {
			host, namespace := chartImageLocation(t, c.load)
			educates := host + "/" + namespace + "/"

			upstream := renderRuntimeImages(t, c.load, c.values(prefixedClusterConfig(""), namespace))
			if !slices.ContainsFunc(upstream, func(ref string) bool { return strings.HasPrefix(ref, educates) }) {
				t.Fatalf("chart rendered no image under %s: %v", educates, upstream)
			}
			mirrored := renderRuntimeImages(t, c.load,
				c.values(prefixedClusterConfig(testRuntimeImageRegistryPrefix), namespace))

			want := make([]string, 0, len(upstream))
			for _, ref := range upstream {
				if strings.HasPrefix(ref, educates) {
					ref = testRuntimeImageRegistryPrefix + "/" + strings.TrimPrefix(ref, host+"/")
				}
				want = append(want, ref)
			}
			slices.Sort(want)
			want = slices.Compact(want)

			if !slices.Equal(mirrored, want) {
				t.Errorf("images with prefix %q:\n got: %v\nwant: %v", testRuntimeImageRegistryPrefix, mirrored, want)
			}
		})
	}
}

// TestSessionManagerChart_ImageRepositoryIsTheEducatesNamespaceUnderThePrefix
// requires the image repository workshop content reaches as
// $(image_repository) to be where the prefix puts the Educates images,
// and to stay unset without a prefix, so sessions keep their in-cluster
// default.
func TestSessionManagerChart_ImageRepositoryIsTheEducatesNamespaceUnderThePrefix(t *testing.T) {
	_, namespace := chartImageLocation(t, vendoredcharts.SessionManager)
	obj := &platformv1alpha1.SessionManager{}

	for _, tc := range []struct {
		prefix string
		want   string
	}{
		{"", ""},
		{testRuntimeImageRegistryPrefix, testRuntimeImageRegistryPrefix + "/" + namespace},
		{"mirror.example.com", "mirror.example.com/" + namespace},
	} {
		values := renderSessionManagerValues(obj, prefixedClusterConfig(tc.prefix), testOperatorNamespace, namespace)
		registry := renderedOperatorConfig(t, values)["imageRegistry"].(map[string]any)
		got := registry["host"].(string)
		if ns := registry["namespace"].(string); ns != "" {
			got += "/" + ns
		}
		if got != tc.want {
			t.Errorf("prefix %q: image repository %q, want %q", tc.prefix, got, tc.want)
		}
	}
}

// chartImageLocation returns the registry host and namespace a runtime
// chart composes its Educates images from by default, its
// educates.dev/image-registry-host and -namespace annotations.
func chartImageLocation(t *testing.T, load func() (*chart.Chart, error)) (host, namespace string) {
	t.Helper()

	chrt, err := load()
	if err != nil {
		t.Fatalf("load chart: %v", err)
	}
	host = chrt.Metadata.Annotations["educates.dev/image-registry-host"]
	namespace = chrt.Metadata.Annotations[imageRegistryNamespaceAnnotation]
	if host == "" || namespace == "" {
		t.Fatalf("chart %s lacks its image registry annotations: %v", chrt.Metadata.Name, chrt.Metadata.Annotations)
	}
	return host, namespace
}

// renderRuntimeImages renders the chart with vals and returns the sorted,
// de-duplicated image references it holds: those of its workloads, and
// those of the session manager's image inventory in its configuration
// Secret.
func renderRuntimeImages(t *testing.T, load func() (*chart.Chart, error), vals map[string]any) []string {
	t.Helper()

	var images []string
	for doc := range renderManifests(t, load, vals) {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		images = collectImages(obj, images)
		if config, ok := operatorConfigOf(t, obj); ok {
			images = collectImages(config, images)
		}
	}
	slices.Sort(images)
	return slices.Compact(images)
}

// renderedOperatorConfig renders the session-manager chart with vals and
// returns the session manager's configuration.
func renderedOperatorConfig(t *testing.T, vals map[string]any) map[string]any {
	t.Helper()

	for doc := range renderManifests(t, vendoredcharts.SessionManager, vals) {
		var obj map[string]any
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			t.Fatalf("parse rendered manifest: %v", err)
		}
		if config, ok := operatorConfigOf(t, obj); ok {
			return config
		}
	}
	t.Fatal("no educates-config Secret in the rendered session-manager chart")
	return nil
}

// operatorConfigOf returns the session manager's configuration when obj
// is the educates-config Secret that carries it.
func operatorConfigOf(t *testing.T, obj map[string]any) (map[string]any, bool) {
	t.Helper()

	if obj["kind"] != "Secret" {
		return nil, false
	}
	if meta, _ := obj["metadata"].(map[string]any); meta["name"] != "educates-config" {
		return nil, false
	}
	data, _ := obj["stringData"].(map[string]any)
	blob, ok := data["educates-operator-config.yaml"].(string)
	if !ok {
		return nil, false
	}
	var config map[string]any
	if err := yaml.Unmarshal([]byte(blob), &config); err != nil {
		t.Fatalf("parse educates-operator-config.yaml: %v", err)
	}
	return config, true
}

// collectImages appends every string held under an "image" key in v.
func collectImages(v any, images []string) []string {
	switch node := v.(type) {
	case map[string]any:
		for key, child := range node {
			if s, ok := child.(string); ok && key == "image" {
				images = append(images, s)
				continue
			}
			images = collectImages(child, images)
		}
	case []any:
		for _, child := range node {
			images = collectImages(child, images)
		}
	}
	return images
}
