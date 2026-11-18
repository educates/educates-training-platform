package rest

import (
	"context"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
	"github.com/educates/educates-training-platform/lookup-service/test/golden"
)

// rotatedPassword is the password the scenario gives rotate-client.
const rotatedPassword = "golden-rotate-password-2"

var _ = Describe("Token revocation", func() {
	sc := setUpScenario()

	It("revokes every token of a client when it logs out", func() {
		sc.replay("auth-login-revoke-client")
		first := sc.token("revoke-client")
		// Tokens issued in the same second are the same token.
		time.Sleep(1100 * time.Millisecond)
		second := sc.login("revoke-client", "golden-revoke-password")
		Expect(second).NotTo(Equal(first))

		sc.replay(
			"auth-logout",
			"auth-verify-revoked",
			"auth-logout-revoked",
			"auth-logout-no-token",
			"auth-logout-not-a-jwt",
			"clients-get-revoked",
		)

		status, body := apiRequest(sc.service.url, http.MethodGet, "/auth/verify", second, "")
		Expect(status).To(Equal(http.StatusUnauthorized))
		Expect(body).To(Equal("Client identity does not match"))
	})

	It("keeps revoked tokens revoked when an update leaves the password unchanged", func() {
		sc.replay("login-alias", "auth-login-revoke-client", "auth-logout")

		updateClientConfig("revoke-client", func(config *lookupv1beta1.ClientConfig) {
			config.Spec.Tenants = []string{"tenant-all", "tenant-names"}
		})
		admin := sc.token("admin-client")
		Eventually(func() string {
			_, body := apiRequest(sc.service.url, http.MethodGet, "/api/v1/clients/revoke-client", admin, "")
			return body
		}).Should(ContainSubstring("tenant-names"), "the service has not seen the update")

		sc.replay("auth-verify-revoked-after-update")
	})

	It("revokes a client's tokens when its password changes", func() {
		sc.replay("auth-login-rotate-client")

		updateClientConfig("rotate-client", func(config *lookupv1beta1.ClientConfig) {
			config.Spec.Client.Password = rotatedPassword
		})
		sc.eventuallyReplay("auth-verify-after-password-change")
		sc.replay("auth-login-old-password")

		// The password change raised the client's revocation counter, which
		// the token issued after it carries. The capture's counter is 1, as
		// the Python service never raised it on a password change.
		Expect(sc.session.Replay("auth-login-new-password", golden.WithIssuedClaim("jti",
			"client@educates:"+uidPlaceholder("ClientConfig", "rotate-client")+"#2"))).To(Succeed())

		status, _ := apiRequest(sc.service.url, http.MethodGet, "/auth/verify", sc.token("rotate-client#2"), "")
		Expect(status).To(Equal(http.StatusOK))
	})

	It("invalidates a deleted client's tokens, and its predecessor's once it is created again", func() {
		sc.replay("auth-login-doomed-client")

		deleteClientConfig("doomed-client")
		sc.eventuallyReplay("auth-verify-deleted-client")
		sc.replay("auth-logout-deleted-client", "auth-login-deleted-client")

		createClientConfig(sc.session, scenarioClientConfig("doomed-client"), "#2")
		sc.eventuallyReplay("auth-verify-recreated-client")
		sc.replay("auth-login-recreated-client", "auth-verify-recreated-client-new-token")
	})
})

// token returns the token the session's logins issued under that name, as
// tenant-client or rotate-client#2.
func (sc *scenario) token(name string) string {
	GinkgoHelper()
	token, ok := sc.session.Value("<token:" + name + ">")
	Expect(ok).To(BeTrue(), "no token is bound as %s", name)
	return token
}

// login logs a client in and returns its token.
func (sc *scenario) login(name, password string) string {
	GinkgoHelper()
	status, body := apiRequest(sc.service.url, http.MethodPost, "/auth/login", "",
		`{"username": "`+name+`", "password": "`+password+`"}`)
	Expect(status).To(Equal(http.StatusOK), body)
	var response struct {
		AccessToken string `json:"access_token"`
	}
	Expect(decodeJSONString(body, &response)).To(Succeed())
	return response.AccessToken
}

// updateClientConfig changes the ClientConfig of that name in the config
// namespace.
func updateClientConfig(name string, change func(*lookupv1beta1.ClientConfig)) {
	GinkgoHelper()
	var config lookupv1beta1.ClientConfig
	Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: configNamespace, Name: name}, &config)).
		To(Succeed())
	change(&config)
	Expect(k8sClient.Update(context.Background(), &config)).To(Succeed())
}

// deleteClientConfig deletes the ClientConfig of that name in the config
// namespace.
func deleteClientConfig(name string) {
	GinkgoHelper()
	Expect(k8sClient.Delete(context.Background(), &lookupv1beta1.ClientConfig{
		ObjectMeta: objectMeta(configNamespace, name),
	})).To(Succeed())
}
