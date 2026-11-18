package rest

import (
	. "github.com/onsi/ginkgo/v2"
)

var _ = Describe("The admin view of tenants", func() {
	sc := setUpScenario()

	BeforeEach(func() {
		sc.replay("auth-login", "login-alias")
	})

	It("lists the tenants, each with the clients whose patterns match it, for an admin only", func() {
		sc.replay(
			"tenants-list",
			"tenants-list-tenant-role",
			"tenants-get",
			"tenants-get-unknown",
		)
	})

	It("lists the portals a tenant reaches, on the local cluster and on a remote one, for an admin only", func() {
		sc.awaitClusters()
		sc.replay(
			"tenants-portals-tenant-all",
			"tenants-portals-tenant-names",
			"tenants-portals-tenant-labels",
			"tenants-portals-tenant-none",
			"tenants-portals-unknown",
			"tenants-portals-tenant-role",
		)
	})
})
