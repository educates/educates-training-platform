package rest

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
)

// kopfFinalizer is the finalizer the Python service's framework left on the
// configuration resources it handled.
const kopfFinalizer = "kopf.zalando.org/KopfFinalizerMarker"

var _ = Describe("The clusters", func() {
	sc := setUpScenario()

	It("stop offering a cluster's portals once its ClusterConfig is deleted, and offer them when it is back", func() {
		sc.replay("auth-login")
		sc.awaitClusters()
		sc.replay("workshops-list-tenant-names")

		By("deleting cluster-b's ClusterConfig, which a finalizer holds back")
		updateClusterConfig("cluster-b", func(config *lookupv1beta1.ClusterConfig) {
			config.Finalizers = append(config.Finalizers, kopfFinalizer)
		})
		Expect(k8sClient.Delete(context.Background(), scenarioClusterConfig("cluster-b"))).To(Succeed())
		applyState(sc.session, "cluster-b-deleted")
		sc.eventuallyReplay("deleted-workshops-tenant-names")

		By("creating it again")
		removeClusterConfig(clusterConfig("cluster-b"))
		Expect(k8sClient.Create(context.Background(), scenarioClusterConfig("cluster-b"))).To(Succeed())
		sc.eventuallyReplay("workshops-list-tenant-names")
	})

	It("connect to a cluster again when its kubeconfig Secret changes", func() {
		sc.awaitClusters()

		By("pointing cluster-b's kubeconfig at cluster-a's API server")
		updateKubeconfigSecret("config", clusterA.kubeconfig(clusterA.config.Host))
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha", "lab-beta"))

		By("pointing it back at cluster-b's")
		updateKubeconfigSecret("config", clusterB.kubeconfig(clusterB.config.Host))
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha"))

		By("giving it a token the cluster refuses")
		updateKubeconfigSecret("config", strings.Replace(clusterB.kubeconfig(clusterB.config.Host),
			clusterB.token, "not-a-token", 1))
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).Should(BeEmpty())
		Eventually(sc.service.logs.Lines).WithTimeout(inventoryTimeout).Should(ContainElement(And(
			ContainSubstring("Cannot list or watch the cluster's training resources"),
			ContainSubstring("cluster=cluster-b"))))
	})

	It("follow a ClusterConfig's labels, and its kubeconfig Secret's key, without a restart", func() {
		sc.awaitClusters()

		updateClusterConfig("cluster-a", func(config *lookupv1beta1.ClusterConfig) {
			config.Spec.Labels[0].Value = new("c")
		})
		Eventually(sc.workshops).WithArguments("tenant-labels").WithTimeout(inventoryTimeout).Should(BeEmpty())

		By("moving cluster-b's kubeconfig to another key of its Secret")
		updateKubeconfigSecret("kubeconfig", clusterA.kubeconfig(clusterA.config.Host))
		updateClusterConfig("cluster-b", func(config *lookupv1beta1.ClusterConfig) {
			config.Spec.Credentials.Kubeconfig.SecretRef.Key = "kubeconfig"
		})
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha", "lab-beta"))
	})

	It("do not connect a remote cluster whose kubeconfig Secret is missing, until it appears", func() {
		sc.awaitClusters()
		Expect(k8sClient.Delete(context.Background(), &corev1.Secret{
			ObjectMeta: objectMeta(configNamespace, kubeconfigSecret),
		})).To(Succeed())
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).Should(BeEmpty())
		Eventually(sc.service.logs.Lines).WithTimeout(inventoryTimeout).Should(ContainElement(And(
			ContainSubstring("the kubeconfig Secret cluster-b-kubeconfig does not exist"),
			ContainSubstring("cluster=cluster-b"))))

		createKubeconfigSecret(clusterB.kubeconfig(clusterB.config.Host))
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha"))
	})

	Describe("validating the selected kubeconfig context only", func() {
		// The kubeconfig holds several contexts. The one a ClusterConfig
		// selects reaches cluster-a, so that tenant-names, which reaches
		// cluster-b only, lists cluster-a's lab-beta once it is connected.
		kubeconfig := func(selected string) string {
			ca := base64.StdEncoding.EncodeToString(clusterA.config.CAData)
			return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: no-ca
  cluster:
    server: %[1]s
- name: cluster-a
  cluster:
    server: %[1]s
    certificate-authority-data: %[2]s
contexts:
- name: exec
  context: {cluster: cluster-a, user: exec}
- name: client-certificate
  context: {cluster: cluster-a, user: client-certificate}
- name: no-token
  context: {cluster: cluster-a, user: no-token}
- name: no-ca
  context: {cluster: no-ca, user: token}
- name: token
  context: {cluster: cluster-a, user: token}
current-context: %[4]s
users:
- name: exec
  user:
    token: %[3]s
    exec:
      apiVersion: client.authentication.k8s.io/v1
      command: gke-gcloud-auth-plugin
- name: client-certificate
  user:
    token: %[3]s
    client-certificate-data: Y2VydGlmaWNhdGU=
    client-key-data: a2V5
- name: no-token
  user: {}
- name: token
  user:
    token: %[3]s
`, clusterA.config.Host, ca, clusterA.token, selected)
		}

		// useKubeconfig gives cluster-b the kubeconfig, with context as the
		// ClusterConfig's context and current as the kubeconfig's.
		useKubeconfig := func(context, current string) {
			updateKubeconfigSecret("config", kubeconfig(current))
			updateClusterConfig("cluster-b", func(config *lookupv1beta1.ClusterConfig) {
				config.Spec.Credentials.Kubeconfig.Context = context
			})
		}

		BeforeEach(func() { sc.awaitClusters() })

		It("accepts a selected context with a token and a CA, whatever the other contexts use", func() {
			useKubeconfig("token", "exec")
			Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).
				Should(ConsistOf("lab-alpha", "lab-beta"))
		})

		It("selects the kubeconfig's current context when the ClusterConfig names none", func() {
			useKubeconfig("", "token")
			Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).
				Should(ConsistOf("lab-alpha", "lab-beta"))
		})

		DescribeTable("refuses a selected context, saying why",
			func(context, current, reason string) {
				useKubeconfig(context, current)
				Eventually(sc.service.logs.Lines).WithTimeout(inventoryTimeout).Should(ContainElement(And(
					ContainSubstring("The cluster is not connected"),
					ContainSubstring("cluster=cluster-b"),
					ContainSubstring(reason))))
				Expect(sc.workshops("tenant-names")).To(BeEmpty())
			},
			Entry("that the kubeconfig does not have", "nowhere", "token", `the kubeconfig has no context "nowhere"`),
			Entry("whose user runs an exec plugin", "exec", "token", `its user "exec" authenticates with exec`),
			Entry("whose user has a client certificate", "client-certificate", "token",
				`its user "client-certificate" authenticates with client-certificate-data`),
			Entry("whose user has no token", "no-token", "token", `its user "no-token" has no token`),
			Entry("whose cluster has no CA", "no-ca", "token", `its cluster "no-ca" has no certificate-authority-data`),
			Entry("that is the current context and uses an exec plugin", "", "exec", `its user "exec" authenticates with exec`),
		)
	})

	It("leave out a cluster whose cache does not sync, without holding back the others or readiness", func() {
		sc.awaitClusters()

		gate := newGatedAPIServer(clusterB.config, "/workshopsessions")
		DeferCleanup(gate.close)
		updateKubeconfigSecret("config", clusterB.kubeconfig(gate.config.Host))
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).Should(BeEmpty())

		Eventually(sc.service.logs.Lines).WithTimeout(inventoryTimeout).Should(ContainElement(And(
			ContainSubstring("The cluster's cache has not synced in time"),
			ContainSubstring("cluster=cluster-b"))))
		sc.consistentlyEmpty("tenant-names")
		Expect(sc.workshops("tenant-labels")).To(ConsistOf("lab-alpha", "lab-beta"))
		Expect(sc.service.ready()).To(BeTrue())

		sc.restart()
		Expect(sc.workshops("tenant-names")).To(BeEmpty())
		Eventually(sc.workshops).WithArguments("tenant-labels").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha", "lab-beta"))

		gate.open()
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha"))
	})

	It("do not connect the local cluster without the remote-access token, and connect it once kubelet mounts it", func() {
		sc.replay("auth-login")
		sc.awaitClusters()

		By("removing the token from the mount")
		removeLocalToken()
		Eventually(sc.workshops).WithArguments("tenant-labels").WithTimeout(inventoryTimeout).Should(BeEmpty())
		Eventually(sc.service.logs.Lines).WithTimeout(inventoryTimeout).Should(ContainElement(And(
			ContainSubstring("the remote-access token is not mounted"),
			ContainSubstring("cluster=cluster-a"))))

		By("starting the service without the token")
		sc.restart()
		Expect(sc.service.ready()).To(BeTrue())
		Eventually(sc.workshops).WithArguments("tenant-names").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha"))
		sc.consistentlyEmpty("tenant-labels")

		By("mounting the token")
		writeLocalToken()
		sc.eventuallyReplay("workshops-list-tenant-labels")
	})

	It("show sessions and environments that arrive before their portal once the portal does", func() {
		sc.awaitClusters()
		clusterA.removeTraining()
		Eventually(sc.workshops).WithArguments("tenant-labels").WithTimeout(inventoryTimeout).Should(BeEmpty())
		admin := func() []string { return sc.adminWorkshops() }
		Eventually(admin).WithTimeout(inventoryTimeout).Should(ConsistOf("lab-alpha"))

		By("creating a session before its environment and portal")
		clusterA.createTraining(sc.session, workshopSessionKind, "<session:alpha-reserved>")
		By("creating an environment before its portal")
		clusterA.createTraining(sc.session, workshopEnvironmentKind, "portal-a-w02")
		// The environment runs, and is offered, but on a portal whose labels
		// are not known yet.
		Eventually(admin).WithTimeout(inventoryTimeout).Should(ConsistOf("lab-alpha", "lab-beta"))
		Expect(sc.workshops("tenant-labels")).To(BeEmpty())

		By("creating the portal")
		clusterA.createTraining(sc.session, trainingPortalKind, "portal-a")
		Eventually(sc.workshops).WithArguments("tenant-labels").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-beta"))

		By("creating the session's environment")
		clusterA.createTraining(sc.session, workshopEnvironmentKind, "portal-a-w01")
		Eventually(sc.workshops).WithArguments("tenant-labels").WithTimeout(inventoryTimeout).
			Should(ConsistOf("lab-alpha", "lab-beta"))

		By("deleting the portal before its environments")
		clusterA.deleteTraining(trainingPortalKind, "portal-a")
		Eventually(sc.workshops).WithArguments("tenant-labels").WithTimeout(inventoryTimeout).Should(BeEmpty())
		Expect(admin()).To(ConsistOf("lab-alpha", "lab-beta"))
		clusterA.deleteTraining(workshopEnvironmentKind, "portal-a-w02")
		Eventually(admin).WithTimeout(inventoryTimeout).Should(ConsistOf("lab-alpha"))
	})
})

// adminWorkshops returns the names of the workshops of every cluster, as
// the workshop list shows them to an admin naming no tenant.
func (sc *scenario) adminWorkshops() []string {
	GinkgoHelper()
	status, body := apiRequest(sc.service.url, http.MethodGet, "/api/v1/workshops", sc.adminToken(), "")
	Expect(status).To(Equal(http.StatusOK), body)
	return workshopNames(body)
}

// clusterConfig returns the ClusterConfig of that name in the config
// namespace.
func clusterConfig(name string) *lookupv1beta1.ClusterConfig {
	GinkgoHelper()
	var config lookupv1beta1.ClusterConfig
	Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: configNamespace, Name: name}, &config)).
		To(Succeed())
	return &config
}

// updateClusterConfig changes the ClusterConfig of that name in the config
// namespace.
func updateClusterConfig(name string, change func(*lookupv1beta1.ClusterConfig)) {
	GinkgoHelper()
	config := clusterConfig(name)
	change(config)
	Expect(k8sClient.Update(context.Background(), config)).To(Succeed())
}

// updateKubeconfigSecret replaces cluster-b's kubeconfig Secret's data with
// the kubeconfig under key.
func updateKubeconfigSecret(key, kubeconfig string) {
	GinkgoHelper()
	var secret corev1.Secret
	Expect(k8sClient.Get(context.Background(), client.ObjectKey{Namespace: configNamespace, Name: kubeconfigSecret},
		&secret)).To(Succeed())
	secret.Data = map[string][]byte{key: []byte(kubeconfig)}
	Expect(k8sClient.Update(context.Background(), &secret)).To(Succeed())
}
