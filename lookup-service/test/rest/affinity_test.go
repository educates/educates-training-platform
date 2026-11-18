package rest

import (
	"net/http"
	"strconv"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// indexURL is the clientIndexUrl the suite's session requests give.
const indexURL = "https://sites.example.test/index"

var _ = Describe("Session affinity", func() {
	sc := setUpScenario()
	BeforeEach(func() {
		resetFakePortals()
		sc.replay("auth-login")
		sc.awaitClusters()
	})

	It("gives a returning user their session back from a portal that is full", func() {
		first := sc.placeSession("tenant-all", "lab-beta", "user-13")
		sc.awaitSession("cluster-a", "portal-a", "portal-a-w02", first.SessionName, phaseAllocating)

		By("lowering portal-a's session maximum to the one session it runs")
		updateTraining(clusterA, trainingPortalKind, "portal-a", func(portal map[string]any) {
			Expect(unstructured.SetNestedField(portal, int64(1), "spec", "portal", "sessions", "maximum")).
				To(Succeed())
		})
		sc.awaitPortalCapacity("cluster-a", "portal-a", 1)
		status, body := sc.requestSession("tenant-all", "lab-beta", "user-14")
		Expect(status).To(Equal(http.StatusServiceUnavailable), body)

		By("requesting the workshop for the returning user")
		again := sc.placeSession("tenant-all", "lab-beta", "user-13")
		Expect(again).To(Equal(first))
		expectReacquired(portalA, "portal-a-w02", first.SessionName, "user-13")
	})

	It("matches a returning user only against the sessions on the portals the tenant reaches", func() {
		By("giving user-15 a session on portal-b through tenant-names")
		remote := sc.placeSession("tenant-names", "lab-alpha", "user-15")
		Expect(remote.ClusterName).To(Equal("cluster-b"))
		sc.awaitSession("cluster-b", "portal-b", "portal-b-w01", remote.SessionName, phaseAllocating)

		By("requesting the workshop for the same user id through tenant-labels, which reaches portal-a only")
		local := sc.placeSession("tenant-labels", "lab-alpha", "user-15")
		Expect(local.ClusterName).To(Equal("cluster-a"))
		Expect(portalB.receivedRequests()).To(HaveLen(1))
		Expect(reacquires(portalA)).To(BeEmpty())
		sc.awaitSession("cluster-a", "portal-a", "portal-a-w01", local.SessionName, phaseAllocating)

		By("requesting it again through each tenant, which gives each its own session back")
		Expect(sc.placeSession("tenant-labels", "lab-alpha", "user-15")).To(Equal(local))
		expectReacquired(portalA, "portal-a-w01", local.SessionName, "user-15")
		Expect(sc.placeSession("tenant-names", "lab-alpha", "user-15")).To(Equal(remote))
		expectReacquired(portalB, "portal-b-w01", remote.SessionName, "user-15")
	})

	It("never gives a returning user a session that is stopping", func() {
		first := sc.placeSession("tenant-all", "lab-beta", "user-16")
		sc.awaitSession("cluster-a", "portal-a", "portal-a-w02", first.SessionName, phaseAllocating)
		updateTraining(clusterA, workshopSessionKind, first.SessionName, func(session map[string]any) {
			session["status"].(map[string]any)["educates"].(map[string]any)["phase"] = phaseStopping
		})
		sc.awaitSession("cluster-a", "portal-a", "portal-a-w02", first.SessionName, phaseStopping)

		second := sc.placeSession("tenant-all", "lab-beta", "user-16")
		Expect(second.SessionName).NotTo(Equal(first.SessionName))
		Expect(reacquires(portalA)).To(BeEmpty())
	})

	It("tries the user's next session when a portal does not give one back", func() {
		local := sc.placeSession("tenant-labels", "lab-alpha", "user-17")
		remote := sc.placeSession("tenant-names", "lab-alpha", "user-17")
		sc.awaitSession("cluster-a", "portal-a", "portal-a-w01", local.SessionName, phaseAllocating)
		sc.awaitSession("cluster-b", "portal-b", "portal-b-w01", remote.SessionName, phaseAllocating)

		// Whichever session is tried first, the one whose portal fails is
		// tried first in one of the two requests.
		By("failing portal-a")
		portalA.behave(portalFails)
		Expect(sc.placeSession("tenant-all", "lab-alpha", "user-17")).To(Equal(remote))
		expectReacquired(portalB, "portal-b-w01", remote.SessionName, "user-17")

		By("failing portal-b")
		portalA.behave(portalAnswers)
		portalB.behave(portalFails)
		Expect(sc.placeSession("tenant-all", "lab-alpha", "user-17")).To(Equal(local))
		expectReacquired(portalA, "portal-a-w01", local.SessionName, "user-17")

		Expect(sc.service.logs.Lines()).To(ContainElement(And(
			ContainSubstring("The portal did not give the user's session back"),
			ContainSubstring("the portal answered 500 Internal Server Error"),
			ContainSubstring("workshop=lab-alpha"))))
	})

	It("requests a new session when no portal gives the user's session back", func() {
		remote := sc.placeSession("tenant-names", "lab-alpha", "user-18")
		sc.awaitSession("cluster-b", "portal-b", "portal-b-w01", remote.SessionName, phaseAllocating)
		portalB.behave(portalFails)

		// portal-a, with sessions left under its maximum, ranks before
		// portal-b, which has no maximum; were it not, portal-b would fail.
		placed := sc.placeSession("tenant-all", "lab-alpha", "user-18")
		Expect(placed.ClusterName).To(Equal("cluster-a"))
		Expect(reacquires(portalB)).To(HaveLen(1))
		Expect(reacquires(portalA)).To(BeEmpty())
		Expect(sc.service.logs.Lines()).To(ContainElement(And(
			ContainSubstring("The portal did not give the user's session back"),
			ContainSubstring("cluster=cluster-b"), ContainSubstring("portal=portal-b"),
			ContainSubstring("environment=portal-b-w01"), ContainSubstring("session="+remote.SessionName))))
	})
})

// placedSession is where a session request placed a session.
type placedSession struct {
	ClusterName     string `json:"clusterName"`
	PortalName      string `json:"portalName"`
	EnvironmentName string `json:"environmentName"`
	SessionName     string `json:"sessionName"`
}

// placeSession has tenant-client request a session of the workshop for the
// user through the tenant, and returns where the session is.
func (sc *scenario) placeSession(tenant, workshop, user string) placedSession {
	GinkgoHelper()
	status, body := sc.requestSession(tenant, workshop, user)
	Expect(status).To(Equal(http.StatusOK), body)
	var placed placedSession
	Expect(decodeJSONString(body, &placed)).To(Succeed())
	return placed
}

// awaitPortalCapacity waits until the service shows the portal on the
// cluster with that session maximum.
func (sc *scenario) awaitPortalCapacity(cluster, portal string, capacity int64) {
	GinkgoHelper()
	Eventually(func() string {
		status, body := apiRequest(sc.service.url, http.MethodGet, "/api/v1/clusters/"+cluster+"/portals/"+portal,
			sc.adminToken(), "")
		if status != http.StatusOK {
			return body
		}
		var details struct {
			Capacity int64 `json:"capacity"`
		}
		if err := decodeJSONString(body, &details); err != nil {
			return err.Error()
		}
		return strconv.FormatInt(details.Capacity, 10)
	}).WithTimeout(inventoryTimeout).Should(Equal(strconv.FormatInt(capacity, 10)))
}

// sessionName returns the session name bound to the placeholder.
func (sc *scenario) sessionName(placeholder string) string {
	GinkgoHelper()
	name, ok := sc.session.Value(placeholder)
	Expect(ok).To(BeTrue(), "no session is bound as %s", placeholder)
	return name
}

// awaitSession waits until the service shows the session of that name, of
// the environment of the portal on the cluster, in the phase, so that a
// request that follows finds it.
func (sc *scenario) awaitSession(cluster, portal, environment, name, phase string) {
	GinkgoHelper()
	path := "/api/v1/clusters/" + cluster + "/portals/" + portal + "/environments/" + environment + "/sessions"
	Eventually(func() string {
		status, body := apiRequest(sc.service.url, http.MethodGet, path, sc.adminToken(), "")
		if status != http.StatusOK {
			return body
		}
		var list struct {
			Sessions []struct {
				Name  string  `json:"name"`
				Phase *string `json:"phase"`
			} `json:"sessions"`
		}
		if err := decodeJSONString(body, &list); err != nil {
			return err.Error()
		}
		for _, session := range list.Sessions {
			if session.Name == name && session.Phase != nil {
				return *session.Phase
			}
		}
		return "no session " + name
	}).WithTimeout(inventoryTimeout).Should(Equal(phase))
}

// reacquires returns the session requests the portal received that named
// the session they reacquire.
func reacquires(p *fakePortal) []sessionRequest {
	var named []sessionRequest
	for _, request := range p.receivedRequests() {
		if request.query.Has("session") {
			named = append(named, request)
		}
	}
	return named
}

// expectReacquired asserts that the last session request the portal
// received reacquired the user's session of that name from the environment:
// with the index URL, the user and the session in the query, and no body.
func expectReacquired(p *fakePortal, environment, session, user string) {
	GinkgoHelper()
	requests := p.receivedRequests()
	Expect(requests).NotTo(BeEmpty())
	request := requests[len(requests)-1]
	Expect(request.method).To(Equal(http.MethodPost))
	Expect(request.environment).To(Equal(environment))
	Expect(request.query).To(Equal(queryOf(map[string]string{
		"index_url": indexURL,
		"user":      user,
		"session":   session,
	})))
	Expect(request.body).To(BeEmpty())
}
