package cmd

import (
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"testing"

	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	clientcmdapi "k8s.io/client-go/tools/clientcmd/api"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// apiServerDataHome is a data home whose router address is 10.10.10.1,
// with the default local cluster on educates.test and the local cluster
// test2 on test2.educates.test.
func apiServerDataHome(t *testing.T) string {
	t.Helper()
	dataHome := routerDataHome(t)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "test2", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ningress:\n  domain: test2.educates.test\n")
	return dataHome
}

func TestLookupKubeconfigServerOfAKubeContext(t *testing.T) {
	dataHome := apiServerDataHome(t)

	cases := []struct {
		name          string
		kubeContext   string
		contextConfig *rest.Config
		want          kubeconfigServer
	}{
		{
			name:          "the default local cluster",
			kubeContext:   "kind-educates",
			contextConfig: &rest.Config{Host: "https://127.0.0.1:59223"},
			want:          kubeconfigServer{Server: "https://10.10.10.1:6443", TLSServerName: "educates-control-plane"},
		},
		{
			name:          "a named local cluster",
			kubeContext:   "kind-educates-test2",
			contextConfig: &rest.Config{Host: "https://127.0.0.1:59224"},
			want:          kubeconfigServer{Server: "https://10.10.10.1:6443", TLSServerName: "educates-test2-control-plane"},
		},
		{
			name:          "a kind cluster named like a local cluster the data home has no folder for",
			kubeContext:   "kind-educates-other",
			contextConfig: &rest.Config{Host: "https://127.0.0.1:59225"},
			want:          kubeconfigServer{Server: "https://127.0.0.1:59225"},
		},
		{
			name:          "a cluster that is no local cluster",
			kubeContext:   "gke_project_region_cluster-1",
			contextConfig: gatewayContext,
			want:          kubeconfigServer{Server: "https://gateway.example.com", TLSServerName: "api.cluster-1.internal"},
		},
		{
			name:          "no kube context, as in a pod",
			kubeContext:   "",
			contextConfig: &rest.Config{Host: "https://10.96.0.1:443"},
			want:          kubeconfigServer{Server: "https://10.96.0.1:443"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := lookupKubeconfigServer("", tc.kubeContext, tc.contextConfig, dataHome)
			if err != nil {
				t.Fatalf("lookupKubeconfigServer: %v", err)
			}
			if got != tc.want {
				t.Errorf("lookupKubeconfigServer(%q) = %+v, want %+v", tc.kubeContext, got, tc.want)
			}
		})
	}
}

// A local cluster whose names the router does not serve, as one with no
// config in the data home, or with no router file to give the router
// address, falls back to its address on Docker's kind network, which only
// the pods of the other local clusters reach, and the note says so.
func TestLookupKubeconfigServerOfALocalClusterTheRouterDoesNotServe(t *testing.T) {
	noConfig := routerDataHome(t)
	test2, err := localclusters.Named("test2", noConfig)
	if err != nil {
		t.Fatalf("Named: %v", err)
	}
	if err := localclusters.MakeClusterFolder(test2); err != nil {
		t.Fatalf("MakeClusterFolder: %v", err)
	}

	cases := []struct {
		name, kubeContext, dataHome, want string
	}{
		{name: "no config", kubeContext: "kind-educates-test2", dataHome: noConfig,
			want: "https://educates-test2-control-plane:6443"},
		{name: "no router file", kubeContext: "kind-educates", dataHome: t.TempDir(),
			want: "https://educates-control-plane:6443"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := lookupKubeconfigServer("", tc.kubeContext, &rest.Config{Host: "https://127.0.0.1:59223"}, tc.dataHome)
			if err != nil {
				t.Fatalf("lookupKubeconfigServer: %v", err)
			}
			if got.Server != tc.want || got.TLSServerName != "" {
				t.Errorf("lookupKubeconfigServer gives server %q and tls-server-name %q, want %q and none",
					got.Server, got.TLSServerName, tc.want)
			}
			for _, part := range []string{tc.want, "kind network", "only the pods of the other local clusters", "router"} {
				if !strings.Contains(got.Note, part) {
					t.Errorf("note %q does not mention %q", got.Note, part)
				}
			}
		})
	}
}

func TestLookupKubeconfigServerGivenWithServer(t *testing.T) {
	dataHome := apiServerDataHome(t)

	for _, kubeContext := range []string{"kind-educates", "gke_project_region_cluster-1"} {
		t.Run(kubeContext, func(t *testing.T) {
			got, err := lookupKubeconfigServer("https://api.cluster-1.example.com:6443", kubeContext, gatewayContext, dataHome)
			if err != nil {
				t.Fatalf("lookupKubeconfigServer: %v", err)
			}
			if want := (kubeconfigServer{Server: "https://api.cluster-1.example.com:6443"}); got != want {
				t.Errorf("lookupKubeconfigServer = %+v, want %+v", got, want)
			}
		})
	}
}

