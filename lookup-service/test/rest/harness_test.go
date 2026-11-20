package rest

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	utilyaml "k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
	"github.com/educates/educates-training-platform/lookup-service/internal/service"
	"github.com/educates/educates-training-platform/lookup-service/internal/tokens"
	"github.com/educates/educates-training-platform/lookup-service/test/golden"
)

const (
	// clusterSyncTimeout is how long the suite's service waits for a
	// cluster's cache to sync before it tries again, and
	// clusterRetryInterval how long it waits before it does.
	clusterSyncTimeout   = 3 * time.Second
	clusterRetryInterval = 500 * time.Millisecond

	// inventoryTimeout bounds the wait for the service to show a change to
	// the clusters' configuration or training resources.
	inventoryTimeout = 20 * time.Second
)

// scenarioDir holds the resources of the golden scenario.
var scenarioDir = filepath.Join("..", "..", "testdata", "golden", "scenario")

// lookupService is the service's composition running in process.
type lookupService struct {
	// url is the base URL of the REST API.
	url string
	// probeURL is the base URL of the probes.
	probeURL string
	// logs records what the service logs.
	logs *logRecorder

	cancel context.CancelFunc
	done   chan error
}

// startLookupService runs the service against the lookup cluster, adjusting
// its options with each of adjust, and waits for it to be ready.
func startLookupService(adjust ...func(*service.Options)) *lookupService {
	s := runLookupService(adjust...)
	Eventually(s.ready).WithTimeout(30 * time.Second).Should(BeTrue())
	return s
}

// runLookupService runs the service without waiting for it to be ready.
func runLookupService(adjust ...func(*service.Options)) *lookupService {
	GinkgoHelper()

	apiListener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())
	probeListener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())

	logs := &logRecorder{}
	// The namespaces come from the environment the suite sets up.
	options := service.Options{
		RESTConfig:    restConfig,
		APIListener:   apiListener,
		ProbeListener: probeListener,
		Clusters:      clusterOptions(),
		Portals:       portalOptions(),
		Placement:     placementOptions(),
		Logger:        logs.logger(logf.Log.WithName("lookup-service")),
	}
	for _, adjustment := range adjust {
		adjustment(&options)
	}

	ctx, cancel := context.WithCancel(context.Background())
	svc, err := service.New(ctx, options)
	if err != nil {
		cancel()
		Expect(err).NotTo(HaveOccurred())
	}

	s := &lookupService{
		url:      "http://" + apiListener.Addr().String(),
		probeURL: "http://" + probeListener.Addr().String(),
		logs:     logs,
		cancel:   cancel,
		done:     make(chan error, 1),
	}
	go func() {
		defer GinkgoRecover()
		s.done <- svc.Run(ctx)
	}()
	return s
}

// ready reports whether the readiness probe answers 200.
func (s *lookupService) ready() bool {
	status, _ := s.probe("/readyz")
	return status == http.StatusOK
}

// probe calls a probe and returns its status and body.
func (s *lookupService) probe(path string) (int, string) {
	response, err := http.Get(s.probeURL + path)
	if err != nil {
		return 0, ""
	}
	defer func() { _ = response.Body.Close() }()
	body, _ := io.ReadAll(response.Body)
	return response.StatusCode, string(body)
}

// stop stops the service and waits for it to have stopped.
func (s *lookupService) stop() {
	GinkgoHelper()
	s.cancel()
	var err error
	Eventually(s.done).WithTimeout(30 * time.Second).Should(Receive(&err))
	Expect(err).NotTo(HaveOccurred())
}

// scenario is the golden scenario: its configuration in the lookup cluster
// and the training resources of both clusters, with the service running
// against them and a golden session replaying fixtures against the service.
type scenario struct {
	service *lookupService
	session *golden.Session
	adjust  []func(*service.Options)
	// admin is admin-client's token, once a spec asked for it.
	admin string
}

