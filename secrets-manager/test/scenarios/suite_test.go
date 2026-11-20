// Package scenarios is the scenario suite of the secrets-manager. It acts as
// a workshop author or platform operator does: it creates namespaces,
// Secrets, service accounts and the four secrets.educates.dev kinds through
// the Kubernetes API of a target, and asserts on the copies, the injected
// names, status and Events the secrets-manager running there produces. It
// never looks inside the secrets-manager.
//
// The target is a live cluster named by the kubeconfig in
// SECRETS_MANAGER_LIVE_KUBECONFIG, using the context in
// SECRETS_MANAGER_LIVE_CONTEXT or else the kubeconfig's current one. With
// no target, the suite skips.
//
// Labels select scenarios in a run:
//
//   - expected-difference: a fix or an addition over the secrets-manager
//     Educates 3.x shipped, written in Python; that secrets-manager fails
//     the scenario.
//   - live-only: the scenario needs Kubernetes garbage collection or the
//     namespace controller, and is skipped on a target without them.
//   - copying, selection, ownership, export-import, injection,
//     target-fields, feedback and admission: the area of behavior the
//     scenario covers.
//
// Every scenario creates uniquely named namespaces and cluster-scoped
// resources of its own and deletes them when it ends, so scenarios can run
// in parallel against one cluster.
package scenarios

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// target is the cluster this run drives.
var target *Target

func TestScenarios(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Secrets Manager Scenario Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	var err error
	target, err = selectTarget()
	Expect(err).NotTo(HaveOccurred())
	if target == nil {
		Skip("no target: set " + liveKubeconfigEnv + " to run the scenarios against a live cluster")
	}
	By("running the scenarios against the " + target.Name)
})

var _ = AfterSuite(func() {
	Expect(target.Stop()).To(Succeed())
})
