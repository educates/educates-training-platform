/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package config

import (
	"errors"
	"io"
	"regexp"
	"slices"
	"strings"
	"testing"

	"helm.sh/helm/v4/pkg/chart/common"
	commonutil "helm.sh/helm/v4/pkg/chart/common/util"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	chartutil "helm.sh/helm/v4/pkg/chart/v2/util"
	"helm.sh/helm/v4/pkg/engine"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	vendoredcharts "github.com/educates/educates-training-platform/installer/operator/vendored-charts"
)

const testImageRegistryPrefix = "mirror.example.com/educates"

// imageArg matches an image reference passed as a container argument,
// as cert-manager passes its ACME HTTP01 solver image to the controller.
var imageArg = regexp.MustCompile(`^--[a-z0-9-]*image=(\S+)$`)

// TestClusterServiceImagesHonourImageRegistryPrefix renders each
// vendored cluster-service chart with its reconciler's values, once
// without and once with spec.imageRegistry.prefix, and requires the
// prefix to move every image the chart can run, hooks and chart tests
// included, to its upstream path under the prefix. That is the layout
// the air-gapped guide tells users to mirror into, so a chart bump that
// leaves an image behind, or a values key the chart doesn't read,
// fails here instead of on a mirrored cluster.
func TestClusterServiceImagesHonourImageRegistryPrefix(t *testing.T) {
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

	for _, svc := range services {
		t.Run(svc.name, func(t *testing.T) {
			upstream := renderChartImages(t, svc.load, svc.release, svc.namespace,
				svc.values(imageRegistryTestConfig("")))
			if len(upstream) == 0 {
				t.Fatal("chart rendered no images")
			}
			mirrored := renderChartImages(t, svc.load, svc.release, svc.namespace,
				svc.values(imageRegistryTestConfig(testImageRegistryPrefix)))

			want := make([]string, 0, len(upstream))
			for _, ref := range upstream {
				want = append(want, testImageRegistryPrefix+"/"+imagePathWithoutRegistry(ref))
			}
			slices.Sort(want)
			want = slices.Compact(want)

			if !slices.Equal(mirrored, want) {
				t.Errorf("images with prefix %q:\n got: %v\nwant: %v", testImageRegistryPrefix, mirrored, want)
			}
		})
	}
}

// imageRegistryTestConfig is the smallest Managed-mode config every
// cluster-service values renderer accepts, with the given prefix.
func imageRegistryTestConfig(prefix string) *configv1alpha1.EducatesClusterConfig {
	obj := &configv1alpha1.EducatesClusterConfig{
		Spec: configv1alpha1.EducatesClusterConfigSpec{
			Mode: configv1alpha1.ClusterConfigModeManaged,
			Ingress: &configv1alpha1.Ingress{
				Domain:           "educates.example.com",
				IngressClassName: "contour",
			},
			DNS: &configv1alpha1.DNS{
				BundledExternalDNS: &configv1alpha1.BundledExternalDNSConfig{
					Provider: configv1alpha1.DNS01ProviderRoute53,
					Route53:  &configv1alpha1.ExternalDNSRoute53Config{HostedZoneID: "Z0000000000000"},
				},
			},
		},
	}
	if prefix != "" {
		obj.Spec.ImageRegistry = &configv1alpha1.ImageRegistry{Prefix: prefix}
	}
	return obj
}

// renderChartImages renders a freshly loaded chart client-side, as an
// install would, and returns the sorted, de-duplicated image references
// it holds.
func renderChartImages(t *testing.T, load func() (*chart.Chart, error), release, namespace string, values map[string]any) []string {
	t.Helper()

	var images []string
	for _, doc := range renderChartDocuments(t, load, release, namespace, values) {
		images = collectImages(doc, images)
	}
	slices.Sort(images)
	return slices.Compact(images)
}

// renderChartDocuments renders a freshly loaded chart client-side, as an
// install would, dropping the subcharts the values disable, and returns
// every manifest it renders, hooks and chart tests included, decoded.
func renderChartDocuments(t *testing.T, load func() (*chart.Chart, error), release, namespace string, values map[string]any) []any {
	t.Helper()

	chrt, err := load()
	if err != nil {
		t.Fatalf("load chart: %v", err)
	}
	if err := chartutil.ProcessDependencies(chrt, values); err != nil {
		t.Fatalf("process chart dependencies: %v", err)
	}
	renderValues, err := commonutil.ToRenderValues(chrt, values,
		common.ReleaseOptions{Name: release, Namespace: namespace, IsInstall: true}, nil)
	if err != nil {
		t.Fatalf("build render values: %v", err)
	}
	manifests, err := engine.Render(chrt, renderValues)
	if err != nil {
		t.Fatalf("render chart: %v", err)
	}

	var docs []any
	for name, manifest := range manifests {
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}
		decoder := utilyaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
		for {
			var doc any
			if err := decoder.Decode(&doc); err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				t.Fatalf("decode %s: %v", name, err)
			}
			if doc != nil {
				docs = append(docs, doc)
			}
		}
	}
	return docs
}

// collectImages appends the image references in a decoded manifest:
// string values of `image` keys, as containers and init containers
// carry them, and `--*image=` container arguments. CRD schemas have
// `image` keys too, but as objects, so they are skipped.
func collectImages(node any, images []string) []string {
	switch n := node.(type) {
	case map[string]any:
		for key, value := range n {
			if s, ok := value.(string); ok && key == "image" {
				images = append(images, s)
				continue
			}
			images = collectImages(value, images)
		}
	case []any:
		for _, item := range n {
			if s, ok := item.(string); ok {
				if m := imageArg.FindStringSubmatch(s); m != nil {
					images = append(images, m[1])
				}
				continue
			}
			images = collectImages(item, images)
		}
	}
	return images
}

// imagePathWithoutRegistry drops the registry host from an image
// reference: its first path component when that names a host, which
// it does when it holds a "." or ":" or is "localhost".
func imagePathWithoutRegistry(ref string) string {
	host, rest, found := strings.Cut(ref, "/")
	if found && (strings.ContainsAny(host, ".:") || host == "localhost") {
		return rest
	}
	return ref
}
