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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	platformv1alpha1 "github.com/educates/educates-training-platform/installer/operator/api/platform/v1alpha1"
)

// CRD validation only: what the API server accepts or rejects for each
// platform CRD, and the defaults it applies. No reconciler runs here.

func deleteIfExists(obj client.Object, name string) {
	GinkgoHelper()
	if err := k8sClient.Get(ctx, types.NamespacedName{Name: name}, obj); err == nil {
		Expect(k8sClient.Delete(ctx, obj)).To(Succeed())
	}
}

var _ = Describe("SecretsManager CRD validation", func() {
	AfterEach(func() {
		deleteIfExists(&platformv1alpha1.SecretsManager{}, "cluster")
	})

	It("accepts a resource named 'cluster'", func() {
		obj := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("rejects a resource with a name other than 'cluster' (singleton CEL)", func() {
		obj := &platformv1alpha1.SecretsManager{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster"},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("singleton"))
	})
})

var _ = Describe("LookupService CRD validation", func() {
	AfterEach(func() {
		deleteIfExists(&platformv1alpha1.LookupService{}, "cluster")
	})

	It("accepts a resource named 'cluster' with a valid spec", func() {
		obj := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{
					Prefix: "educates-api",
				},
			},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("rejects a resource with a name other than 'cluster' (singleton CEL)", func() {
		obj := &platformv1alpha1.LookupService{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster"},
			Spec: platformv1alpha1.LookupServiceSpec{
				Ingress: platformv1alpha1.LookupServiceIngress{
					Prefix: "educates-api",
				},
			},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("singleton"))
	})
})

var _ = Describe("SessionManager CRD validation", func() {
	AfterEach(func() {
		deleteIfExists(&platformv1alpha1.SessionManager{}, "cluster")
	})

	It("accepts a resource named 'cluster'", func() {
		obj := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
		}
		Expect(k8sClient.Create(ctx, obj)).To(Succeed())
	})

	It("rejects a resource with a name other than 'cluster' (singleton CEL)", func() {
		obj := &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: "not-cluster"},
		}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("singleton"))
	})

	withPrePuller := func(p *platformv1alpha1.ImagePrePuller) *platformv1alpha1.SessionManager {
		return &platformv1alpha1.SessionManager{
			ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
			Spec:       platformv1alpha1.SessionManagerSpec{ImagePrePuller: p},
		}
	}

	It("defaults the image pre-puller off, with the default images included", func() {
		Expect(k8sClient.Create(ctx, withPrePuller(&platformv1alpha1.ImagePrePuller{}))).To(Succeed())

		got := &platformv1alpha1.SessionManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Spec.ImagePrePuller.Enabled).To(BeFalse())
		Expect(got.Spec.ImagePrePuller.IncludeDefaultImages).To(HaveValue(BeTrue()))
		Expect(got.Spec.ImagePrePuller.Images).To(BeEmpty())
	})

	It("accepts image pre-puller entries giving a name or an image", func() {
		images := []platformv1alpha1.PrePulledImage{
			{Name: "jdk21-environment"},
			{Image: "registry.example.com/workshops/custom-base:1.0"},
		}
		Expect(k8sClient.Create(ctx, withPrePuller(&platformv1alpha1.ImagePrePuller{
			Enabled:              true,
			IncludeDefaultImages: new(false),
			Images:               images,
		}))).To(Succeed())

		got := &platformv1alpha1.SessionManager{}
		Expect(k8sClient.Get(ctx, types.NamespacedName{Name: "cluster"}, got)).To(Succeed())
		Expect(got.Spec.ImagePrePuller.Enabled).To(BeTrue())
		Expect(got.Spec.ImagePrePuller.IncludeDefaultImages).To(HaveValue(BeFalse()))
		Expect(got.Spec.ImagePrePuller.Images).To(Equal(images))
	})

	It("accepts a switched-off image pre-puller with nothing to pull", func() {
		Expect(k8sClient.Create(ctx, withPrePuller(&platformv1alpha1.ImagePrePuller{
			IncludeDefaultImages: new(false),
		}))).To(Succeed())
	})

	It("rejects an image pre-puller entry giving both a name and an image", func() {
		err := k8sClient.Create(ctx, withPrePuller(&platformv1alpha1.ImagePrePuller{
			Enabled: true,
			Images: []platformv1alpha1.PrePulledImage{
				{Name: "jdk21-environment", Image: "registry.example.com/jdk21:1.0"},
			},
		}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must set exactly one of name and image"))
	})

	It("rejects an image pre-puller entry giving neither a name nor an image", func() {
		err := k8sClient.Create(ctx, withPrePuller(&platformv1alpha1.ImagePrePuller{
			Enabled: true,
			Images:  []platformv1alpha1.PrePulledImage{{}},
		}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must set exactly one of name and image"))
	})

	It("rejects an image pre-puller entry giving an empty name", func() {
		// A typed client drops an empty name, so write the entry as YAML
		// applied with kubectl would.
		obj := &unstructured.Unstructured{Object: map[string]any{
			"apiVersion": platformv1alpha1.GroupVersion.String(),
			"kind":       "SessionManager",
			"metadata":   map[string]any{"name": "cluster"},
			"spec": map[string]any{
				"imagePrePuller": map[string]any{
					"enabled": true,
					"images":  []any{map[string]any{"name": ""}},
				},
			},
		}}
		err := k8sClient.Create(ctx, obj)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("spec.imagePrePuller.images[0].name"))
	})

	It("rejects a switched-on image pre-puller with nothing to pull", func() {
		err := k8sClient.Create(ctx, withPrePuller(&platformv1alpha1.ImagePrePuller{
			Enabled:              true,
			IncludeDefaultImages: new(false),
		}))
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("must list at least one entry in images"))
	})
})
