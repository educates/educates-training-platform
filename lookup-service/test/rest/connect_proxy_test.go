package rest

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
)

// connectProxy is an HTTP proxy that tunnels the CONNECT requests for one
// address, which nothing else resolves, to another, and refuses every other
// request. It stands for a proxy a cluster is reached through, at a name
// only the proxy knows.
type connectProxy struct {
	// url is the proxy's URL.
	url string

	server *httptest.Server

	mu sync.Mutex
	// tunnelled are the addresses of the CONNECT requests it tunnelled.
	tunnelled []string
	// connections are the two ends of every tunnel it opened, which the
	// server no longer tracks once they are hijacked.
	connections []net.Conn
}

// newConnectProxy returns a proxy that tunnels the CONNECT requests for
// address to target, both as host:port.
func newConnectProxy(address, target string) *connectProxy {
	p := &connectProxy{}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect || r.Host != address {
			http.Error(w, "the proxy tunnels "+address+" only", http.StatusForbidden)
			return
		}
		upstream, err := net.Dial("tcp", target)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		downstream, buffered, err := http.NewResponseController(w).Hijack()
		if err != nil {
			_ = upstream.Close()
			return
		}
		if _, err := io.WriteString(downstream, "HTTP/1.1 200 Connection established\r\n\r\n"); err != nil {
			_ = upstream.Close()
			_ = downstream.Close()
			return
		}
		p.mu.Lock()
		p.tunnelled = append(p.tunnelled, r.Host)
		p.connections = append(p.connections, upstream, downstream)
		p.mu.Unlock()
		go tunnel(upstream, buffered, upstream, downstream)
		go tunnel(downstream, upstream, upstream, downstream)
	}))
	p.url = p.server.URL
	return p
}

// tunnel copies from to to until either end closes, then closes both ends.
func tunnel(to io.Writer, from io.Reader, ends ...net.Conn) {
	_, _ = io.Copy(to, from)
	for _, end := range ends {
		_ = end.Close()
	}
}

// tunnelledAddresses returns the addresses of the CONNECT requests the
// proxy tunnelled.
func (p *connectProxy) tunnelledAddresses() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.tunnelled)
}

func (p *connectProxy) close() {
	p.mu.Lock()
	for _, connection := range p.connections {
		_ = connection.Close()
	}
	p.mu.Unlock()
	p.server.Close()
}
