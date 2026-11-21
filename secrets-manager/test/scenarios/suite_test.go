// Package scenarios is the scenario suite of the secrets-manager. It acts as
// a workshop author or platform operator does: it creates namespaces,
// Secrets, service accounts and the four secrets.educates.dev kinds through
// the Kubernetes API of a target, and asserts on the copies, the injected
// names, status and Events the secrets-manager running there produces. It
// never looks inside the secrets-manager.
//
// The target is a live cluster named by the kubeconfig in
// SECRETS_MANAGER_LIVE_KUBECONFIG, using the context in
// SECRETS_MANAGER_LIVE_CONTEXT or else the kubeconfig's current one, its
// secrets-manager running in the namespace SECRETS_MANAGER_LIVE_NAMESPACE
// names, or else educates. With
// no live cluster named, the target is an envtest API server with the four
// kinds loaded from the secrets-manager subchart's CRDs, and the
// secrets-manager's real composition running in this process against it;
// KUBEBUILDER_ASSETS names the envtest binaries, as make test sets it.
//
// Labels select scenarios in a run:
//
//   - expected-difference: a fix or an addition over the secrets-manager
//     Educates 3.x shipped, written in Python; that secrets-manager fails
//     the scenario.
//   - live-only: the scenario needs Kubernetes garbage collection, the
//     namespace controller or a secrets-manager deployed on the cluster, and
//     is skipped on a target without them.
//   - copying, selection, ownership, export-import, injection,
//     target-fields, feedback, admission and convergence: the area of
//     behavior the scenario covers.
//
// Every scenario creates uniquely named namespaces and cluster-scoped
// resources of its own and deletes them when it ends, so scenarios can run
// in parallel against one cluster. A scenario that depends on when the
// secrets-manager reconciles a resource of its own, or that changes the
// cluster for every other scenario, runs alone, after the others.
//
// Two live-only scenarios change the cluster itself: one scales the
// Deployment of the cluster's secrets-manager to no replica, and the other
// scales it to two replicas and marks every node unschedulable. Each puts
// back what it changed when it ends, whether it passed or failed. Both
// first record on the Deployment and on the nodes what they found, so that
// a run that was killed before it could put it back is undone by the next
// run against the cluster, as it starts. They are meant for a cluster of
// their own; the label filter !live-only leaves the cluster's nodes and
// secrets-manager alone.
package scenarios

import (
	"context"
	"os"
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

	if os.Getenv(liveKubeconfigEnv) == "" && os.Getenv("KUBEBUILDER_ASSETS") == "" && firstEnvTestBinaryDir() == "" {
		Skip("no target: " + liveKubeconfigEnv + " names no live cluster, and there are no envtest binaries, since " +
			"KUBEBUILDER_ASSETS is not set and make setup-envtest has downloaded none; run the suite with make test")
	}
	var err error
	target, err = selectTarget()
	Expect(err).NotTo(HaveOccurred())
	By("running the scenarios against the " + target.Name)
	// A run that was killed may have left the cluster as its scenarios
	// changed it.
	Expect(restoreCluster(context.Background(), target)).To(Succeed())
})

var _ = AfterSuite(func() {
	Expect(target.Stop()).To(Succeed())
})