func TestLookupKubeconfigServerRefusesAServerThatIsNoHTTPSAddress(t *testing.T) {
	dataHome := t.TempDir()

	for _, server := range []string{
		"http://api.cluster-1.example.com:6443",
		"api.cluster-1.example.com:6443",
		"https://",
		"https://api.cluster-1.example.com:6443/%zz",
	} {
		t.Run(server, func(t *testing.T) {
			_, err := lookupKubeconfigServer(server, "kind-educates", &rest.Config{Host: "https://127.0.0.1:59223"}, dataHome)
			if err == nil {
				t.Fatalf("lookupKubeconfigServer(%q) gave no error", server)
			}
		})
	}
}

// gatewayContext is the client configuration of a kube context that reaches
// its API server through a gateway, verifying the API server's certificate
// against a name other than the gateway's.
var gatewayContext = &rest.Config{
	Host:            "https://gateway.example.com",
	TLSClientConfig: rest.TLSClientConfig{ServerName: "api.cluster-1.internal"},
}

func TestLookupKubeconfigClusterCopiesTheTLSServerNameOfTheKubeContextsOwnServer(t *testing.T) {
	o := &LookupConfigOptions{}
	got, _, err := o.kubeconfigCluster("gke_project_region_cluster-1", gatewayContext, t.TempDir())
	if err != nil {
		t.Fatalf("kubeconfigCluster: %v", err)
	}
	if got.Server != "https://gateway.example.com" || got.TLSServerName != "api.cluster-1.internal" {
		t.Errorf("kubeconfigCluster gives server %q and tls-server-name %q, want %q and %q",
			got.Server, got.TLSServerName, "https://gateway.example.com", "api.cluster-1.internal")
	}
}

func TestLookupKubeconfigClusterLeavesOutTheKubeContextsTLSServerNameWithServer(t *testing.T) {
	cases := []struct {
		name        string
		server      string
		kubeContext string
		wantServer  string
	}{
		{name: "an address --server gives", server: "https://api.cluster-1.example.com:6443",
			kubeContext: "gke_project_region_cluster-1", wantServer: "https://api.cluster-1.example.com:6443"},
		{name: "the kube context's own address --server gives", server: "https://gateway.example.com",
			kubeContext: "gke_project_region_cluster-1", wantServer: "https://gateway.example.com"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &LookupConfigOptions{Server: tc.server}
			got, _, err := o.kubeconfigCluster(tc.kubeContext, gatewayContext, t.TempDir())
			if err != nil {
				t.Fatalf("kubeconfigCluster: %v", err)
			}
			if got.Server != tc.wantServer || got.TLSServerName != "" {
				t.Errorf("kubeconfigCluster gives server %q and tls-server-name %q, want %q and none",
					got.Server, got.TLSServerName, tc.wantServer)
			}
		})
	}
}

// The kubeconfig of a local cluster names the router's address for its
// API server, with its control-plane node as tls-server-name, and needs no
// note.
func TestLookupKubeconfigClusterNamesTheRoutersAddressForALocalCluster(t *testing.T) {
	o := &LookupConfigOptions{}
	got, note, err := o.kubeconfigCluster("kind-educates-test2", &rest.Config{Host: "https://127.0.0.1:59224"},
		apiServerDataHome(t))
	if err != nil {
		t.Fatalf("kubeconfigCluster: %v", err)
	}
	if got.Server != "https://10.10.10.1:6443" || got.TLSServerName != "educates-test2-control-plane" || note != "" {
		t.Errorf("kubeconfigCluster gives server %q, tls-server-name %q and note %q, want %q, %q and none",
			got.Server, got.TLSServerName, note, "https://10.10.10.1:6443", "educates-test2-control-plane")
	}
}

func TestLookupKubeconfigClusterGivesTheTLSServerNameOfTheFlag(t *testing.T) {
	cases := []struct {
		name        string
		server      string
		kubeContext string
	}{
		{name: "the kube context's own server", kubeContext: "gke_project_region_cluster-1"},
		{name: "an address --server gives", server: "https://api.cluster-1.example.com:6443",
			kubeContext: "gke_project_region_cluster-1"},
		{name: "the address of a local cluster", kubeContext: "kind-educates"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := &LookupConfigOptions{Server: tc.server, TLSServerName: "api.cluster-1.example.com"}
			got, _, err := o.kubeconfigCluster(tc.kubeContext, gatewayContext, apiServerDataHome(t))
			if err != nil {
				t.Fatalf("kubeconfigCluster: %v", err)
			}
			if got.TLSServerName != "api.cluster-1.example.com" {
				t.Errorf("kubeconfigCluster gives tls-server-name %q, want %q",
					got.TLSServerName, "api.cluster-1.example.com")
			}
		})
	}
}

