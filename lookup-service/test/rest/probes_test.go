package rest

import (
	"net/http"
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
