package rest

import (
	"context"
	"net/http"
	"net/url"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/lookup-service/internal/service"
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

		betaUser1 := sc.sessionName("<session:beta-user-1>")
		By("reacquiring user-1's session while it awaits activation")
		applyState(sc.session, "beta-allocating")
		sc.awaitSession("cluster-a", "portal-a", "portal-a-w02", betaUser1, phaseAllocating)
		sc.replay("session-reacquire-allocating")
		expectReacquired(portalA, "portal-a-w02", betaUser1, "user-1")

		By("reacquiring user-1's session once it is activated, and never through a tenant that does not reach it")
		applyState(sc.session, "beta-allocated")
		sc.awaitSession("cluster-a", "portal-a", "portal-a-w02", betaUser1, phaseAllocated)
		sc.replay("session-reacquire-allocated")
		expectReacquired(portalA, "portal-a-w02", betaUser1, "user-1")
		sc.replay("session-affinity-outside-tenant")
		Expect(portalA.receivedRequests()).To(HaveLen(3))
		By("requesting a session from the remote cluster's portal, over TLS, through its public URL")
		sc.replay("session-request-remote")
		Expect(portalB.receivedRequests()).To(HaveLen(1))

		By("reacquiring user-2's session from the remote cluster's portal")
		applyState(sc.session, "remote-allocating")
		alphaUser2 := sc.sessionName("<session:alpha-user-2>")
		sc.awaitSession("cluster-b", "portal-b", "portal-b-w01", alphaUser2, phaseAllocating)
		sc.replay("session-reacquire-remote")
		expectReacquired(portalB, "portal-b-w01", alphaUser2, "user-2")
		Expect(portalB.receivedRequests()).To(HaveLen(2))
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
		AfterEach(func() {
			portalA.awaitCreations()
			portalB.awaitCreations()
		})

		It("alternates between clusters of equal capacity", func() {
			sc.prepareSpreading(3, 3)
			portalA.delayCreations()
			portalB.delayCreations()

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
			portalA.delayCreations()
			portalB.delayCreations()
			placed := sc.requestSessions("lab-alpha", 4)
			for i := 0; i < len(placed); i += 2 {
				Expect(placed[i]).NotTo(Equal(placed[i+1]), "placed %v: sessions %d and %d", placed, i+3, i+4)
			}
		})
	})

	DescribeTable("fail over to the next candidate within seconds, logging why the portal gave no session",
		func(behavior portalBehavior, reason string) {
			sc.replay("auth-login")
			sc.awaitClusters()
			portalA.behave(behavior)

			// portal-a, with sessions left under its maximum, ranks before
			// portal-b, which has no maximum.
			started := time.Now()
			Expect(sc.requestSessions("lab-alpha", 1)).To(Equal([]string{"cluster-b"}))
			Expect(time.Since(started)).To(BeNumerically("<", 2*portalTimeout))
			Expect(sc.service.logs.Lines()).To(ContainElement(And(
				ContainSubstring("The portal did not give a session"), ContainSubstring(reason),
				ContainSubstring("cluster=cluster-a"), ContainSubstring("portal=portal-a"),
				ContainSubstring("environment=portal-a-w01"))))
		},
		Entry("when the portal refuses the request", portalRefuses,
			`the portal answered 503 Service Unavailable: {"error":"No session available"}`),
		Entry("when the portal answers an error", portalFails, "the portal answered 500 Internal Server Error"),
		Entry("when the portal does not answer in time", portalHangs, "the portal did not answer within 2s"),
		Entry("when the portal cannot be reached", portalUnreachable, "connection refused"),
	)

	It("answer that the workshop is not available once every candidate failed", func() {
		sc.replay("auth-login")
		sc.awaitClusters()
		portalA.behave(portalRefuses)
		portalB.behave(portalFails)
		status, body := sc.requestSession("tenant-all", "lab-alpha", "user-7")
		Expect(status).To(Equal(http.StatusServiceUnavailable))
		Expect(body).To(Equal("Workshop not available"))
		Expect(portalA.receivedRequests()).To(HaveLen(1))
		Expect(portalB.receivedRequests()).To(HaveLen(1))
	})

	It("try only the running environments of the workshop, never one with a capacity of 0", func() {
		sc.replay("auth-login")
		updateTraining(clusterA, workshopEnvironmentKind, "portal-a-w01", func(environment map[string]any) {
			Expect(unstructured.SetNestedField(environment, int64(0), "status", "educates", "capacity")).To(Succeed())
		})
		updateTraining(clusterA, workshopEnvironmentKind, "portal-a-w02", func(environment map[string]any) {
			unstructured.RemoveNestedField(environment, "metadata", "labels", "training.educates.dev/portal.workshop")
		})
		sc.restart()
		sc.awaitClusters()

		By("requesting lab-alpha, which portal-b's environment offers under the name its label gives")
		Expect(sc.requestSessions("lab-alpha", 1)).To(Equal([]string{"cluster-b"}))
		Expect(portalA.receivedRequests()).To(BeEmpty())

		By("requesting lab-beta, which portal-a's environment without the label offers under its workshop's name")
		Expect(sc.requestSessions("lab-beta", 1)).To(Equal([]string{"cluster-a"}))

		By("stopping portal-b's environment")
		updateTraining(clusterB, workshopEnvironmentKind, "portal-b-w01", func(environment map[string]any) {
			environment["status"].(map[string]any)["educates"].(map[string]any)["phase"] = phaseStopping
		})
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).Should(BeEmpty())
		status, body := sc.requestSession("tenant-all", "lab-alpha", "user-8")
		Expect(status).To(Equal(http.StatusServiceUnavailable))
		Expect(body).To(Equal("Workshop not available"))
		Expect(portalA.receivedRequests()).To(HaveLen(1))
		Expect(portalB.receivedRequests()).To(HaveLen(1))
	})

	It("pass the analytics webhook URL to the portal, and no workshop parameters as an empty list", func() {
		sc.replay("auth-login")
		sc.awaitClusters()
		status, body := apiRequest(sc.service.url, http.MethodPost, "/api/v1/workshops", sc.token("tenant-client"),
			`{"tenantName": "tenant-all", "workshopName": "lab-beta", "clientIndexUrl": "https://sites.example.test/index",`+
				` "clientUserId": "user-9", "analyticsWebhookUrl": "https://sites.example.test/analytics"}`)
		Expect(status).To(Equal(http.StatusOK), body)
		requests := portalA.receivedRequests()
		Expect(requests).To(HaveLen(1))
		Expect(requests[0].query.Get("analytics_url")).To(Equal("https://sites.example.test/analytics"))
		Expect(requests[0].body).To(MatchJSON(`{"parameters": []}`))
	})

	// The specs of the portal client's caches request lab-beta for one user
	// again and again, which the portal answers with the user's session.
	requestBeta := func() {
		GinkgoHelper()
		status, body := sc.requestSession("tenant-all", "lab-beta", "user-12")
		Expect(status).To(Equal(http.StatusOK), body)
	}

	It("obtain a portal's OAuth token once, and a new one after the portal refuses it", func() {
		sc.replay("auth-login")
		sc.awaitClusters()
		requestBeta()
		requestBeta()
		Expect(portalA.receivedTokenRequests()).To(HaveLen(1))

		portalA.revokeTokens()
		requestBeta()
		Expect(portalA.receivedTokenRequests()).To(HaveLen(2))
		// The request the portal refused for its token, and the same again.
		Expect(portalA.receivedRequests()).To(HaveLen(4))
	})

	It("read a portal's robot credentials once, and again once its status names another Secret", func() {
		sc.replay("auth-login")
		sc.awaitClusters()
		requestBeta()

		By("changing the robot credentials Secret, which the service has read already")
		setRobotCredentials(portalA, robotCredentialsSecret, portalA.rotated)
		DeferCleanup(setRobotCredentials, portalA, robotCredentialsSecret, portalA.credentials)
		portalA.revokeTokens()
		requestBeta()
		Expect(portalA.receivedTokenRequests()[1].clientID).To(Equal(portalA.credentials.clientID))

		By("naming another Secret in the portal's status, with the credentials the portal now accepts")
		portalA.accept(portalA.rotated)
		portalA.revokeTokens()
		updateTraining(clusterA, trainingPortalKind, "portal-a", func(portal map[string]any) {
			Expect(unstructured.SetNestedField(portal, rotatedCredentialsSecret,
				"status", "educates", "credentialSecrets", "robot")).To(Succeed())
		})
		Eventually(func() int {
			status, _ := sc.requestSession("tenant-all", "lab-beta", "user-12")
			return status
		}).WithTimeout(inventoryTimeout).Should(Equal(http.StatusOK))
		tokenRequests := portalA.receivedTokenRequests()
		Expect(tokenRequests[len(tokenRequests)-1].clientID).To(Equal(portalA.rotated.clientID))
	})

	It("trust a remote portal's certificate only through the ingress CA", func() {
		sc.adjust = append(sc.adjust, func(options *service.Options) {
			options.Portals.CAFile = filepath.Join(GinkgoT().TempDir(), "ca.crt")
		})
		sc.restart()
		sc.replay("auth-login")
		sc.awaitClusters()
		status, _ := sc.requestSession("tenant-names", "lab-alpha", "user-11")
		Expect(status).To(Equal(http.StatusServiceUnavailable))
		Expect(sc.service.logs.Lines()).To(ContainElement(And(
			ContainSubstring("The portal did not give a session"), ContainSubstring("certificate"),
			ContainSubstring("cluster=cluster-b"))))
		Expect(portalB.receivedTokenRequests()).To(BeEmpty())
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

// requestSession has tenant-client request a session of the workshop for
// the user through the tenant, and returns the status and body of the
// answer.
func (sc *scenario) requestSession(tenant, workshop, user string) (int, string) {
	GinkgoHelper()
	return apiRequest(sc.service.url, http.MethodPost, "/api/v1/workshops", sc.token("tenant-client"),
		`{"tenantName": "`+tenant+`", "workshopName": "`+workshop+`", "clientUserId": "`+user+`", `+
			`"clientIndexUrl": "https://sites.example.test/index"}`)
}

// requestSessions has tenant-client request count sessions of the workshop
// through tenant-all, one after another, each for a user of its own, and
// returns the cluster each was placed on.
func (sc *scenario) requestSessions(workshop string, count int) []string {
	GinkgoHelper()
	placed := make([]string, 0, count)
	for range count {
		status, body := sc.requestSession("tenant-all", workshop, "user-"+randomToken()[:8])
		Expect(status).To(Equal(http.StatusOK), body)
		var response struct {
			ClusterName string `json:"clusterName"`
		}
		Expect(decodeJSONString(body, &response)).To(Succeed())
		placed = append(placed, response.ClusterName)
	}
	return placed
}

// setRobotCredentials makes the portal's robot credentials Secret of that
// name hold the credentials.
func setRobotCredentials(p *fakePortal, name string, credentials robotCredentials) {
	GinkgoHelper()
	var secret corev1.Secret
	Expect(p.cluster.client.Get(context.Background(), client.ObjectKey{Namespace: p.namespace, Name: name}, &secret)).
		To(Succeed())
	secret.Data = credentials.data()
	Expect(p.cluster.client.Update(context.Background(), &secret)).To(Succeed())
}

// activate does what a session's user activating it makes the portal do:
// the session of that placeholder becomes Allocated.
func (sc *scenario) activate(cluster *testCluster, placeholder string) {
	GinkgoHelper()
	name, ok := sc.session.Value(placeholder)
	Expect(ok).To(BeTrue(), "no session is bound as %s", placeholder)
	updateTraining(cluster, workshopSessionKind, name, func(session map[string]any) {
		session["status"].(map[string]any)["educates"].(map[string]any)["phase"] = phaseAllocated
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
