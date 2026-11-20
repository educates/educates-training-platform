package rest

import (
	"context"
	"time"

	"github.com/golang-jwt/jwt/v5"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/lookup-service/internal/tokens"
)

var _ = Describe("Client authentication", func() {
	sc := setUpScenario()

	It("logs clients in through /auth/login and its /login alias with HS256 tokens valid for 72 hours", func() {
		sc.replay(
			"auth-login",
			"login-alias",
			"auth-login-both-client",
			"auth-login-user-client",
			"auth-login-revoke-client",
			"auth-login-rotate-client",
			"auth-login-doomed-client",
		)
	})

	It("refuses a login without a name or password, or with a wrong one", func() {
		sc.replay(
			"auth-login-missing-username",
			"auth-login-missing-password",
			"auth-login-wrong-password",
			"auth-login-unknown-client",
			"login-alias-wrong-password",
		)
	})

	It("answers 400 to a login whose body is not a JSON object", func() {
		sc.replay(
			"auth-login-malformed-json",
			"auth-login-empty-body",
			"auth-login-array-body",
		)
	})

	It("verifies a valid token", func() {
		sc.replay("auth-login", "login-alias", "auth-verify", "auth-verify-admin")
	})

	It("answers 400 without a token or with a malformed Authorization header", func() {
		sc.replay(
			"auth-verify-no-token",
			"auth-verify-bearer-without-token",
			"auth-verify-basic-scheme",
			"auth-verify-three-words",
		)
	})

	It("answers 401 to a token that is not a JWT, has expired or is signed with another key", func() {
		identity := "client@educates:" + sc.uid("ClientConfig", "tenant-client") + "#1"
		sc.session.Bind("<token:expired>", signToken(serviceSigningKey(), "tenant-client", identity, -time.Hour))
		sc.session.Bind("<token:foreign>", signToken([]byte("not-the-service-key"), "tenant-client", identity, time.Hour))

		sc.replay("auth-verify-not-a-jwt", "auth-verify-expired", "auth-verify-foreign-key")
	})

	It("answers a path no route serves with 404, and a route asked with another method with 405", func() {
		sc.replay("login-alias", "unknown-route", "auth-login-wrong-method")
	})
})

// uid returns the uid of the lookup cluster's object the session bound.
func (sc *scenario) uid(kind, name string) string {
	GinkgoHelper()
	uid, ok := sc.session.Value(uidPlaceholder(kind, name))
	Expect(ok).To(BeTrue(), "the uid of %s %s is not bound", kind, name)
	return uid
}

// serviceSigningKey returns the key the service signs tokens with, from its
// token Secret.
func serviceSigningKey() []byte {
	GinkgoHelper()
	var secret corev1.Secret
	Expect(k8sClient.Get(context.Background(),
		client.ObjectKey{Namespace: serviceNamespace, Name: tokens.SecretName}, &secret)).To(Succeed())
	Expect(secret.Data).To(HaveKey(tokens.SigningKeyKey))
	return secret.Data[tokens.SigningKeyKey]
}

// signToken signs an HS256 token as the service does, expiring after
// expiresIn, which is negative for a token that has expired.
func signToken(key []byte, subject, identity string, expiresIn time.Duration) string {
	GinkgoHelper()
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.RegisteredClaims{
		Subject:   subject,
		ID:        identity,
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(expiresIn)),
	}).SignedString(key)
	Expect(err).NotTo(HaveOccurred())
	return token
}
