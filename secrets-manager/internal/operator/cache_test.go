package operator_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/secrets-manager/internal/operator"
)

// lastAppliedAnnotation is where kubectl apply keeps the manifest it last
// applied, a Secret's data included.
const lastAppliedAnnotation = "kubectl.kubernetes.io/last-applied-configuration"

var _ = Describe("The cache", func() {
	It("holds no Secret data, the manifest kubectl apply keeps in an annotation included", func() {
		var composed cache.Cache
		runSecretsManager(func(options *operator.Options) {
			options.NewCache = func(config *rest.Config, opts cache.Options) (cache.Cache, error) {
				c, err := cache.New(config, opts)
				composed = c
				return c, err
			}
		})

		ctx := context.Background()
		secret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Namespace:    "default",
				GenerateName: "applied-",
				Annotations: map[string]string{
					lastAppliedAnnotation: `{"apiVersion":"v1","kind":"Secret","data":{"token":"aHVudGVyMg=="}}`,
					"example.com/kept":    "yes",
				},
			},
			Data: map[string][]byte{"token": []byte("hunter2")},
		}
		Expect(k8sClient.Create(ctx, secret)).To(Succeed())
		DeferCleanup(func() { Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, secret))).To(Succeed()) })

		cached := &metav1.PartialObjectMetadata{}
		cached.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("Secret"))
		Eventually(func() error {
			return composed.Get(ctx, client.ObjectKeyFromObject(secret), cached)
		}).WithTimeout(30 * time.Second).Should(Succeed())

		Expect(cached.Annotations).To(HaveKeyWithValue("example.com/kept", "yes"))
		Expect(cached.Annotations).NotTo(HaveKey(lastAppliedAnnotation))
		Expect(cached.ManagedFields).To(BeEmpty())
	})
})
