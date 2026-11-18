package rest

import (
	"context"
	"net/http"
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
	"github.com/educates/educates-training-platform/lookup-service/internal/service"
)

var _ = Describe("The config watcher", func() {
	Context("in the scenario", func() {
		sc := setUpScenario()

		It("writes nothing to the ClientConfigs it watches, adding no finalizers", func() {
			before := clientConfigVersions()

			sc.replay("auth-login", "auth-login-rotate-client", "auth-login-revoke-client", "auth-logout")
			updateClientConfig("rotate-client", func(config *lookupv1beta1.ClientConfig) {
				config.Spec.Client.Password = rotatedPassword
			})
			sc.eventuallyReplay("auth-verify-after-password-change")
			sc.restart()

			after := clientConfigVersions()
			Expect(after["rotate-client"]).NotTo(Equal(before["rotate-client"]))
			delete(before, "rotate-client")
			delete(after, "rotate-client")
			Expect(after).To(Equal(before))

			var configs lookupv1beta1.ClientConfigList
			Expect(k8sClient.List(context.Background(), &configs, client.InNamespace(configNamespace))).To(Succeed())
			for _, config := range configs.Items {
				Expect(config.Finalizers).To(BeEmpty(), "ClientConfig %s has finalizers", config.Name)
			}

			deleteClientConfig("doomed-client")
			err := k8sClient.Get(context.Background(),
				client.ObjectKey{Namespace: configNamespace, Name: "doomed-client"}, &lookupv1beta1.ClientConfig{})
			Expect(apierrors.IsNotFound(err)).To(BeTrue(), "a deleted ClientConfig is held back")
		})
	})

	Context("choosing the config namespace", func() {
		const otherNamespace = "lookup-config"

		BeforeEach(func() {
			err := k8sClient.Create(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{Name: otherNamespace},
			})
			if !apierrors.IsAlreadyExists(err) {
				Expect(err).NotTo(HaveOccurred())
			}

			session := fixtures.NewSession("")
			createClientConfigs(session)
			elsewhere := scenarioClientConfig("tenant-client")
			elsewhere.Name = "elsewhere-client"
			elsewhere.Namespace = otherNamespace
			Expect(k8sClient.Create(context.Background(), elsewhere)).To(Succeed())
		})
		AfterEach(removeConfiguration)

		It("reads educates-config unless the environment names another namespace", func() {
			svc := startLookupService()
			Expect(loginStatus(svc, "tenant-client")).To(Equal(http.StatusOK))
			Expect(loginStatus(svc, "elsewhere-client")).To(Equal(http.StatusUnauthorized))
			svc.stop()

			Expect(os.Setenv(service.ConfigNamespaceEnv, otherNamespace)).To(Succeed())
			DeferCleanup(os.Unsetenv, service.ConfigNamespaceEnv)

			svc = startLookupService()
			defer svc.stop()
			Expect(loginStatus(svc, "elsewhere-client")).To(Equal(http.StatusOK))
			Expect(loginStatus(svc, "tenant-client")).To(Equal(http.StatusUnauthorized))
		})
	})
})

// clientConfigVersions returns the resource version of each ClientConfig in
// the config namespace.
func clientConfigVersions() map[string]string {
	GinkgoHelper()
	var configs lookupv1beta1.ClientConfigList
	Expect(k8sClient.List(context.Background(), &configs, client.InNamespace(configNamespace))).To(Succeed())
	versions := map[string]string{}
	for _, config := range configs.Items {
		versions[config.Name] = config.ResourceVersion
	}
	return versions
}

// loginStatus returns the status of a login to svc with tenant-client's
// password.
func loginStatus(svc *lookupService, name string) int {
	GinkgoHelper()
	status, _ := apiRequest(svc.url, http.MethodPost, "/auth/login", "",
		`{"username": "`+name+`", "password": "golden-tenant-password"}`)
	return status
}
