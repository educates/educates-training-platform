// Package rest is the REST suite of the Lookup Service: it runs the service's
// real composition in process against envtest and calls the REST API as a
// client does, asserting the golden responses captured from the Python
// service under testdata/golden.
package rest

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	lookupv1beta1 "github.com/educates/educates-training-platform/lookup-service/api/v1beta1"
	"github.com/educates/educates-training-platform/lookup-service/internal/service"
	"github.com/educates/educates-training-platform/lookup-service/test/golden"
)

const (
	// configNamespace is the namespace the service reads its configuration
	// from when the environment names no other.
	configNamespace = "educates-config"

	// serviceNamespace is the namespace the service runs in.
	serviceNamespace = "educates"
)

// servedRoutes are the routes of the REST API the Go service serves. Every
// fixture whose request one of them serves must be replayed by a spec: the
// suite fails otherwise.
var servedRoutes = []string{
	"POST /auth/login",
	"POST /login",
	"POST /auth/logout",
	"GET /auth/verify",
	"GET /api/v1/clients",
	"GET /api/v1/clients/{client}",
}

// unroutedFixtures are the fixtures of requests that no route serves, which
// the suite must replay too.
var unroutedFixtures = []string{
	"unknown-route",
	"auth-login-wrong-method",
}

var (
	testEnv    *envtest.Environment
	restConfig *rest.Config
	k8sClient  client.Client
	scheme     *runtime.Scheme
	fixtures   *golden.Fixtures
)

func TestREST(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Lookup Service REST Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	// The service learns its namespaces from the environment, as in a pod.
	// The specs that need another config namespace set it themselves.
	Expect(os.Unsetenv(service.ConfigNamespaceEnv)).To(Succeed())
	Expect(os.Setenv(service.ServiceNamespaceEnv, serviceNamespace)).To(Succeed())

	scheme = runtime.NewScheme()
	Expect(clientgoscheme.AddToScheme(scheme)).To(Succeed())
	Expect(lookupv1beta1.AddToScheme(scheme)).To(Succeed())

	By("starting the lookup cluster's API server")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths: []string{
			filepath.Join("..", "..", "..", "installer", "charts", "educates-training-platform",
				"charts", "lookup-service", "crds"),
		},
		ErrorIfCRDPathMissing: true,
	}
	if dir := firstEnvTestBinaryDir(); dir != "" {
		testEnv.BinaryAssetsDirectory = dir
	}
	var err error
	restConfig, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())

	k8sClient, err = client.New(restConfig, client.Options{Scheme: scheme})
	Expect(err).NotTo(HaveOccurred())

	for _, name := range []string{configNamespace, serviceNamespace} {
		Expect(k8sClient.Create(context.Background(), &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: name},
		})).To(Succeed())
	}

	fixtures, err = golden.Load(filepath.Join("..", "..", "testdata", "golden"))
	Expect(err).NotTo(HaveOccurred())
})

var _ = AfterSuite(func() {
	By("stopping the lookup cluster's API server")
	Eventually(testEnv.Stop, time.Minute, time.Second).Should(Succeed())

	By("checking that every golden response of the served routes was asserted")
	if suiteRanEverySpec() {
		missing := fixtures.NotReplayed(func(f *golden.Fixture) bool {
			return f.ServedBy(servedRoutes) || slices.Contains(unroutedFixtures, f.Slug())
		})
		Expect(missing).To(BeEmpty(), "golden responses of served routes that no spec replays")
	}
})

// suiteRanEverySpec reports whether the run selected every spec, without a
// focus, skip or label filter.
func suiteRanEverySpec() bool {
	config, _ := GinkgoConfiguration()
	return len(config.FocusStrings) == 0 && len(config.SkipStrings) == 0 &&
		len(config.FocusFiles) == 0 && len(config.SkipFiles) == 0 && config.LabelFilter == ""
}

// firstEnvTestBinaryDir finds the envtest binaries make setup-envtest
// downloads, so that the suite also runs outside make, as from an IDE.
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
