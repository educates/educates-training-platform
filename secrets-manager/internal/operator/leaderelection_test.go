package operator_test

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	coordinationv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/educates/educates-training-platform/secrets-manager/internal/operator"
)

var _ = Describe("Leader election", func() {
	It("holds the Lease secrets-manager in its namespace, keeps a standby ready, and hands over when the leader stops",
		func() {
			namespace := createNamespace("leader-election-")
			leaderElection := func(options *operator.Options) {
				options.LeaderElection = true
				options.LeaderElectionNamespace = namespace
			}

			leader := runSecretsManager(leaderElection)
			Eventually(leaseHolder(namespace)).WithTimeout(30 * time.Second).ShouldNot(BeEmpty())
			first := leaseHolder(namespace)()
			Expect(leader.ready()).To(BeTrue())

			standby := runSecretsManager(leaderElection)
			Eventually(standby.ready).WithTimeout(30 * time.Second).Should(BeTrue())
			Consistently(leaseHolder(namespace)).WithTimeout(3 * time.Second).Should(Equal(first))

			By("releasing the Lease when the leader stops, so the standby takes over before it would expire")
			lease := &coordinationv1.Lease{}
			Expect(k8sClient.Get(context.Background(),
				client.ObjectKey{Namespace: namespace, Name: "secrets-manager"}, lease)).To(Succeed())
			Expect(lease.Spec.LeaseDurationSeconds).NotTo(BeNil())
			expiry := time.Duration(*lease.Spec.LeaseDurationSeconds) * time.Second

			leader.stop()
			Eventually(leaseHolder(namespace)).WithTimeout(expiry / 2).
				Should(And(Not(BeEmpty()), Not(Equal(first))))
		})
})

// createNamespace creates a namespace whose name starts with prefix and
// returns its name.
func createNamespace(prefix string) string {
	GinkgoHelper()
	namespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{GenerateName: prefix}}
	Expect(k8sClient.Create(context.Background(), namespace)).To(Succeed())
	return namespace.Name
}

// leaseHolder returns a function reporting the holder of the Lease
// secrets-manager in namespace, empty while there is none.
func leaseHolder(namespace string) func() string {
	return func() string {
		lease := &coordinationv1.Lease{}
		err := k8sClient.Get(context.Background(), client.ObjectKey{Namespace: namespace, Name: "secrets-manager"}, lease)
		if err != nil || lease.Spec.HolderIdentity == nil {
			return ""
		}
		return *lease.Spec.HolderIdentity
	}
}
