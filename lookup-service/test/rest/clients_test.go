package rest

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("Client details", func() {
	sc := setUpScenario()

	BeforeEach(func() {
		sc.replay("auth-login", "login-alias", "auth-login-both-client")
	})

	It("lists every client for an admin, and refuses a client without the admin role", func() {
		sc.replay(
			"clients-list",
			"clients-list-tenant-role",
			"clients-list-both-roles",
			"clients-list-no-token",
		)
	})

	It("gives a client its own details, a tenant no one else's and an admin anyone's", func() {
		sc.replay(
			"clients-get-self",
			"clients-get-other-tenant-role",
			"clients-get-other-both-roles",
			"clients-get-user-client",
			"clients-get-unknown",
		)
	})
})
