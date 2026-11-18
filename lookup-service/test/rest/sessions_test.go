package rest

import (
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Session requests", func() {
	sc := setUpScenario()
	BeforeEach(resetFakePortals)

	It("give a session from a portal on the local cluster, reached through its in-cluster service", func() {
		sc.replay("auth-login")
		sc.awaitClusters()
		sc.replay("session-request")

		Expect(portalA.receivedTokenRequests()).To(ConsistOf(tokenRequest{
			grantType: "password", username: "robot@educates", password: "portal-a-robot-password",
			clientID: "portal-a-client-id", clientSecret: "portal-a-client-secret",
		}))
		requests := portalA.receivedRequests()
		Expect(requests).To(HaveLen(1))
		request := requests[0]
		Expect(request.method).To(Equal("POST"))
		Expect(request.environment).To(Equal("portal-a-w02"))
		Expect(request.authorization).To(HavePrefix("Bearer "))
		Expect(request.query).To(Equal(queryOf(map[string]string{
			"user":          "user-1",
			"email":         "user-1@example.test",
			"first_name":    "User",
			"last_name":     "One",
			"index_url":     "https://sites.example.test/index",
			"analytics_url": "",
		})))
		Expect(request.body).To(MatchJSON(`{"parameters": [{"name": "greeting", "value": "hello"}]}`))
		Expect(portalB.receivedRequests()).To(BeEmpty())
	})
})

// queryOf returns a query string's parameters, each with one value.
func queryOf(values map[string]string) url.Values {
	query := url.Values{}
	for name, value := range values {
		query.Set(name, value)
	}
	return query
}
