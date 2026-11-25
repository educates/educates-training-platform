package localclusters

import (
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// A cluster whose TLS ends at the router keeps https on its derived
// nip.io domain: the domain does not make it insecure, which the loader
// would refuse beside externalTLSTermination.
func TestLoadConfigOfAClusterEndingTLSAtTheRouterKeepsHTTPSOnItsNipDomain(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.1\n"+
		"ingress:\n  externalTLSTermination: true\n")

	loaded, err := LoadConfig(Default(dataHome))
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if got, want := loaded.Domain(), "10-10-10-1.nip.io"; got != want {
		t.Errorf("Domain() = %q, want %q", got, want)
	}
	c := loaded.Config.(*v1alpha1.EducatesLocalConfig)
	if c.Ingress.Insecure {
		t.Errorf("ingress.insecure = true, want false for a cluster whose TLS ends at the router")
	}
	if !c.Ingress.ExternalTLSTermination {
		t.Errorf("ingress.externalTLSTermination = false, want true as declared")
	}
}

// endingTLS is a route of the named local cluster name of dataHome on
// domain, with extra domains, whose TLS ends at the router.
func endingTLS(t *testing.T, dataHome, name, domain string, extra ...string) RouterRoute {
	t.Helper()
	id := namedID(t, name, dataHome)
	return RouterRoute{
		Cluster:         id,
		Names:           append([]string{domain}, extra...),
		Node:            id.ControlPlaneNode(),
		State:           Running,
		TLSEndsAtRouter: true,
	}
}

// routerCertificateFor is a certificate, with its key, for domain, as the
// local certificates module gives one.
func routerCertificateFor(domain string) RouterCertificate {
	return RouterCertificate{
		Domain:      domain,
		Certificate: []byte("certificate of " + domain + "\n"),
		Key:         []byte("key of " + domain + "\n"),
	}
}

// routerFileNamed returns the router file name among files, and whether
// there is one.
func routerFileNamed(files []RouterFile, name string) (RouterFile, bool) {
	for _, f := range files {
		if f.Name == name {
			return f, true
		}
	}
	return RouterFile{}, false
}

func TestRouterRoutesEndTLSAtTheRouterForAClusterThatAsksForIt(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.1\n"+
		"ingress:\n  domain: educates.test\n")
	writeClusterConfig(t, dataHome, "tls1", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.1\n"+
		"ingress:\n  domain: tls1.test\n  externalTLSTermination: true\n")

	routes := routerRoutes(t, dataHome, []NodeContainer{kindControlPlane("educates", true), kindControlPlane("educates-tls1", true)})

	ends := map[string]bool{}
	for _, r := range routes {
		ends[r.Cluster.KindName] = r.TLSEndsAtRouter
	}
	if want := map[string]bool{"educates": false, "educates-tls1": true}; !reflect.DeepEqual(ends, want) {
		t.Errorf("routes ending TLS at the router: %v, want %v", ends, want)
	}
	if got, want := RouterCertificateDomains(routes), []string{"tls1.test"}; !slices.Equal(got, want) {
		t.Errorf("RouterCertificateDomains() = %q, want %q", got, want)
	}
}

