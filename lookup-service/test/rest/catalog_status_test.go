package rest

import (
	"net/http"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("The workshop catalog of a workshop without a title or description", func() {
	sc := setUpScenario()

	It("shows the title and description as null", func() {
		sc.awaitClusters()
		updateTraining(clusterB, workshopEnvironmentKind, "portal-b-w01", func(environment map[string]any) {
			workshop := environment["status"].(map[string]any)["educates"].(map[string]any)["workshop"].(map[string]any)
			spec := workshop["spec"].(map[string]any)
			delete(spec, "title")
			delete(spec, "description")
		})

		// tenant-names reaches cluster-b only, whose one workshop is lab-alpha.
		for _, path := range []string{"/api/v1/tenants/tenant-names/workshops", "/api/v1/workshops?tenant=tenant-names"} {
			Eventually(sc.catalog).WithArguments(path).WithTimeout(inventoryTimeout).Should(ConsistOf(And(
				HaveKeyWithValue("name", "lab-alpha"),
				HaveKeyWithValue("title", BeNil()),
				HaveKeyWithValue("description", BeNil()),
			)), path)
		}
	})
})

// catalog returns the workshops a catalog route lists to an admin.
func (sc *scenario) catalog(path string) []map[string]any {
	GinkgoHelper()
	status, body := apiRequest(sc.service.url, http.MethodGet, path, sc.adminToken(), "")
	Expect(status).To(Equal(http.StatusOK), body)
	var list struct {
		Workshops []map[string]any `json:"workshops"`
	}
	Expect(decodeJSONString(body, &list)).To(Succeed())
	return list.Workshops
}
