package rest

import (
	"context"
	"net/http"
	"net/url"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	It("answer the scenario's session requests from step to step, on the local and the remote cluster", func() {
		sc.replay("auth-login", "auth-login-user-client")
		sc.awaitClusters()
		sc.replay("session-request")

		applyState(sc.session, "beta-allocating")
		sc.replay("session-reacquire-allocating")

		applyState(sc.session, "beta-allocated")
		sc.replay("session-reacquire-allocated", "session-affinity-outside-tenant")
		By("requesting a session from the remote cluster's portal, over TLS, through its public URL")
		sc.replay("session-request-remote")
		Expect(portalB.receivedRequests()).To(HaveLen(1))

		applyState(sc.session, "remote-allocating")
		sc.replay("session-reacquire-remote")
		By("requesting a session as a client whose spec.user overrides the request's user")
		sc.replay("session-request-spec-user")
		Expect(portalA.receivedRequests()).To(HaveLen(4))
		Expect(portalA.receivedRequests()[3].query.Get("user")).To(Equal("fixed-user"))

		sc.activate(clusterA, "<session:alpha-reserved>")
		By("requesting a session without a user, for whom the portal makes one")
		sc.replay("session-request-no-user")
		Expect(portalA.receivedRequests()[4].query.Get("user")).To(BeEmpty())
		sc.bindPortalUser(clusterA, "beta-anonymous")

		applyState(sc.session, "portal-a-busy")
		sc.replay("session-environment-full")

		applyState(sc.session, "remote-expired")
		sc.replay("session-after-expiry")

		Expect(k8sClient.Delete(context.Background(), scenarioClusterConfig("cluster-b"))).To(Succeed())
		applyState(sc.session, "cluster-b-deleted")
		sc.eventuallyReplay("deleted-session-tenant-names")
	})

	It("refuse a request that lacks a field, or that the client may not make, before contacting any portal", func() {
		sc.replay("auth-login", "login-alias")
		sc.awaitClusters()
		sc.replay(
			"session-missing-tenant",
			"session-missing-workshop",
			"session-missing-index-url",
			"session-malformed-json",
			"session-array-body",
			"session-no-token",
			"session-tenant-not-allowed",
			"session-admin-without-tenants",
			"session-tenant-unknown",
			"session-tenant-without-portals",
			"session-workshop-unknown",
			"session-workshop-not-on-tenant",
		)
		Expect(portalA.receivedTokenRequests()).To(BeEmpty())
		Expect(portalA.receivedRequests()).To(BeEmpty())
		Expect(portalB.receivedRequests()).To(BeEmpty())
	})

	Describe("spreading sessions across two clusters offering the same workshop (#975)", func() {
		// The portals create each session's WorkshopSession some time after
		// answering, so that only the requests in flight tell the service
		// where the sessions it asked for went.
		const creationDelay = 3 * time.Second
		AfterEach(func() {
			portalA.awaitCreations()
			portalB.awaitCreations()
		})

		It("alternates between clusters of equal capacity", func() {
			sc.prepareSpreading(3, 3)
			portalA.delayCreation(creationDelay)
			portalB.delayCreation(creationDelay)

			placed := sc.requestSessions("lab-alpha", 6)
			for i := 0; i < len(placed); i += 2 {
				Expect(placed[i]).NotTo(Equal(placed[i+1]), "placed %v: sessions %d and %d", placed, i+1, i+2)
			}
			Expect(placed).To(HaveEach(BeElementOf("cluster-a", "cluster-b")))
		})

		It("fills the larger cluster until both have as many sessions left, and then alternates", func() {
			sc.prepareSpreading(4, 2)

			By("placing two sessions, which the larger cluster takes, and letting their sessions show")
			Expect(sc.requestSessions("lab-alpha", 2)).To(Equal([]string{"cluster-a", "cluster-a"}))
			portalA.awaitCreations()
			// The watch delivers the sessions well within this; until it does,
			// the requests count as in flight, which places the same.
			time.Sleep(time.Second)

			By("placing the rest while their sessions do not show yet")
			portalA.delayCreation(creationDelay)
			portalB.delayCreation(creationDelay)
			placed := sc.requestSessions("lab-alpha", 4)
			for i := 0; i < len(placed); i += 2 {
				Expect(placed[i]).NotTo(Equal(placed[i+1]), "placed %v: sessions %d and %d", placed, i+3, i+4)
			}
		})
	})

	It("answer within seconds when the only portal never answers", func() {
		sc.replay("auth-login")
		sc.awaitClusters()
		portalA.behave(portalHangsOnToken)
		started := time.Now()
		sc.replay("D01-session-portal-timeout")
		Expect(time.Since(started)).To(BeNumerically("<", 2*portalTimeout))
		Expect(sc.service.logs.Lines()).To(ContainElement(And(
			ContainSubstring("The portal did not give a session"),
			ContainSubstring("the portal did not answer within 2s"),
			ContainSubstring("cluster=cluster-a"), ContainSubstring("portal=portal-a"),
			ContainSubstring("environment=portal-a-w02"))))
	})
})

