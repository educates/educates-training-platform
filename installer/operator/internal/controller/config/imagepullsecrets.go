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
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	corev1ac "k8s.io/client-go/applyconfigurations/core/v1"
	metav1ac "k8s.io/client-go/applyconfigurations/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"

	configv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/config/v1alpha1"
	"github.com/educates/educates-training-platform/installer/operator/internal/ownership"
	"github.com/educates/educates-training-platform/installer/operator/internal/platformnamespace"
)

// A pod can use an image pull Secret only from its own namespace, and
// the spec names the pull Secrets in the operator namespace. So the
// reconciler copies each of them into every namespace the operator
// installs a chart into before it converges that release: the platform
// namespace in both modes, and in Managed mode the namespace of each
// cluster service it installs. Copies keep their source's name, which
// is the name the charts are given, and are controlled by the
// EducatesClusterConfig. They are left in place by the finalizer drain,
// so a chart's pre-delete hooks can still pull, and are garbage
// collected with the EducatesClusterConfig.

// imagePullSecretCopyLabel marks a Secret as the reconciler's copy of an
// image pull Secret, so it can find and delete the copies of Secrets the
// spec stops naming.
const imagePullSecretCopyLabel = "config.educates.dev/image-pull-secret-copy"

// specImageRegistry returns the image registry the spec declares for
// its mode, nil when it declares none, and the spec path of the field
// that declares it.
func specImageRegistry(obj *configv1alpha1.EducatesClusterConfig) (*configv1alpha1.ImageRegistry, string) {
	switch obj.Spec.Mode {
	case configv1alpha1.ClusterConfigModeInline:
		if obj.Spec.Inline != nil {
			return obj.Spec.Inline.ImageRegistry, "spec.inline.imageRegistry"
		}
	case configv1alpha1.ClusterConfigModeManaged:
		return obj.Spec.ImageRegistry, "spec.imageRegistry"
	}
	return nil, ""
}

// specImagePullSecrets returns the image pull Secrets the spec names.
func specImagePullSecrets(obj *configv1alpha1.EducatesClusterConfig) []configv1alpha1.LocalObjectReference {
	if registry, _ := specImageRegistry(obj); registry != nil {
		return registry.PullSecrets
	}
	return nil
}

// validateImagePullSecrets checks that each image pull Secret the spec
// names exists in the operator namespace and has type
// kubernetes.io/dockerconfigjson, so that a Secret the copies cannot be
// made from, or that pods could not pull with, is reported on the field
// naming it rather than as image pull failures in every namespace. The
// legacy kubernetes.io/dockercfg type is refused too: secrets-manager
// injects only dockerconfigjson Secrets into a ServiceAccount's
// imagePullSecrets, so workshop pods could not pull with one.
func (r *EducatesClusterConfigReconciler) validateImagePullSecrets(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	registry, field := specImageRegistry(obj)
	if registry == nil {
		return nil
	}
	for i, ref := range registry.PullSecrets {
		refField := fmt.Sprintf("%s.pullSecrets[%d]", field, i)
		source := &corev1.Secret{}
		err := r.APIReader.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: ref.Name}, source)
		if apierrors.IsNotFound(err) {
			return &validationError{
				Field:  refField,
				Reason: fmt.Sprintf("image pull Secret %q not found in the operator namespace %q", ref.Name, r.OperatorNamespace),
			}
		}
		if err != nil {
			return fmt.Errorf("read image pull Secret %s/%s: %w", r.OperatorNamespace, ref.Name, err)
		}
		if source.Type != corev1.SecretTypeDockerConfigJson {
			return &validationError{
				Field: refField,
				Reason: fmt.Sprintf("image pull Secret %q has type %q; it must have type %q",
					ref.Name, source.Type, corev1.SecretTypeDockerConfigJson),
			}
		}
	}
	return nil
}

// ensurePlatformImagePullSecrets copies the image pull Secrets into the
// platform namespace, creating it first when the spec names any, so they
// are there before a platform reconciler installs a runtime chart, which
// it does only once the EducatesClusterConfig is Ready.
func (r *EducatesClusterConfigReconciler) ensurePlatformImagePullSecrets(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig) error {
	if len(specImagePullSecrets(obj)) > 0 {
		if err := platformnamespace.Ensure(ctx, r.Client); err != nil {
			return err
		}
	}
	return r.ensureImagePullSecretCopies(ctx, obj, platformnamespace.Name)
}

