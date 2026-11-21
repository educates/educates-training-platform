// The composition suite starts the secrets-manager as its binary composes it,
// against an envtest API server with the four kinds loaded from the
// secrets-manager subchart's CRDs, and checks what the scenario suite cannot
// see from the Kubernetes API alone: the probes and leader election.
package operator_test

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/educates/educates-training-platform/secrets-manager/internal/operator"
)

// chartCRDs is the crds/ directory of the secrets-manager subchart, which
// holds the schema of record of the four kinds.
var chartCRDs = filepath.Join("..", "..", "..", "installer", "charts", "educates-training-platform", "charts",
	"secrets-manager", "crds")

var (
	testEnv    *envtest.Environment
	restConfig *rest.Config
	k8sClient  client.Client
)

func TestComposition(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Secrets Manager Composition Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	testEnv = &envtest.Environment{CRDDirectoryPaths: []string{chartCRDs}, ErrorIfCRDPathMissing: true}
	if dir := firstEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}
	var err error
	restConfig, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())

	scheme := runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	k8sClient, err = client.New(restConfig, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	if testEnv != nil {
		Expect(testEnv.Stop()).To(Succeed())
	}
})

// secretsManager is a secrets-manager composed and run in this process.
type secretsManager struct {
	cancel   context.CancelFunc
	stopped  chan error
	probeURL string
}

// runSecretsManager composes the secrets-manager against the envtest API
// server, serving its probes on a port of its own, and runs it until the
// spec ends. Each option changes what it is composed with.
func runSecretsManager(options ...func(*operator.Options)) *secretsManager {
	GinkgoHelper()
	probeListener, err := net.Listen("tcp", "127.0.0.1:0")
	Expect(err).NotTo(HaveOccurred())

	opts := operator.Options{
		RESTConfig:    restConfig,
		Logger:        logf.Log.WithName("secrets-manager"),
		ProbeListener: probeListener,
	}
	for _, option := range options {
		option(&opts)
	}

	ctx, cancel := context.WithCancel(context.Background())
	composed, err := operator.New(ctx, opts)
	if err != nil {
		cancel()
		Expect(probeListener.Close()).To(Succeed())
	}
	Expect(err).NotTo(HaveOccurred())

	sm := &secretsManager{cancel: cancel, stopped: make(chan error, 1),
		probeURL: "http://" + probeListener.Addr().String()}
	go func() { sm.stopped <- composed.Run(ctx) }()
	DeferCleanup(sm.stop)
	return sm
}

// stop stops the secrets-manager, when it runs, and waits until it has
// stopped.
func (s *secretsManager) stop() {
	if s.cancel == nil {
		return
	}
	s.cancel()
	s.cancel = nil
	Eventually(s.stopped).WithTimeout(time.Minute).Should(Receive())
}

// probe calls a probe and returns its status, or 0 when it does not answer.
func (s *secretsManager) probe(path string) int {
	response, err := http.Get(s.probeURL + path)
	if err != nil {
		return 0
	}
	defer func() { _ = response.Body.Close() }()
	_, _ = io.Copy(io.Discard, response.Body)
	return response.StatusCode
}

// ready reports whether the readiness probe answers 200.
func (s *secretsManager) ready() bool {
	return s.probe("/readyz") == http.StatusOK
}

// gatedAPIServer is a proxy to the envtest API server that holds back the
// requests whose path contains a given string until it is opened.
type gatedAPIServer struct {
	server   *httptest.Server
	config   *rest.Config
	once     sync.Once
	released chan struct{}
}

// newGatedAPIServer returns a proxy to the envtest API server, shut for the
// requests whose path contains gated, and closed when the spec ends.
func newGatedAPIServer(gated string) *gatedAPIServer {
	GinkgoHelper()
	transport, err := rest.TransportFor(restConfig)
	Expect(err).NotTo(HaveOccurred())
	targetURL, err := url.Parse(restConfig.Host)
	Expect(err).NotTo(HaveOccurred())

	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = transport
	proxy.FlushInterval = -1

	g := &gatedAPIServer{released: make(chan struct{})}
	g.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, gated) {
			select {
			case <-g.released:
			case <-r.Context().Done():
				return
			}
		}
		proxy.ServeHTTP(w, r)
	}))
	g.config = &rest.Config{Host: g.server.URL}
	DeferCleanup(g.close)
	return g
}

// open lets the held back requests through, and every later one.
func (g *gatedAPIServer) open() {
	g.once.Do(func() { close(g.released) })
}

func (g *gatedAPIServer) close() {
	g.open()
	g.server.CloseClientConnections()
	g.server.Close()
}

// firstEnvTestBinaryDir finds the envtest binaries make downloads, for a run
// without KUBEBUILDER_ASSETS set, such as from an editor.
func firstEnvTestBinaryDir() string {
	base := filepath.Join("..", "..", "bin", "k8s")
	entries, err := os.ReadDir(base)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() {
			return filepath.Join(base, entry.Name())
		}
	}
	return ""
}
