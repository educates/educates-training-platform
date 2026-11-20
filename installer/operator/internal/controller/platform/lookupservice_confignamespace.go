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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	// kopfFinalizer is the finalizer the kopf-based Lookup Service of
	// earlier releases put on every configuration. The Lookup Service adds
	// no finalizer, but a configuration created under a kopf-based one can
	// still carry it, since the configuration namespace outlives the
	// release.
	kopfFinalizer = "kopf.zalando.org/KopfFinalizerMarker"
)

// lookupServiceConfigurationKinds are the kinds of lookup-service's
// configurations, which it reads from its configuration namespace.
var lookupServiceConfigurationKinds = []schema.GroupVersionKind{
	{Group: "lookup.educates.dev", Version: "v1beta1", Kind: "ClusterConfig"},
	{Group: "lookup.educates.dev", Version: "v1beta1", Kind: "ClientConfig"},
	{Group: "lookup.educates.dev", Version: "v1beta1", Kind: "TenantConfig"},
}

// releaseLookupServiceConfigurations removes kopf's finalizer from every
// lookup-service configuration in the configuration namespace, leaving any
// other finalizer. The namespace and its configurations outlive the
// lookup-service release, and nothing else removes kopf's finalizer from a
// configuration that still carries it, so deleting that configuration, or
// the namespace, would wait forever. Call it once the release is
// uninstalled.
func releaseLookupServiceConfigurations(ctx context.Context, c client.Client) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		for _, gvk := range lookupServiceConfigurationKinds {
			list := &unstructured.UnstructuredList{}
			list.SetGroupVersionKind(gvk.GroupVersion().WithKind(gvk.Kind + "List"))
			if err := c.List(ctx, list, client.InNamespace(lookupServiceConfigNamespace)); err != nil {
				// The kind is not served: its CRD is gone, so are its
				// resources. A list in a missing namespace is empty
				// rather than an error, so NotFound means the same as a
				// NoMatch, from a mapping cached before the CRD went.
				if meta.IsNoMatchError(err) || apierrors.IsNotFound(err) {
					continue
				}
				return fmt.Errorf("list %s resources in namespace %q: %w", gvk.Kind, lookupServiceConfigNamespace, err)
			}
			for i := range list.Items {
				obj := &list.Items[i]
				if !controllerutil.ContainsFinalizer(obj, kopfFinalizer) {
					continue
				}
				patch := client.MergeFromWithOptions(obj.DeepCopy(), client.MergeFromWithOptimisticLock{})
				controllerutil.RemoveFinalizer(obj, kopfFinalizer)
				if err := c.Patch(ctx, obj, patch); err != nil {
					if apierrors.IsNotFound(err) {
						continue
					}
					return fmt.Errorf("remove finalizer %s from %s %s/%s: %w",
						kopfFinalizer, gvk.Kind, obj.GetNamespace(), obj.GetName(), err)
				}
			}
		}
		return nil
	})
}

// adoptLookupServiceConfigNamespace lets the lookup-service release take
// over an educates-config Namespace created by hand, with the
// configurations in it. Helm refuses to install a resource that already
// exists unless it carries the release's ownership metadata, so such a
// Namespace is given the managed-by label and the release name and
// namespace annotations. A Namespace that names a Helm release already,
// this one or another, is left as it is, so Helm still refuses to take one
// over from another release. A missing Namespace is left for the chart to
// create.
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
	logf.FromContext(ctx).Info(fmt.Sprintf("Namespace %s, created by hand, taken over by the %s release",
		lookupServiceConfigNamespace, lookupServiceReleaseName))
	return nil
}