// Port 443 sends a cluster whose TLS ends at the router to an internal
// frontend that ends TLS with the certificate of its domain and forwards
// plain HTTP to the node's port 80 by its port 80 backend, keeping Host
// and adding X-Forwarded-Proto and X-Forwarded-For, beside a cluster whose
// TLS passes through as before.
func TestRenderRouterEndsTLSForOneClusterBesideAnotherPassingItThrough(t *testing.T) {
	dataHome := t.TempDir()
	passthrough := RouterRoute{Cluster: Default(dataHome), Names: []string{"educates.test"}, Node: "educates-control-plane", State: Running}
	ended := endingTLS(t, dataHome, "tls1", "TLS1.test", "extra.test")

	files := RenderRouter([]RouterRoute{passthrough, ended}, routerCertificateFor("TLS1.test"))

	for header, want := range map[string]string{
		"backend tls_educates": "backend tls_educates\n    mode tcp\n    server node educates-control-plane:443\n",
		"backend tls_educates-tls1": `backend tls_educates-tls1
    mode tcp
    # TLS ends at the router for this cluster's domain: frontend tls_ended
    # presents its certificate. A name under one of its extra domains, which
    # the certificate does not cover, is closed, as an unknown name is.
    tcp-request content reject unless { str(.),concat(,sess.sni,) -m end .tls1.test }
    server tls_ended abns@educates-tls-ended send-proxy-v2
`,
		"frontend tls_ended": `frontend tls_ended
    mode http
    bind abns@educates-tls-ended accept-proxy ssl crt /usr/local/etc/haproxy/certs/ strict-sni alpn http/1.1
    option forwardfor
    http-request set-header X-Forwarded-Proto https
    http-request set-var(txn.sni) ssl_fc_sni,lower
    http-request set-var(txn.cluster) str(.),concat(,txn.sni,),map_end(/usr/local/etc/haproxy/hosts.map)
    use_backend http_%[var(txn.cluster)]
`,
		"backend http_educates-tls1": `backend http_educates-tls1
    mode http
    http-error status 503 content-type "text/plain; charset=utf-8" file /usr/local/etc/haproxy/503-educates-tls1.txt
    server node educates-tls1-control-plane:80
`,
	} {
		if got := configSection(t, files, header); got != want {
			t.Errorf("section:\n%s\nwant:\n%s", got, want)
		}
	}
	wantEntries(t, mapEntries(t, files), ".educates.test educates", ".extra.test educates-tls1", ".tls1.test educates-tls1")

	// The certificate takes the folder's default mode, readable by every
	// user, and its key is readable only by the user.
	for name, want := range map[string]struct {
		content string
		mode    fs.FileMode
	}{
		"certs/tls1.test.pem":     {"certificate of TLS1.test\n", 0},
		"certs/tls1.test.pem.key": {"key of TLS1.test\n", 0o600},
	} {
		f, ok := routerFileNamed(files, name)
		if !ok {
			t.Errorf("no router file %s", name)
			continue
		}
		if string(f.Content) != want.content || f.Mode != want.mode {
			t.Errorf("%s = %q with mode %v, want %q with mode %v", name, f.Content, f.Mode, want.content, want.mode)
		}
	}
}

// With no cluster ending TLS at the router, the router holds no
// certificate and ends TLS for nothing.
func TestRenderRouterWithNoClusterEndingTLSHasNoCertificate(t *testing.T) {
	files := RenderRouter([]RouterRoute{{Cluster: Default(t.TempDir()), Names: []string{"educates.test"}, Node: "educates-control-plane"}},
		routerCertificateFor("educates.test"))

	if cfg := renderedFile(t, files, "haproxy.cfg"); strings.Contains(cfg, "tls_ended") || strings.Contains(cfg, " crt ") {
		t.Errorf("haproxy.cfg ends TLS with no cluster asking for it:\n%s", cfg)
	}
	for _, f := range files {
		if strings.HasPrefix(f.Name, "certs/") {
			t.Errorf("router file %s rendered with no cluster ending TLS at the router", f.Name)
		}
	}
}

// A cluster whose TLS ends at the router, but whose certificate the router
// could not get, has every TLS connection closed, and nothing else ends
// TLS.
func TestRenderRouterClosesTLSForAClusterWithoutItsCertificate(t *testing.T) {
	dataHome := t.TempDir()

	files := RenderRouter([]RouterRoute{endingTLS(t, dataHome, "tls1", "tls1.test")})

	want := `backend tls_educates-tls1
    mode tcp
    # TLS ends at the router for this cluster's domain, but the router has
    # no certificate for it: every connection is closed.
    tcp-request content reject
`
	if got := configSection(t, files, "backend tls_educates-tls1"); got != want {
		t.Errorf("backend:\n%s\nwant:\n%s", got, want)
	}
	if cfg := renderedFile(t, files, "haproxy.cfg"); strings.Contains(cfg, "frontend tls_ended") {
		t.Errorf("haproxy.cfg ends TLS with no certificate:\n%s", cfg)
	}
}

