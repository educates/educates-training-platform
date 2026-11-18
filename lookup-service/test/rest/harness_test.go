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

// scenarioDir holds the resources of the golden scenario.
var scenarioDir = filepath.Join("..", "..", "testdata", "golden", "scenario")

// lookupService is the service's composition running in process.
type lookupService struct {
	// url is the base URL of the REST API.
	url string
	// probeURL is the base URL of the probes.
	probeURL string

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

	// The namespaces come from the environment the suite sets up.
	options := service.Options{
		RESTConfig:    restConfig,
		APIListener:   apiListener,
		ProbeListener: probeListener,
		Logger:        logf.Log.WithName("lookup-service"),
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

// scenario is the golden scenario's configuration in the lookup cluster,
// with the service running against it and a golden session replaying
// fixtures against the service.
type scenario struct {
	service *lookupService
	session *golden.Session
	adjust  []func(*service.Options)
}

// setUpScenario creates the scenario's resources and starts the service
// before each spec of the container, and removes them after it, so that
// each spec starts from the first start of the service.
func setUpScenario() *scenario {
	sc := &scenario{}
	BeforeEach(func() {
		sc.adjust = nil
		sc.session = fixtures.NewSession("")
		createClientConfigs(sc.session)
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
		WithTimeout(10 * time.Second).Should(Succeed())
}

// scenarioClientConfigs returns the ClientConfigs of the golden scenario.
func scenarioClientConfigs() []*lookupv1beta1.ClientConfig {
	GinkgoHelper()
	data, err := os.ReadFile(filepath.Join(scenarioDir, "lookup", "clientconfigs.yaml"))
	Expect(err).NotTo(HaveOccurred())

	var configs []*lookupv1beta1.ClientConfig
	decoder := utilyaml.NewYAMLOrJSONDecoder(bytes.NewReader(data), 4096)
	for {
		var config lookupv1beta1.ClientConfig
		err := decoder.Decode(&config)
		if errors.Is(err, io.EOF) {
			break
		}
		Expect(err).NotTo(HaveOccurred())
		if config.Name != "" {
			configs = append(configs, &config)
		}
	}
	Expect(configs).NotTo(BeEmpty())
	return configs
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

// uidPlaceholder is the placeholder of the uid of an object on the lookup
// cluster, cluster-a in the scenario.
func uidPlaceholder(kind, name string) string {
	return "<uid:cluster-a/" + kind + "/" + name + ">"
}

// removeConfiguration deletes the lookup configuration and the service's
// token Secret.
func removeConfiguration() {
	GinkgoHelper()
	ctx := context.Background()
	var namespaces corev1.NamespaceList
	Expect(k8sClient.List(ctx, &namespaces)).To(Succeed())
	for _, namespace := range namespaces.Items {
		Expect(k8sClient.DeleteAllOf(ctx, &lookupv1beta1.ClientConfig{}, client.InNamespace(namespace.Name))).
			To(Succeed())
	}
	err := k8sClient.Delete(ctx, &corev1.Secret{ObjectMeta: objectMeta(serviceNamespace, tokens.SecretName)})
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