// prepareSpreading makes lab-alpha's environments on both clusters' portals
// the only candidates of a session request, with room for ten sessions and
// no reserved session, on portals whose session maximums are maximumA and
// maximumB. The service is started again, so that it starts from them, and
// tenant-client logged in.
func (sc *scenario) prepareSpreading(maximumA, maximumB int64) {
	GinkgoHelper()
	reserved, ok := sc.session.Value("<session:alpha-reserved>")
	Expect(ok).To(BeTrue())
	clusterA.deleteTraining(workshopSessionKind, reserved)
	for _, c := range []struct {
		cluster             *testCluster
		portal, environment string
		maximum             int64
	}{
		{clusterA, "portal-a", "portal-a-w01", maximumA},
		{clusterB, "portal-b", "portal-b-w01", maximumB},
	} {
		updateTraining(c.cluster, trainingPortalKind, c.portal, func(portal map[string]any) {
			Expect(unstructured.SetNestedField(portal, c.maximum, "spec", "portal", "sessions", "maximum")).
				To(Succeed())
		})
		updateTraining(c.cluster, workshopEnvironmentKind, c.environment, func(environment map[string]any) {
			Expect(unstructured.SetNestedField(environment, int64(10), "status", "educates", "capacity")).
				To(Succeed())
			Expect(unstructured.SetNestedField(environment, int64(0), "status", "educates", "reserved")).
				To(Succeed())
		})
	}
	sc.restart()
	sc.awaitClusters()
	sc.replay("auth-login")
}

// requestSessions has tenant-client request count sessions of the workshop
// through tenant-all, one after another, each for a user of its own, and
// returns the cluster each was placed on.
func (sc *scenario) requestSessions(workshop string, count int) []string {
	GinkgoHelper()
	placed := make([]string, 0, count)
	for range count {
		user := "user-" + randomToken()[:8]
		status, body := apiRequest(sc.service.url, http.MethodPost, "/api/v1/workshops", sc.token("tenant-client"),
			`{"tenantName": "tenant-all", "workshopName": "`+workshop+`", "clientUserId": "`+user+`", `+
				`"clientIndexUrl": "https://sites.example.test/index"}`)
		Expect(status).To(Equal(http.StatusOK), body)
		var response struct {
			ClusterName string `json:"clusterName"`
		}
		Expect(decodeJSONString(body, &response)).To(Succeed())
		placed = append(placed, response.ClusterName)
	}
	return placed
}

// activate does what a session's user activating it makes the portal do:
// the session of that placeholder becomes Allocated.
func (sc *scenario) activate(cluster *testCluster, placeholder string) {
	GinkgoHelper()
	name, ok := sc.session.Value(placeholder)
	Expect(ok).To(BeTrue(), "no session is bound as %s", placeholder)
	updateTraining(cluster, workshopSessionKind, name, func(session map[string]any) {
		session["status"].(map[string]any)["educates"].(map[string]any)["phase"] = "Allocated"
	})
}

// bindPortalUser binds the portal user of the session labelled label to the
// user the fake portal made for it.
func (sc *scenario) bindPortalUser(cluster *testCluster, label string) {
	GinkgoHelper()
	name, ok := sc.session.Value("<session:" + label + ">")
	Expect(ok).To(BeTrue(), "no session is labelled %s", label)
	session := &unstructured.Unstructured{}
	session.SetGroupVersionKind(workshopSessionKind)
	Expect(cluster.client.Get(context.Background(), client.ObjectKey{Name: name}, session)).To(Succeed())
	user, _, _ := unstructured.NestedString(session.Object, "status", "educates", "user")
	Expect(user).NotTo(BeEmpty())
	sc.session.Bind("<portal-user:"+label+">", user)
}

// queryOf returns a query string's parameters, each with one value.
func queryOf(values map[string]string) url.Values {
	query := url.Values{}
	for name, value := range values {
		query.Set(name, value)
	}
	return query
}
