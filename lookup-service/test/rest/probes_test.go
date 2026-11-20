package rest

import (
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/educates/educates-training-platform/lookup-service/internal/service"
)

var _ = Describe("The probes", func() {
	AfterEach(removeConfiguration)

	It("serve liveness on /healthz, and readiness on /readyz once the config caches have synced", func() {
		gate := newGatedAPIServer(restConfig, "/clientconfigs")
		DeferCleanup(gate.close)

		svc := runLookupService(func(options *service.Options) { options.RESTConfig = gate.config })
		DeferCleanup(svc.stop)

		Eventually(func() int { status, _ := svc.probe("/healthz"); return status }).Should(Equal(http.StatusOK))
		Consistently(svc.ready).WithTimeout(2 * time.Second).Should(BeFalse())
		status, _ := svc.probe("/readyz")
		Expect(status).To(Equal(http.StatusServiceUnavailable))

		gate.open()
		Eventually(svc.ready).WithTimeout(30 * time.Second).Should(BeTrue())
		status, body := svc.probe("/healthz")
		Expect(status).To(Equal(http.StatusOK))
		Expect(body).To(Equal("ok"))
	})
})

var _ = Describe("Readiness", func() {
	sc := setUpScenario()

	// awaitingClusters returns the lines the service logged on its
	// configuration caches syncing, which is when it starts waiting for the
	// clusters.
	awaitingClusters := func(svc *lookupService) func() []string {
		return func() []string {
			var lines []string
			for _, line := range svc.logs.Lines() {
				if strings.Contains(line, "The configuration caches have synced") {
					lines = append(lines, line)
				}
			}
			return lines
		}
	}

	It("waits for the caches of the clusters the configuration names, so that a ready service lists their workshops", func() {
		sc.service.stop()
		sc.service = nil
		for range 3 {
			sc.start()
			// No waiting: the service is ready, so both clusters are there.
			Expect(sc.workshops("tenant-labels")).To(ConsistOf("lab-alpha", "lab-beta"))
			Expect(sc.workshops("tenant-names")).To(ConsistOf("lab-alpha"))
			sc.service.stop()
			sc.service = nil
		}
	})

	It("waits no longer than a bounded time for a cluster whose cache does not sync", func() {
		gate := newGatedAPIServer(clusterB.config, "/workshopsessions")
		DeferCleanup(gate.close)
		updateKubeconfigSecret("config", clusterB.kubeconfig(gate.config.Host))
		sc.service.stop()
		sc.service = nil

		svc := runLookupService()
		DeferCleanup(svc.stop)
		Eventually(awaitingClusters(svc)).WithTimeout(30 * time.Second).Should(HaveLen(1))
		waiting := time.Now()
		Consistently(svc.ready).WithTimeout(clusterReadyTimeout / 2).Should(BeFalse())

		Eventually(svc.ready).WithTimeout(clusterReadyTimeout + 5*time.Second).Should(BeTrue())
		Expect(time.Since(waiting)).To(BeNumerically("<", clusterReadyTimeout+3*time.Second))
		Expect(svc.logs.Lines()).To(ContainElement(And(
			ContainSubstring("Ready"), ContainSubstring("cluster-b"), ContainSubstring("have not synced"))))

		By("serving the cluster that did sync")
		sc.service = svc
		sc.session.BaseURL = svc.url
		Expect(sc.workshops("tenant-labels")).To(ConsistOf("lab-alpha", "lab-beta"))
		Expect(sc.workshops("tenant-names")).To(BeEmpty())
		sc.service = nil
	})
})
