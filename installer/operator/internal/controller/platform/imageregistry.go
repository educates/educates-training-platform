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
	"strings"

	chart "helm.sh/helm/v4/pkg/chart/v2"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
)

// imageRegistryNamespaceAnnotation is the Chart.yaml annotation holding
// the registry namespace a runtime chart composes its Educates images
// under by default. The release workflow stamps it for each fork, and
// hack/lint-chart-versions.sh keeps it the same across the charts.
const imageRegistryNamespaceAnnotation = "educates.dev/image-registry-namespace"

// chartImageNamespace returns the registry namespace chrt publishes its
// Educates images under.
func chartImageNamespace(chrt *chart.Chart) string {
	if chrt == nil || chrt.Metadata == nil {
		return ""
	}
	return chrt.Metadata.Annotations[imageRegistryNamespaceAnnotation]
}

// developmentImageRegistryValues returns the development value that
// moves a runtime chart's Educates images under the image registry
// prefix the cluster config publishes, or nil when it publishes none.
//
// Each image keeps its upstream repository path under the prefix, as the
// cluster-service images do, so one name-preserving mirror serves both:
// under registry.internal/mirror, ghcr.io/educates/educates-secrets-manager
// is registry.internal/mirror/educates/educates-secrets-manager. The
// charts compose {host}/{namespace}/educates-<name>, so the host is the
// prefix up to its first slash, and the namespace is the rest of the
// prefix followed by imageNamespace, the chart's own namespace. The
// session-manager chart also hands the pair to workshop sessions as
// $(image_repository).
func developmentImageRegistryValues(cfg *configv1alpha1.EducatesClusterConfig, imageNamespace string) map[string]any {
	if cfg.Status.ImageRegistry == nil || cfg.Status.ImageRegistry.Prefix == "" {
		return nil
	}
	host, path, _ := strings.Cut(cfg.Status.ImageRegistry.Prefix, "/")
	var segments []string
	for _, s := range []string{path, imageNamespace} {
		if s != "" {
			segments = append(segments, s)
		}
	}
	return map[string]any{
		"imageRegistry": map[string]any{
			"host":      host,
			"namespace": strings.Join(segments, "/"),
		},
	}
}