// setUpScenario creates the scenario's configuration and the training
// resources of its initial step, and starts the service, before each spec
// of the container, and removes them after it, so that each spec starts
// from the first start of the service.
func setUpScenario() *scenario {
	sc := &scenario{}
	BeforeEach(func() {
		sc.adjust = nil
		sc.admin = ""
		sc.session = fixtures.NewSession("")
		createClientConfigs(sc.session)
		createTenantConfigs()
		createKubeconfigSecret(clusterB.kubeconfig(clusterB.config.Host))
		sc.session.Bind("<server:cluster-b>", clusterB.config.Host)
		createClusterConfigs()
		applyState(sc.session, "initial")
		sc.start()
	})
	AfterEach(func() {
		if sc.service != nil {
			sc.service.stop()
			sc.service = nil
		}
		removeConfiguration()
	})
	return sc
}

func (sc *scenario) start() {
	GinkgoHelper()
	sc.service = startLookupService(sc.adjust...)
	sc.session.BaseURL = sc.service.url
}

// restart stops the service and starts it again, against the same token
// Secret.
func (sc *scenario) restart() {
	GinkgoHelper()
	sc.service.stop()
	sc.start()
}

// replay replays golden fixtures in turn.
func (sc *scenario) replay(names ...string) {
	GinkgoHelper()
	for _, name := range names {
		Expect(sc.session.Replay(name)).To(Succeed())
	}
}

// eventuallyReplay replays a fixture until the service answers as it
// expects, for a response that follows a change the watch delivers later.
func (sc *scenario) eventuallyReplay(name string) {
	GinkgoHelper()
	Eventually(func() error { return sc.session.Replay(name) }).
		WithTimeout(inventoryTimeout).Should(Succeed())
}

// decodeScenario returns the resources of a file of the golden scenario.
func decodeScenario[T any, PT interface {
	*T
	client.Object
}](file string) []PT {
	GinkgoHelper()
	data, err := os.ReadFile(filepath.Join(scenarioDir, file))
	Expect(err).NotTo(HaveOccurred())

	var objects []PT
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		object := PT(new(T))
		err := decoder.Decode(object)
		if errors.Is(err, io.EOF) {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		if object.GetName() != "" {
			objects = append(objects, object)
		}
	}
	Expect(objects).NotTo(BeEmpty())
	return objects
}

// scenarioClientConfigs returns the ClientConfigs of the golden scenario.
func scenarioClientConfigs() []*lookupv1beta1.ClientConfig {
	GinkgoHelper()
	return decodeScenario[lookupv1beta1.ClientConfig](filepath.Join("lookup", "clientconfigs.yaml"))
}

// scenarioClientConfig returns the scenario's ClientConfig of that name.
func scenarioClientConfig(name string) *lookupv1beta1.ClientConfig {
	GinkgoHelper()
	for _, config := range scenarioClientConfigs() {
		if config.Name == name {
			return config
		}
	}
	Fail("the scenario has no ClientConfig " + name)
	return nil
}

// createClientConfigs creates the scenario's ClientConfigs in the config
// namespace, binding each uid in the session.
func createClientConfigs(session *golden.Session) {
	GinkgoHelper()
	for _, config := range scenarioClientConfigs() {
		createClientConfig(session, config, "")
	}
}

// createClientConfig creates config in the config namespace and binds its
// uid to the placeholder of that client, with suffix, such as #2 for a
// client created again.
func createClientConfig(session *golden.Session, config *lookupv1beta1.ClientConfig, suffix string) {
	GinkgoHelper()
	config.Namespace = configNamespace
	Expect(k8sClient.Create(context.Background(), config)).To(Succeed())
	session.Bind(uidPlaceholder("ClientConfig", config.Name+suffix), string(config.UID))
}

// createTenantConfigs creates the scenario's TenantConfigs in the config
// namespace.
func createTenantConfigs() {
	GinkgoHelper()
	for _, config := range decodeScenario[lookupv1beta1.TenantConfig](filepath.Join("lookup", "tenantconfigs.yaml")) {
		config.Namespace = configNamespace
		Expect(k8sClient.Create(context.Background(), config)).To(Succeed())
	}
}