// The router's certificate and key are written with the router folder,
// the key readable only by the user, and read back from it.
func TestWriteRouterFolderKeepsTheCertificateKeyReadableOnlyByTheUser(t *testing.T) {
	dataHome := t.TempDir()
	cert := routerCertificateFor("tls1.test")

	if err := WriteRouterFolder(dataHome, RenderRouter([]RouterRoute{endingTLS(t, dataHome, "tls1", "tls1.test")}, cert)); err != nil {
		t.Fatalf("WriteRouterFolder: %v", err)
	}

	for name, want := range map[string]fs.FileMode{"certs/tls1.test.pem": 0o644, "certs/tls1.test.pem.key": 0o600} {
		info, err := os.Stat(filepath.Join(config.LocalRouterDir(dataHome), filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != want {
			t.Errorf("%s has mode %v, want %v", name, mode, want)
		}
	}
	got, err := ReadRouterCertificate(dataHome, "TLS1.test")
	if err != nil {
		t.Fatalf("ReadRouterCertificate: %v", err)
	}
	if want := (RouterCertificate{Domain: "TLS1.test", Certificate: cert.Certificate, Key: cert.Key}); got == nil || !reflect.DeepEqual(*got, want) {
		t.Errorf("ReadRouterCertificate() = %+v, want %+v", got, want)
	}
}

// A cluster deleted takes its certificate and key out of the router
// folder, and reading them back gives none.
func TestWriteRouterFolderRemovesTheCertificateOfADeletedCluster(t *testing.T) {
	dataHome := t.TempDir()
	if err := WriteRouterFolder(dataHome, RenderRouter([]RouterRoute{endingTLS(t, dataHome, "tls1", "tls1.test")},
		routerCertificateFor("tls1.test"))); err != nil {
		t.Fatalf("WriteRouterFolder: %v", err)
	}

	if err := WriteRouterFolder(dataHome, RenderRouter(nil)); err != nil {
		t.Fatalf("WriteRouterFolder: %v", err)
	}

	for name := range folderFiles(t, config.LocalRouterDir(dataHome)) {
		if strings.HasPrefix(name, "certs/") {
			t.Errorf("router folder still holds %s", name)
		}
	}
	if got, err := ReadRouterCertificate(dataHome, "tls1.test"); err != nil || got != nil {
		t.Errorf("ReadRouterCertificate() = %v, %v; want none", got, err)
	}
}

// local router status shows a cluster's domain with its TLS ended at the
// router, and its extra domains, which the router serves on port 80 only,
// with no TLS.
func TestRouterStatusShowsTLSEndedAtTheRouterForTheDomainOnly(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.1\n"+
		"ingress:\n  domain: educates.test\n")
	writeClusterConfig(t, dataHome, "tls1", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.1\n"+
		"ingress:\n  domain: tls1.test\n  externalTLSTermination: true\nresolver:\n  extraDomains:\n  - extra.test\n")
	inv := NewInventory(Observed{
		Nodes:              []NodeContainer{kindControlPlane("educates", true), kindControlPlane("educates-tls1", false)},
		InterfaceAddresses: hostInterfaces,
	}, dataHome)

	status, err := inv.RouterStatus(pinnedRouterImage)
	if err != nil {
		t.Fatalf("RouterStatus: %v", err)
	}

	wantStatusRows(t, status.Routes,
		statusRowSummary{"educates.test", "educates", RouterTLSPassthrough, Running},
		statusRowSummary{"tls1.test", "educates-tls1", RouterTLSTerminated, Stopped},
		statusRowSummary{"extra.test", "educates-tls1", RouterTLSNone, Stopped},
	)
}
