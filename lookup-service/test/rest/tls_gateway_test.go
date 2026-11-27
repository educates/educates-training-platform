package rest

import (
	"crypto/tls"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/rest"
	certutil "k8s.io/client-go/util/cert"
)

// tlsGateway is a proxy in front of an API server that serves TLS with a
// certificate of its own, which names one server name and no address, as a
// gateway routing several clusters by server name does. A client reaching
// it at its address verifies its certificate only when it sends that name.
type tlsGateway struct {
	// url is the gateway's address, which its certificate does not name.
	url string
	// serverName is the one name its certificate names.
	serverName string
	// caData is the PEM encoded CA certificate its certificate is verified
	// against.
	caData []byte

	server *httptest.Server
}

// newTLSGateway returns a gateway to target whose certificate names
// serverName. It verifies target with target's CA alone, so that the API
// server authenticates the token of each request it passes on.
func newTLSGateway(target *rest.Config, serverName string) *tlsGateway {
	GinkgoHelper()
	certPEM, keyPEM, err := certutil.GenerateSelfSignedCertKey(serverName, nil, nil)
	Expect(err).NotTo(HaveOccurred())
	certificate, err := tls.X509KeyPair(certPEM, keyPEM)
	Expect(err).NotTo(HaveOccurred())
	// The PEM holds the gateway's certificate, then the CA that signed it.
	chain, err := certutil.ParseCertsPEM(certPEM)
	Expect(err).NotTo(HaveOccurred())
	Expect(chain).To(HaveLen(2))
	caData, err := certutil.EncodeCertificates(chain[1])
	Expect(err).NotTo(HaveOccurred())

	transport, err := rest.TransportFor(&rest.Config{
		Host:            target.Host,
		TLSClientConfig: rest.TLSClientConfig{CAData: target.CAData},
	})
	Expect(err).NotTo(HaveOccurred())
	targetURL, err := url.Parse(target.Host)
	Expect(err).NotTo(HaveOccurred())
	proxy := httputil.NewSingleHostReverseProxy(targetURL)
	proxy.Transport = transport
	proxy.FlushInterval = -1

	server := httptest.NewUnstartedServer(proxy)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{certificate}}
	server.StartTLS()
	return &tlsGateway{url: server.URL, serverName: serverName, caData: caData, server: server}
}

// kubeconfig returns a kubeconfig reaching cluster through the gateway as
// the remote-access identity, which gives no tls-server-name.
func (g *tlsGateway) kubeconfig(cluster *testCluster) string {
	return cluster.kubeconfigReaching(kubeconfigCluster{server: g.url, ca: g.caData})
}

// kubeconfigWithServerName returns a kubeconfig reaching cluster through
// the gateway as the remote-access identity, which gives the name the
// gateway's certificate names as its cluster's tls-server-name.
func (g *tlsGateway) kubeconfigWithServerName(cluster *testCluster) string {
	return cluster.kubeconfigReaching(kubeconfigCluster{server: g.url, ca: g.caData, tlsServerName: g.serverName})
}

func (g *tlsGateway) close() {
	g.server.CloseClientConnections()
	g.server.Close()
}