func TestRemoteAccessKubeconfigGivesTheClusterEntryWithTheSecretsCA(t *testing.T) {
	kubeconfig, err := remoteAccessKubeconfig(
		&clientcmdapi.Cluster{Server: "https://gateway.example.com", TLSServerName: "api.cluster-1.internal",
			ProxyURL: "socks5://proxy.example.com:1080"},
		remoteAccessCredentials{CACertificate: []byte("ca certificate"), Token: []byte("token")})
	if err != nil {
		t.Fatalf("remoteAccessKubeconfig: %v", err)
	}
	config, err := clientcmd.Load(kubeconfig)
	if err != nil {
		t.Fatalf("loading the kubeconfig: %v", err)
	}
	cluster, ok := config.Clusters["training-platform"]
	if !ok {
		t.Fatalf("the kubeconfig has no cluster training-platform")
	}
	if cluster.Server != "https://gateway.example.com" || cluster.TLSServerName != "api.cluster-1.internal" ||
		cluster.ProxyURL != "socks5://proxy.example.com:1080" ||
		string(cluster.CertificateAuthorityData) != "ca certificate" {
		t.Errorf("the kubeconfig's cluster is %+v", cluster)
	}
	if user, ok := config.AuthInfos["remote-access"]; !ok || user.Token != "token" {
		t.Errorf("the kubeconfig's user remote-access is %+v", user)
	}
}

// proxiedContext is the client configuration of a kube context that reaches
// its API server through a SOCKS proxy on the loopback address.
var proxiedContext = &rest.Config{
	Host:  "https://34.1.2.3",
	Proxy: http.ProxyURL(&url.URL{Scheme: "socks5", Host: "localhost:1080"}),
}

func TestLookupKubeconfigClusterGivesTheProxyURLOfTheFlag(t *testing.T) {
	for _, proxyURL := range []string{
		"http://proxy.example.com:3128",
		"https://proxy.example.com",
		"socks5://user:password@proxy.example.com:1080",
	} {
		t.Run(proxyURL, func(t *testing.T) {
			o := &LookupConfigOptions{ProxyURL: proxyURL}
			got, note, err := o.kubeconfigCluster("gke_project_region_cluster-1", proxiedContext, t.TempDir())
			if err != nil {
				t.Fatalf("kubeconfigCluster: %v", err)
			}
			if got.ProxyURL != proxyURL {
				t.Errorf("kubeconfigCluster gives proxy-url %q, want %q", got.ProxyURL, proxyURL)
			}
			if note != "" {
				t.Errorf("kubeconfigCluster gives the note %q, want none", note)
			}
		})
	}
}

func TestLookupKubeconfigClusterRefusesAProxyURLThatIsNoHTTPHTTPSOrSOCKS5Address(t *testing.T) {
	for _, proxyURL := range []string{
		"ftp://proxy.example.com",
		"proxy.example.com:3128",
		"http://",
		"http://proxy.example.com/%zz",
	} {
		t.Run(proxyURL, func(t *testing.T) {
			o := &LookupConfigOptions{ProxyURL: proxyURL}
			if _, _, err := o.kubeconfigCluster("gke_project_region_cluster-1", proxiedContext, t.TempDir()); err == nil {
				t.Fatalf("kubeconfigCluster with --proxy-url %q gave no error", proxyURL)
			}
		})
	}
}

func TestLookupKubeconfigClusterNotesTheKubeContextsProxyItLeavesOut(t *testing.T) {
	o := &LookupConfigOptions{}
	got, note, err := o.kubeconfigCluster("gke_project_region_cluster-1", proxiedContext, t.TempDir())
	if err != nil {
		t.Fatalf("kubeconfigCluster: %v", err)
	}
	if got.ProxyURL != "" {
		t.Errorf("kubeconfigCluster gives proxy-url %q, want none", got.ProxyURL)
	}
	if !strings.Contains(note, "--proxy-url") {
		t.Errorf("kubeconfigCluster gives the note %q, want one naming --proxy-url", note)
	}

	_, note, err = o.kubeconfigCluster("gke_project_region_cluster-1", gatewayContext, t.TempDir())
	if err != nil {
		t.Fatalf("kubeconfigCluster: %v", err)
	}
	if note != "" {
		t.Errorf("kubeconfigCluster gives the note %q for a kube context without a proxy, want none", note)
	}
}
