package operator_test

import (
	"net/http"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/educates/educates-training-platform/secrets-manager/internal/operator"
)

var _ = Describe("The probes", func() {
	It("serve liveness on /healthz at once, and readiness on /readyz once the caches have synced", func() {
		gate := newGatedAPIServer("/secretcopiers")
		sm := runSecretsManager(func(options *operator.Options) { options.RESTConfig = gate.config })
		// Cleanups run last first: the gate opens before the secrets-manager
		// stops, so that it does not stop while its caches wait.
		DeferCleanup(gate.open)

		Eventually(func() int { return sm.probe("/healthz") }).WithTimeout(10 * time.Second).
			Should(Equal(http.StatusOK))
		Consistently(func() int { return sm.probe("/readyz") }).WithTimeout(2 * time.Second).
			Should(Equal(http.StatusServiceUnavailable))

		gate.open()
		Eventually(sm.ready).WithTimeout(30 * time.Second).Should(BeTrue())
		Expect(sm.probe("/healthz")).To(Equal(http.StatusOK))
	})
})
