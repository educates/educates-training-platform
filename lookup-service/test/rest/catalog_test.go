package rest

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"sigs.k8s.io/controller-runtime/pkg/client"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
)

var _ = Describe("The workshop catalog", func() {
	sc := setUpScenario()

	It("lists the workshops a tenant reaches on the local cluster and on a remote one", func() {
		sc.replay("auth-login")
		// cluster-b is the only cluster tenant-names reaches, and cluster-a
		// the only one tenant-labels does.
		sc.eventuallyReplay("workshops-list-tenant-names")
		sc.eventuallyReplay("workshops-list-tenant-labels")
		sc.replay("workshops-list-tenant-all", "workshops-list-tenant-none")
	})

	It("lists every cluster's workshops for an admin naming no tenant, and a tenant's for one naming it", func() {
		sc.replay("login-alias")
		sc.awaitClusters()
		sc.replay(
			"workshops-list-admin-no-tenant",
			"workshops-list-admin-tenant",
			"workshops-list-admin-tenant-unknown",
		)
	})

	It("holds a client acting as a tenant, even one that is an admin too, to the tenants it may act for", func() {
		sc.replay("auth-login", "auth-login-both-client")
		sc.awaitClusters()
		sc.replay(
			"workshops-list-tenant-role-no-tenant",
			"workshops-list-tenant-not-allowed",
			"workshops-list-tenant-unknown",
			"workshops-list-both-roles-no-tenant",
			"workshops-list-both-roles-tenant-not-allowed",
			"workshops-list-no-token",
		)
	})

	It("lists a tenant's workshops by the tenant route", func() {
		sc.replay("auth-login", "login-alias")
		sc.awaitClusters()
		sc.replay(
			"tenant-workshops-tenant-all",
			"tenant-workshops-tenant-names",
			"tenant-workshops-tenant-labels",
			"tenant-workshops-tenant-none",
			"tenant-workshops-not-allowed",
			"tenant-workshops-unknown",
			"tenant-workshops-admin",
		)
	})

	It("lists only the workshops of running environments", func() {
		sc.awaitClusters()
		updateTraining(clusterA, workshopEnvironmentKind, "portal-a-w02", func(environment map[string]any) {
			environment["status"].(map[string]any)["educates"].(map[string]any)["phase"] = "Stopping"
		})
		Eventually(sc.workshops).WithArguments("tenant-labels").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha"))
	})

	It("matches a label without a value as the empty string", func() {
		sc.replay("auth-login", "login-alias")
		sc.awaitClusters()
		applyState(sc.session, "label-without-value")
		sc.eventuallyReplay("label-workshops-tenant-all")
		sc.replay("label-workshops-admin", "label-workshops-tenant-labels")

		createTenantConfig("tenant-featured", lookupv1beta1.TenantConfigSpec{
			Portals: &lookupv1beta1.ResourceSelector{LabelSelector: &lookupv1beta1.LabelSelector{
				MatchLabels: map[string]string{"featured": ""},
			}},
		})
		createTenantConfig("tenant-featured-exists", lookupv1beta1.TenantConfigSpec{
			Portals: &lookupv1beta1.ResourceSelector{LabelSelector: &lookupv1beta1.LabelSelector{
				MatchExpressions: []lookupv1beta1.LabelSelectorRequirement{{Key: "featured", Operator: "Exists"}},
			}},
		})
		createTenantConfig("tenant-featured-in", lookupv1beta1.TenantConfigSpec{
			Portals: &lookupv1beta1.ResourceSelector{LabelSelector: &lookupv1beta1.LabelSelector{
				MatchExpressions: []lookupv1beta1.LabelSelectorRequirement{
					{Key: "featured", Operator: "In", Values: []string{""}},
				},
			}},
		})
		for _, tenant := range []string{"tenant-featured", "tenant-featured-exists", "tenant-featured-in"} {
			Eventually(sc.workshops).WithArguments(tenant).WithTimeout(inventoryTimeout).
				Should(ConsistOf("lab-alpha"), "tenant %s", tenant)
		}
	})

	It("matches names with glob patterns, and requires both name and labels, and every expression, to match", func() {
		sc.awaitClusters()
		createTenantConfig("tenant-glob", lookupv1beta1.TenantConfigSpec{
			Clusters: &lookupv1beta1.ResourceSelector{NameSelector: &lookupv1beta1.NameSelector{
				MatchNames: []string{"cluster-[!b]"},
			}},
			Portals: &lookupv1beta1.ResourceSelector{NameSelector: &lookupv1beta1.NameSelector{
				MatchNames: []string{"nowhere", "portal-?"},
			}},
		})
		createTenantConfig("tenant-name-not-labels", lookupv1beta1.TenantConfigSpec{
			Clusters: &lookupv1beta1.ResourceSelector{
				NameSelector:  &lookupv1beta1.NameSelector{MatchNames: []string{"cluster-a"}},
				LabelSelector: &lookupv1beta1.LabelSelector{MatchLabels: map[string]string{"location": "b"}},
			},
		})
		createTenantConfig("tenant-some-expressions", lookupv1beta1.TenantConfigSpec{
			Portals: &lookupv1beta1.ResourceSelector{LabelSelector: &lookupv1beta1.LabelSelector{
				MatchExpressions: []lookupv1beta1.LabelSelectorRequirement{
					{Key: "region", Operator: "In", Values: []string{"east", "west"}},
					{Key: "region", Operator: "NotIn", Values: []string{"west"}},
				},
			}},
		})
		createTenantConfig("tenant-metadata-labels", lookupv1beta1.TenantConfigSpec{
			Portals: &lookupv1beta1.ResourceSelector{LabelSelector: &lookupv1beta1.LabelSelector{
				MatchExpressions: []lookupv1beta1.LabelSelectorRequirement{
					{Key: "training.educates.dev/portal.name", Operator: "Exists"},
				},
			}},
		})

		Eventually(sc.workshops).WithArguments("tenant-glob").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha", "lab-beta"))
		Eventually(sc.workshops).WithArguments("tenant-some-expressions").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha", "lab-beta"))
		Expect(sc.workshops("tenant-name-not-labels")).To(BeEmpty())
		Expect(sc.workshops("tenant-metadata-labels")).To(BeEmpty())
	})

	It("lets a client act for the tenants its glob patterns match", func() {
		sc.awaitClusters()
		createTenantConfig("tenant-1", lookupv1beta1.TenantConfigSpec{})
		createTenantConfig("tenant-x", lookupv1beta1.TenantConfigSpec{})
		client := scenarioClientConfig("tenant-client")
		client.Name = "pattern-client"
		client.Spec.Tenants = []string{"tenant-[0-9]", "tenant-?ll"}
		createClientConfig(sc.session, client, "")
		Eventually(func() int {
			return loginStatusWith("pattern-client", "golden-tenant-password", sc.service)
		}).WithTimeout(inventoryTimeout).Should(Equal(http.StatusOK), "the service has not seen the new client")
		token := sc.login("pattern-client", "golden-tenant-password")

		for _, tenant := range []string{"tenant-all", "tenant-1"} {
			Eventually(func() int {
				status, _ := apiRequest(sc.service.url, http.MethodGet, "/api/v1/tenants/"+tenant+"/workshops", token, "")
				return status
			}).WithTimeout(inventoryTimeout).Should(Equal(http.StatusOK), "tenant %s", tenant)
		}
		for _, tenant := range []string{"tenant-x", "tenant-names", "tenant-all-but"} {
			status, body := apiRequest(sc.service.url, http.MethodGet, "/api/v1/workshops?tenant="+tenant, token, "")
			Expect(status).To(Equal(http.StatusForbidden), "tenant %s", tenant)
			Expect(body).To(Equal("Client not allowed access to tenant"))
		}
	})

	It("follows changes to a TenantConfig without a restart", func() {
		sc.awaitClusters()
		Expect(sc.workshops("tenant-none")).To(BeEmpty())

		var tenant lookupv1beta1.TenantConfig
		Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: configNamespace, Name: "tenant-none"},
			&tenant)).To(Succeed())
		tenant.Spec = lookupv1beta1.TenantConfigSpec{}
		Expect(k8sClient.Update(context.Background(), &tenant)).To(Succeed())
		Eventually(sc.workshops).WithArguments("tenant-none").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha", "lab-beta"))

		Expect(k8sClient.Delete(context.Background(), &tenant)).To(Succeed())
		Eventually(func() int {
			status, _ := apiRequest(sc.service.url, http.MethodGet, "/api/v1/tenants/tenant-none/workshops",
				sc.adminToken(), "")
			return status
		}).WithTimeout(inventoryTimeout).Should(Equal(http.StatusNotFound))
	})
})

