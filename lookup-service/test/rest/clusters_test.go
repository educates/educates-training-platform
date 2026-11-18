package rest

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/educates/educates-training-platform/lookup-service/internal/clusters"
)

const (
	// remoteAccessNamespace and remoteAccessAccount name the identity the
	// service reaches every cluster as, which the remote-access subchart
	// installs.
	remoteAccessNamespace = "educates"
	remoteAccessAccount   = "remote-access"

	// kubeconfigSecret is the Secret, in the config namespace, holding
	// cluster-b's kubeconfig under the default key.
	kubeconfigSecret = "cluster-b-kubeconfig"
)

// localTokenDirectory stands for the directory the remote-access token is
// mounted at in the service's pod.
var localTokenDirectory string

// testCluster is a cluster of the scenario: an envtest API server holding
// the training resources, with the remote-access identity the service
// reaches it as.
type testCluster struct {
	// name is the cluster's name in the scenario, as cluster-a.
	name string

	env *envtest.Environment
	// config and client reach the API server as its administrator.
	config *rest.Config
	client client.Client

	// token is a token of the remote-access identity, which may list and
	// watch the training resources and nothing else.
	token string
}

// startTestCluster starts an API server with the CRDs of the directories
// and creates the remote-access identity on it.
func startTestCluster(name string, crdDirectories ...string) *testCluster {
	GinkgoHelper()
	env := &envtest.Environment{CRDDirectoryPaths: crdDirectories, ErrorIfCRDPathMissing: true}
	if dir := firstEnvTestBinaryDir(); dir != "" {
		env.BinaryAssetsDirectory = dir
	}
	config, err := env.Start()
	Expect(err).NotTo(HaveOccurred())
	c, err := client.New(config, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	cluster := &testCluster{name: name, env: env, config: config, client: c}
	cluster.token = cluster.createRemoteAccessIdentity()
	return cluster
}

// createRemoteAccessIdentity creates the remote-access service account with
// the rights the remote-access subchart grants it, and returns a token for
// it.
func (c *testCluster) createRemoteAccessIdentity() string {
	GinkgoHelper()
	ctx := context.Background()
	Expect(c.client.Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: remoteAccessNamespace}})).
		To(Succeed())
	Expect(c.client.Create(ctx, &corev1.ServiceAccount{
		ObjectMeta: objectMeta(remoteAccessNamespace, remoteAccessAccount),
	})).To(Succeed())
	Expect(c.client.Create(ctx, &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{Name: "educates:remote-access"},
		Rules: []rbacv1.PolicyRule{{
			APIGroups: []string{"training.educates.dev"},
			Resources: []string{"trainingportals", "workshopenvironments", "workshopsessions"},
			Verbs:     []string{"list", "watch"},
		}},
	})).To(Succeed())
	Expect(c.client.Create(ctx, &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{Name: "educates:remote-access"},
		RoleRef:    rbacv1.RoleRef{APIGroup: rbacv1.GroupName, Kind: "ClusterRole", Name: "educates:remote-access"},
		Subjects: []rbacv1.Subject{{
			Kind: rbacv1.ServiceAccountKind, Name: remoteAccessAccount, Namespace: remoteAccessNamespace,
		}},
	})).To(Succeed())

	clientset, err := kubernetes.NewForConfig(c.config)
	Expect(err).NotTo(HaveOccurred())
	lifetime := int64(48 * 60 * 60)
	request, err := clientset.CoreV1().ServiceAccounts(remoteAccessNamespace).CreateToken(ctx, remoteAccessAccount,
		&authenticationv1.TokenRequest{Spec: authenticationv1.TokenRequestSpec{ExpirationSeconds: &lifetime}},
		metav1.CreateOptions{})
	Expect(err).NotTo(HaveOccurred())
	return request.Status.Token
}

// kubeconfig returns a kubeconfig reaching the cluster at server as the
// remote-access identity, as educates admin lookup kubeconfig writes it.
func (c *testCluster) kubeconfig(server string) string {
	return fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- name: training-platform
  cluster:
    server: %s
    certificate-authority-data: %s
contexts:
- name: training-platform
  context:
    cluster: training-platform
    user: remote-access
current-context: training-platform
users:
- name: remote-access
  user:
    token: %s
`, server, base64.StdEncoding.EncodeToString(c.config.CAData), c.token)
}

// writeLocalToken mounts cluster-a's remote-access token and CA, as kubelet
// does from the remote-access-token Secret.
func writeLocalToken() {
	GinkgoHelper()
	Expect(os.WriteFile(filepath.Join(localTokenDirectory, "token"), []byte(clusterA.token), 0o600)).To(Succeed())
	Expect(os.WriteFile(filepath.Join(localTokenDirectory, "ca.crt"), clusterA.config.CAData, 0o600)).To(Succeed())
}

// removeLocalToken empties the token mount, as when the remote-access-token
// Secret does not exist.
func removeLocalToken() {
	GinkgoHelper()
	for _, name := range []string{"token", "ca.crt"} {
		err := os.Remove(filepath.Join(localTokenDirectory, name))
		if !os.IsNotExist(err) {
			Expect(err).NotTo(HaveOccurred())
		}
	}
}

// clusterOptions are the options the suite runs the cluster registry with:
// the local cluster is cluster-a's API server, reached through the token
// mount, with a kubeconfig that names the in-cluster address as in a pod,
// and a cache that does not sync is retried within seconds.
func clusterOptions() clusters.Options {
	return clusters.Options{
		LocalTokenDirectory:   localTokenDirectory,
		LocalServer:           clusterA.config.Host,
		LocalKubeconfigServer: clusters.DefaultLocalServer,
		SyncTimeout:           clusterSyncTimeout,
		RetryInterval:         clusterRetryInterval,
	}
}
