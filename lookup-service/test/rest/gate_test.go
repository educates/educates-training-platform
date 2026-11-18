package rest

import (
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/client-go/rest"
)

// gatedAPIServer is a proxy in front of an API server that holds back every
// request whose path contains a gated part, such as the list and watch of a
// resource, until it is opened. It lets a spec keep a cache from syncing.
type gatedAPIServer struct {
	// config reaches the API server through the proxy.
	config *rest.Config

	server   *httptest.Server
	released chan struct{}
	once     sync.Once
}

func newGatedAPIServer(target *rest.Config, gated string) *gatedAPIServer {
	GinkgoHelper()
	transport, err := rest.TransportFor(target)
	Expect(err).NotTo(HaveOccurred())
	targetURL, err := url.Parse(target.Host)
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