// loginStatusWith returns the status of a login to svc with that name and
// password.
func loginStatusWith(name, password string, svc *lookupService) int {
	GinkgoHelper()
	status, _ := apiRequest(svc.url, http.MethodPost, "/auth/login", "",
		`{"username": "`+name+`", "password": "`+password+`"}`)
	return status
}

// adminToken logs admin-client in and returns its token, for a spec that
// reads the service's state through an admin route.
func (sc *scenario) adminToken() string {
	GinkgoHelper()
	if sc.admin == "" {
		sc.admin = sc.login("admin-client", "golden-admin-password")
	}
	return sc.admin
}

// workshops returns the names of the workshops the tenant reaches, as the
// tenant route lists them to an admin.
func (sc *scenario) workshops(tenant string) []string {
	GinkgoHelper()
	status, body := apiRequest(sc.service.url, http.MethodGet, "/api/v1/tenants/"+tenant+"/workshops", sc.adminToken(), "")
	if status != http.StatusOK {
		// The service has not seen the tenant yet. The answer stands in for
		// the list, so that a spec waiting for one tries again.
		return []string{fmt.Sprintf("%d: %s", status, body)}
	}
	return workshopNames(body)
}

// workshopNames returns the names of the workshops a workshop list holds,
// in order of name.
func workshopNames(body string) []string {
	GinkgoHelper()
	var list struct {
		Workshops []struct {
			Name string `json:"name"`
		} `json:"workshops"`
	}
	Expect(decodeJSONString(body, &list)).To(Succeed())
	names := make([]string, 0, len(list.Workshops))
	for _, workshop := range list.Workshops {
		names = append(names, workshop.Name)
	}
	slices.Sort(names)
	return names
}

// awaitClusters waits until the service shows the training resources of
// both clusters: tenant-labels reaches only cluster-a, and tenant-names
// only cluster-b.
func (sc *scenario) awaitClusters() {
	GinkgoHelper()
	Eventually(sc.workshops).WithArguments("tenant-labels").WithTimeout(inventoryTimeout).
		Should(ConsistOf("lab-alpha", "lab-beta"))
	Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).
		Should(ConsistOf("lab-alpha"))
}

// createTenantConfig creates a TenantConfig in the config namespace.
func createTenantConfig(name string, spec lookupv1beta1.TenantConfigSpec) {
	GinkgoHelper()
	Expect(k8sClient.Create(context.Background(), &lookupv1beta1.TenantConfig{
		ObjectMeta: objectMeta(configNamespace, name),
		Spec:       spec,
	})).To(Succeed())
}

// consistentlyEmpty checks that the tenant reaches no workshop for a while,
// long enough for a cache to sync and to be started again.
func (sc *scenario) consistentlyEmpty(tenant string) {
	GinkgoHelper()
	Consistently(sc.workshops).WithArguments(tenant).WithTimeout(clusterSyncTimeout+time.Second).
		Should(BeEmpty(), "tenant %s", tenant)
}
