package rest

import (
	"context"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
	"github.com/educates/educates-training-platform/lookup-service/internal/tokens"
)

var _ = Describe("Tokens across restarts", func() {
	sc := setUpScenario()

	It("keeps tokens valid across a restart of the service against the same Secret", func() {
		sc.replay("auth-login", "login-alias")
		sc.restart()
		// The logins after the restart issue new tokens, which differ from
		// the first ones only once their expiry, in seconds, does.
		time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
		sc.replay(
			"restarted-verify-old-token",
			"restarted-login",
			"restarted-verify-new-token",
			"restarted-login-admin",
		)
	})

	It("keeps a logout's revocation across a restart", func() {
		sc.replay("auth-login-revoke-client", "auth-logout")
		sc.restart()
		sc.replay("auth-verify-revoked")
	})

	It("revokes a client's tokens when its password changed while the service was stopped", func() {
		sc.replay("auth-login-rotate-client")
		sc.service.stop()
		sc.service = nil

		updateClientConfig("rotate-client", func(config *lookupv1beta1.ClientConfig) {
			config.Spec.Client.Password = rotatedPassword
		})
		sc.start()
		sc.replay("auth-verify-after-password-change")
	})

	It("creates its token Secret in its own namespace on the first start, with a new random signing key", func() {
		firstKey := serviceSigningKey()
		Expect(len(firstKey)).To(BeNumerically(">=", 32))
		err := k8sClient.Get(context.Background(),
			client.ObjectKey{Namespace: configNamespace, Name: tokens.SecretName}, &corev1.Secret{})
		Expect(apierrors.IsNotFound(err)).To(BeTrue(), "the token Secret is in the config namespace")

		sc.replay("auth-login")
		sc.service.stop()
		sc.service = nil
		Expect(k8sClient.Delete(context.Background(), &corev1.Secret{
			ObjectMeta: objectMeta(serviceNamespace, tokens.SecretName),
		})).To(Succeed())

		sc.start()
		Expect(serviceSigningKey()).NotTo(Equal(firstKey))
		status, body := apiRequest(sc.service.url, http.MethodGet, "/auth/verify", sc.token("tenant-client"), "")
		Expect(status).To(Equal(http.StatusUnauthorized))
		Expect(body).To(Equal("JWT token is invalid"))
	})
})
