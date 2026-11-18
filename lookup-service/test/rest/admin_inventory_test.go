package rest

import (
	"context"
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
)

var _ = Describe("The admin view of the clusters", func() {
	sc := setUpScenario()

	BeforeEach(func() {
		sc.replay("auth-login", "login-alias")
		sc.awaitClusters()
	})

	It("lists every portal of every cluster, for an admin only", func() {
		sc.replay("portals-list", "portals-list-tenant-role")
	})

	It("lists the clusters and gives a cluster's details, for an admin only", func() {
		sc.replay(
			"clusters-list",
			"clusters-list-tenant-role",
			"clusters-get",
			"clusters-get-remote",
			"clusters-get-unknown",
		)
	})

	It("gives the kubeconfig each cluster is reached through, for an admin only", func() {
		sc.replay(
			"clusters-kubeconfig-local",
			"clusters-kubeconfig-remote",
			"clusters-kubeconfig-unknown",
			"clusters-kubeconfig-tenant-role",
		)
	})

	It("shows each cluster's portals, environments, sessions and users, for an admin only", func() {
		sc.replay(
			"initial-cluster-a-portals",
			"initial-portal-a",
			"initial-portal-a-environments",
			"initial-portal-a-w01",
			"initial-portal-a-w01-sessions",
			"initial-portal-a-w01-users",
			"initial-portal-a-w02",
			"initial-portal-a-w02-sessions",
			"initial-portal-a-w02-users",
			"initial-cluster-b-portals",
			"initial-portal-b",
			"initial-portal-b-environments",
			"initial-portal-b-w01",
			"initial-portal-b-w01-sessions",
			"initial-portal-b-w01-users",
			"environment-user-sessions-none",
			"portal-tenant-role",
		)
	})

	It("answers 404 for a cluster, portal or environment it does not have", func() {
		sc.replay(
			"cluster-portals-unknown-cluster",
			"portal-unknown",
			"portal-unknown-cluster",
			"environments-unknown-portal",
			"environment-unknown",
			"environment-sessions-unknown",
			"environment-users-unknown",
			"environment-user-sessions-unknown",
		)
	})

	// The responses of a later step follow changes the watches deliver in
	// no particular order, so each is waited for.
	DescribeTable("follows the scenario's steps, counting sessions awaiting activation or stopping as allocated",
		func(step string, names ...string) {
			applyState(sc.session, step)
			for _, name := range names {
				sc.eventuallyReplay(name)
			}
		},
		Entry("with a session awaiting activation", "beta-allocating",
			"allocating-portal-a",
			"allocating-portal-a-w02",
			"allocating-portal-a-w02-sessions",
			"allocating-portal-a-w02-users",
			"allocating-portal-a-w02-user-sessions",
		),
		Entry("with the session activated", "beta-allocated",
			"allocated-portal-a",
			"allocated-portal-a-w02",
			"allocated-portal-a-w02-sessions",
		),
		Entry("with a session awaiting activation on the remote cluster", "remote-allocating",
			"remote-allocating-portal-b",
			"remote-allocating-portal-b-w01",
			"remote-allocating-portal-b-w01-sessions",
			"remote-allocating-portal-b-w01-users",
			"remote-allocating-user-2-sessions",
			"remote-allocating-tenant-all-portals",
		),
		Entry("with an environment at its capacity", "portal-a-busy",
			"busy-cluster-a-portals",
			"busy-portal-a",
			"busy-portal-a-environments",
			"busy-portal-a-w01",
			"busy-portal-a-w01-sessions",
			"busy-portal-a-w01-users",
			"busy-portal-a-w02",
			"busy-portal-a-w02-sessions",
			"busy-portal-a-w02-users",
		),
		Entry("with a reserved session stopping", "reserved-stopping",
			"stopping-portal-a-w01-sessions",
			"stopping-portal-a-w01",
			"stopping-portal-a",
		),
		Entry("with every session settled", "sessions-settled",
			"settled-cluster-a-portals",
			"settled-portal-a",
			"settled-portal-a-environments",
			"settled-portal-a-w01",
			"settled-portal-a-w01-sessions",
			"settled-portal-a-w01-users",
			"settled-portal-a-w02",
			"settled-portal-a-w02-sessions",
			"settled-portal-a-w02-users",
			"settled-cluster-b-portals",
			"settled-portal-b",
			"settled-portal-b-environments",
			"settled-portal-b-w01",
			"settled-portal-b-w01-sessions",
			"settled-portal-b-w01-users",
			"settled-user-1-sessions",
			"settled-fixed-user-sessions",
		),
		Entry("with a portal label without a value", "label-without-value",
			"label-portal-b",
			"label-cluster-b-portals",
			"label-tenant-portals-tenant-names",
		),
	)

	It("stops showing a cluster once its ClusterConfig is deleted, and after a restart", func() {
		Expect(k8sClient.Delete(context.Background(), scenarioClusterConfig("cluster-b"))).To(Succeed())
		applyState(sc.session, "cluster-b-deleted")
		sc.eventuallyReplay("deleted-clusters-list")
		sc.eventuallyReplay("deleted-portals-list")
		sc.replay("deleted-cluster-get")

		sc.restart()
		applyState(sc.session, "restarted")
		// The login after the restart issues a new token, which differs from
		// the first one only once its expiry, in seconds, does.
		time.Sleep(time.Until(time.Now().Truncate(time.Second).Add(time.Second)))
		sc.replay("restarted-login-admin")
		sc.eventuallyReplay("restarted-clusters-list")
	})

	It("shows what a resource's status does not give as null, and a capacity it does not give as 0", func() {
		updateTraining(clusterB, trainingPortalKind, "portal-b", func(portal map[string]any) {
			delete(portal["status"].(map[string]any)["educates"].(map[string]any), "url")
		})
		updateTraining(clusterB, workshopEnvironmentKind, "portal-b-w01", func(environment map[string]any) {
			status := environment["status"].(map[string]any)["educates"].(map[string]any)
			delete(status, "capacity")
			delete(status, "phase")
			delete(status["workshop"].(map[string]any)["spec"].(map[string]any), "title")
		})

		portal := "/api/v1/clusters/cluster-b/portals/portal-b"
		Eventually(sc.adminObject).WithArguments(portal).WithTimeout(inventoryTimeout).
			Should(HaveKeyWithValue("url", BeNil()))
		Eventually(sc.adminObject).WithArguments(portal + "/environments/portal-b-w01").WithTimeout(inventoryTimeout).
			Should(And(
				HaveKeyWithValue("capacity", BeEquivalentTo(0)),
				HaveKeyWithValue("phase", BeNil()),
				HaveKeyWithValue("title", BeNil()),
				HaveKeyWithValue("description", Equal("A workshop both clusters offer.")),
			))
	})

	It("leaves out a cluster whose kubeconfig is refused", func() {
		updateClusterConfig("cluster-b", func(config *lookupv1beta1.ClusterConfig) {
			config.Spec.Credentials.Kubeconfig.Context = "nowhere"
		})
		Eventually(sc.clusterNames).WithTimeout(inventoryTimeout).Should(ConsistOf("cluster-a"))
		for _, path := range []string{"/api/v1/clusters/cluster-b", "/api/v1/clusters/cluster-b/portals"} {
			status, body := apiRequest(sc.service.url, http.MethodGet, path, sc.adminToken(), "")
			Expect(status).To(Equal(http.StatusNotFound), path)
			Expect(body).To(Equal("Cluster not available"), path)
		}

		updateClusterConfig("cluster-b", func(config *lookupv1beta1.ClusterConfig) {
			config.Spec.Credentials.Kubeconfig.Context = ""
		})
		sc.eventuallyReplay("clusters-list")
	})
})

// adminObject returns the JSON object an admin route answers with.
func (sc *scenario) adminObject(path string) map[string]any {
	GinkgoHelper()
	status, body := apiRequest(sc.service.url, http.MethodGet, path, sc.adminToken(), "")
	Expect(status).To(Equal(http.StatusOK), body)
	var object map[string]any
	Expect(decodeJSONString(body, &object)).To(Succeed())
	return object
}

// clusterNames returns the names of the clusters the admin route lists.
func (sc *scenario) clusterNames() []string {
	GinkgoHelper()
	status, body := apiRequest(sc.service.url, http.MethodGet, "/api/v1/clusters", sc.adminToken(), "")
	Expect(status).To(Equal(http.StatusOK), body)
	var list struct {
		Clusters []struct {
			Name string `json:"name"`
		} `json:"clusters"`
	}
	Expect(decodeJSONString(body, &list)).To(Succeed())
	names := make([]string, 0, len(list.Clusters))
	for _, cluster := range list.Clusters {
		names = append(names, cluster.Name)
	}
	return names
}