// ensureImagePullSecretCopies server-side applies a copy of each image
// pull Secret the spec names into namespace, with its source's type and
// data, so the copies follow their sources on every reconcile, and then
// deletes the copies it made there of Secrets the spec no longer names.
// The operator namespace holds the sources and gets no copies: one
// applied over its source would make the EducatesClusterConfig the
// source's controller, and the source would be garbage collected with
// it.
func (r *EducatesClusterConfigReconciler) ensureImagePullSecretCopies(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig, namespace string) error {
	if namespace == r.OperatorNamespace {
		return nil
	}
	gvk, err := apiutil.GVKForObject(obj, r.Scheme)
	if err != nil {
		return fmt.Errorf("resolve EducatesClusterConfig GVK: %w", err)
	}
	keep := map[string]bool{}
	for _, ref := range specImagePullSecrets(obj) {
		keep[ref.Name] = true
		// The operator namespace is in the Secret cache, whose watch
		// reconciles on every change to a source, and
		// validateImagePullSecrets has already read each one fresh.
		source := &corev1.Secret{}
		if err := r.Get(ctx, types.NamespacedName{Namespace: r.OperatorNamespace, Name: ref.Name}, source); err != nil {
			return fmt.Errorf("read image pull Secret %s/%s: %w", r.OperatorNamespace, ref.Name, err)
		}
		copied := corev1ac.Secret(ref.Name, namespace).
			WithLabels(imagePullSecretCopyLabels()).
			WithOwnerReferences(metav1ac.OwnerReference().
				WithAPIVersion(gvk.GroupVersion().String()).
				WithKind(gvk.Kind).
				WithName(obj.GetName()).
				WithUID(obj.GetUID()).
				WithController(true).
				WithBlockOwnerDeletion(true)).
			WithType(source.Type).
			WithData(source.Data)
		if err := r.Apply(ctx, copied, client.FieldOwner(ownership.FieldManager), client.ForceOwnership); err != nil {
			return fmt.Errorf("apply image pull Secret %s/%s: %w", namespace, ref.Name, err)
		}
	}
	return r.deleteRetiredImagePullSecretCopies(ctx, obj, namespace, keep)
}

// imagePullSecretCopyLabels are the labels on every image pull Secret
// copy.
func imagePullSecretCopyLabels() map[string]string {
	return map[string]string{
		ownership.ManagedByLabel: ownership.ManagedByValue,
		imagePullSecretCopyLabel: "true",
	}
}

// deleteRetiredImagePullSecretCopies deletes the copies the
// EducatesClusterConfig controls in namespace whose names are not in
// keep. The copies' namespaces are outside the operator's Secret
// cache, so it lists their metadata through the uncached reader.
func (r *EducatesClusterConfigReconciler) deleteRetiredImagePullSecretCopies(ctx context.Context, obj *configv1alpha1.EducatesClusterConfig, namespace string, keep map[string]bool) error {
	copies := &metav1.PartialObjectMetadataList{}
	copies.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("SecretList"))
	if err := r.APIReader.List(ctx, copies, client.InNamespace(namespace), client.MatchingLabels(imagePullSecretCopyLabels())); err != nil {
		return fmt.Errorf("list image pull Secret copies in %s: %w", namespace, err)
	}
	for i := range copies.Items {
		item := &copies.Items[i]
		if keep[item.Name] || !metav1.IsControlledBy(item, obj) {
			continue
		}
		uid := item.UID
		retired := &corev1.Secret{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: item.Name}}
		if err := r.Delete(ctx, retired, client.Preconditions{UID: &uid}); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete image pull Secret copy %s/%s: %w", namespace, item.Name, err)
		}
	}
	return nil
}

// setGlobalImagePullSecrets names the spec's image pull Secrets in a
// cluster-service chart's global.imagePullSecrets, which each vendored
// chart applies to every pod it renders, hooks and chart tests
// included. The operator copies the Secrets into the chart's namespace
// before it converges the release. With no pull Secrets it leaves values
// as they are.
func setGlobalImagePullSecrets(values map[string]any, obj *configv1alpha1.EducatesClusterConfig) {
	if obj == nil {
		return
	}
	pullSecrets := specImagePullSecrets(obj)
	if len(pullSecrets) == 0 {
		return
	}
	refs := make([]any, 0, len(pullSecrets))
	for _, ref := range pullSecrets {
		refs = append(refs, map[string]any{"name": ref.Name})
	}
	global, _ := values["global"].(map[string]any)
	if global == nil {
		global = map[string]any{}
		values["global"] = global
	}
	global["imagePullSecrets"] = refs
}
