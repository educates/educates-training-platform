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
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/educates/educates-training-platform/installer/operator/internal/ownership"
)

const (
	// lookupServiceConfigNamespace is the namespace lookup-service reads
	// its configuration from. The lookup-service chart creates it, and Helm
	// keeps it when the release is uninstalled.
	lookupServiceConfigNamespace = "educates-config"

	// The ownership metadata Helm requires on a resource that already
	// exists before a release may install it.
	helmManagedByValue             = "Helm"
	helmReleaseNameAnnotation      = "meta.helm.sh/release-name"
	helmReleaseNamespaceAnnotation = "meta.helm.sh/release-namespace"
)

// adoptLookupServiceConfigNamespace lets the lookup-service release take
// over an educates-config Namespace created by hand, as operators were once
// told to create it, with the configurations in it. Helm refuses to install
// a resource that already exists unless it carries the release's ownership
// metadata, so such a Namespace is given the managed-by label and the
// release name and namespace annotations. A Namespace that names a Helm
// release already, this one or another, is left as it is, so Helm still
// refuses to take one over from another release. A missing Namespace is
// left for the chart to create.
func adoptLookupServiceConfigNamespace(ctx context.Context, c client.Client) error {
	ns := &corev1.Namespace{}
	if err := c.Get(ctx, types.NamespacedName{Name: lookupServiceConfigNamespace}, ns); err != nil {
		if client.IgnoreNotFound(err) == nil {
			return nil
		}
		return fmt.Errorf("get Namespace %q: %w", lookupServiceConfigNamespace, err)
	}

	annotations := ns.GetAnnotations()
	if annotations[helmReleaseNameAnnotation] != "" || annotations[helmReleaseNamespaceAnnotation] != "" {
		return nil
	}

	patch := client.MergeFrom(ns.DeepCopy())
	ownership.RestoreLabels(ns, map[string]string{ownership.ManagedByLabel: helmManagedByValue})
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[helmReleaseNameAnnotation] = lookupServiceReleaseName
	annotations[helmReleaseNamespaceAnnotation] = platformNamespace
	ns.SetAnnotations(annotations)
	if err := c.Patch(ctx, ns, patch); err != nil {
		return fmt.Errorf("give Namespace %q the %s release's ownership metadata: %w",
			lookupServiceConfigNamespace, lookupServiceReleaseName, err)
	}
	logf.FromContext(ctx).Info("Namespace created by hand taken over by the lookup-service release",
		"namespace", lookupServiceConfigNamespace)
	return nil
}