// scenarioClusterConfig returns the scenario's ClusterConfig of that name.
func scenarioClusterConfig(name string) *lookupv1beta1.ClusterConfig {
	GinkgoHelper()
	for _, config := range decodeScenario[lookupv1beta1.ClusterConfig](filepath.Join("lookup", "clusterconfigs.yaml")) {
		if config.Name == name {
			config.Namespace = configNamespace
			return config
		}
	}
	Fail("the scenario has no ClusterConfig " + name)
	return nil
}

// createClusterConfigs creates the scenario's ClusterConfigs in the config
// namespace: cluster-a, the lookup cluster, and cluster-b, reached through
// the kubeconfig Secret.
func createClusterConfigs() {
	GinkgoHelper()
	for _, name := range []string{"cluster-a", "cluster-b"} {
		Expect(k8sClient.Create(context.Background(), scenarioClusterConfig(name))).To(Succeed())
	}
}

// createKubeconfigSecret creates cluster-b's kubeconfig Secret with the
// kubeconfig under the default key.
func createKubeconfigSecret(kubeconfig string) {
	GinkgoHelper()
	Expect(k8sClient.Create(context.Background(), &corev1.Secret{
		ObjectMeta: objectMeta(configNamespace, kubeconfigSecret),
		Data:       map[string][]byte{"config": []byte(kubeconfig)},
	})).To(Succeed())
}

// uidPlaceholder is the placeholder of the uid of an object on the lookup
// cluster, cluster-a in the scenario.
func uidPlaceholder(kind, name string) string {
	return clusterUIDPlaceholder("cluster-a", kind, name)
}

// removeConfiguration deletes the lookup configuration, the kubeconfig
// Secrets, the service's token Secret and the training resources of both
// clusters.
func removeConfiguration() {
	GinkgoHelper()
	ctx := context.Background()
	var namespaces corev1.NamespaceList
	Expect(k8sClient.List(ctx, &namespaces)).To(Succeed())
	for _, namespace := range namespaces.Items {
		in := client.InNamespace(namespace.Name)
		Expect(k8sClient.DeleteAllOf(ctx, &lookupv1beta1.ClientConfig{}, in)).To(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &lookupv1beta1.TenantConfig{}, in)).To(Succeed())
		var clusterConfigs lookupv1beta1.ClusterConfigList
		Expect(k8sClient.List(ctx, &clusterConfigs, in)).To(Succeed())
		for i := range clusterConfigs.Items {
			removeClusterConfig(&clusterConfigs.Items[i])
		}
	}
	Expect(k8sClient.DeleteAllOf(ctx, &corev1.Secret{}, client.InNamespace(configNamespace))).To(Succeed())
	err := k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: objectMeta(serviceNamespace, tokens.SecretName)})
	if !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
	clusterA.removeTraining()
	clusterB.removeTraining()
	writeLocalToken()
}

// removeClusterConfig deletes a ClusterConfig, removing any finalizer that
// would hold it back.
func removeClusterConfig(config *lookupv1beta1.ClusterConfig) {
	GinkgoHelper()
	ctx := context.Background()
	if len(config.Finalizers) > 0 {
		config.Finalizers = nil
		err := k8sClient.Update(ctx, config)
		if apierrors.IsNotFound(err) {
			return
		}
		Expect(err).NotTo(HaveOccurred())
	}
	err := k8sClient.Delete(ctx, config)
	if !apierrors.IsNotFound(err) {
		Expect(err).NotTo(HaveOccurred())
	}
}

// apiRequest sends a request to the service's REST API with an optional
// bearer token and returns the status and body of the response.
func apiRequest(baseURL, method, path, token, body string) (int, string) {
	GinkgoHelper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	request, err := http.NewRequest(method, baseURL+path, reader)
	Expect(err).NotTo(HaveOccurred())
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, err := http.DefaultClient.Do(request)
	Expect(err).NotTo(HaveOccurred())
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(response.Body)
	Expect(err).NotTo(HaveOccurred())
	return response.StatusCode, string(data)
}
