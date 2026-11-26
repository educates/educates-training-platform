package localclusters

import (
	"cmp"
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// RouterConfigDir is where the router container mounts the router folder
// of the data home, config.LocalRouterDir, read-only. The rendered
// haproxy.cfg names every other file of the folder by its path there.
const RouterConfigDir = "/usr/local/etc/haproxy"

// The files of the router folder the router's configuration is rendered
// into, by their names in the folder.
const (
	// routerConfigFile is HAProxy's configuration.
	routerConfigFile = "haproxy.cfg"

	// routerHostMapFile maps each domain the router serves to the local
	// cluster it routes to.
	routerHostMapFile = "hosts.map"

	// routerNotFoundFile is the body of the router's 404, for a name no
	// local cluster serves.
	routerNotFoundFile = "404.txt"
)

// RouterRoute is what the router serves for one local cluster: the
// domains it sends to the cluster, and where it sends them. A domain
// matches itself and every name under it.
type RouterRoute struct {
	// Cluster is the local cluster's identity.
	Cluster Identity

	// IngressDomain is the cluster's ingress domain, declared or derived,
	// empty when its config declares none.
	IngressDomain string

	// ExtraDomains are the cluster's resolver.extraDomains, each once, and
	// none of them its ingress domain.
	ExtraDomains []string

	// Node is the name of the container of the cluster's control-plane
	// node, which the router reaches through Docker's DNS: Envoy listens
	// on ports 80 and 443 of every node.
	Node string

	// State is whether the cluster's nodes run. The router's configuration
	// does not depend on it: a stopped cluster keeps its route, and the
	// router answers its names with a 503 until its node runs again.
	State State

	// TLSEndsAtRouter reports whether the router ends TLS for the cluster,
	// as its ingress.externalTLSTermination asks, rather than passing it
	// through: for its ingress domain, with the certificate the router
	// holds for it. The names under its extra domains, which that
	// certificate does not cover, are served on port 80 only.
	TLSEndsAtRouter bool
}

// Domains returns the domains the router sends to the cluster of route r:
// its ingress domain, when it has one, then its extra domains.
func (r RouterRoute) Domains() []string {
	if r.IngressDomain == "" {
		return r.ExtraDomains
	}
	return append([]string{r.IngressDomain}, r.ExtraDomains...)
}

// RouterFile is a file of the router folder, config.LocalRouterDir, as
// RenderRouter renders it.
type RouterFile struct {
	// Name is the file's path in the router folder, with slashes.
	Name string

	// Content is the file's content.
	Content []byte

	// Mode is the file's permissions: 0644, readable by every user, when
	// zero, and 0600, readable only by the user, for a private key.
	Mode fs.FileMode
}

// RouterCertificate is the certificate the router ends TLS with for the
// ingress domain of a local cluster whose TLS ends at the router.
type RouterCertificate struct {
	// Domain is the ingress domain the certificate serves.
	Domain string

	// Certificate is the certificate, followed by the intermediate CAs it
	// chains through, as PEM.
	Certificate []byte

	// Key is the certificate's private key, as PEM.
	Key []byte
}

// RenderRouter returns the files of the router folder that configure the
// router to serve routes: haproxy.cfg, the host map, the body of the 404
// for a name no route matches, the body of each route's 503, and the
// certificate and key of each route whose TLS ends at the router, among
// certificates, the one for its domain.
//
// Port 80 routes each request by its Host header, without its port,
// through the host map, to the node of the cluster whose domain is the
// most specific to match it, on the node's port 80. A domain matches
// itself and every name under it. Upgraded connections, such as a
// terminal's websocket, may stay idle for an hour. A name no route matches
// gets a plain-text 404 listing the domains the router serves, and a
// route whose node does not answer, such as a stopped cluster's, a
// plain-text 503 naming the cluster and the command that starts it. The
// router never redirects port 80 to https.
//
// Port 443 routes each connection by the server name of its TLS handshake,
// lowercased, through the same host map, and passes it through untouched
// to the node's port 443, where the cluster's ingress presents the
// certificate its own domain CA issued: the router holds no certificate
// and no key for it. A connection that is not TLS, names no server, or
// names one no route matches is closed, and so is one for a route whose
// node does not answer. A connection passed through may stay idle for an
// hour.
//
// For a route whose TLS ends at the router, port 443 hands the connection
// instead to an internal frontend, which ends TLS with the certificate of
// the cluster's domain and sends each request on as plain HTTP, as port
// 80 does, with its Host header unchanged and X-Forwarded-Proto: https and
// X-Forwarded-For added, so a stopped cluster's names get the 503 over
// https too. A name under one of the route's extra domains, which the
// certificate does not cover, is closed, as an unknown name is, and so is
// every connection for the route when certificates hold none for its
// domain.
//
// Each node is found by its container name through Docker's embedded DNS,
// and its address resolved again every second, so a stopped cluster never
// stops the router from starting, and a node that comes back on another
// address is reached there. A route with no domains is left out.
func RenderRouter(routes []RouterRoute, certificates ...RouterCertificate) []RouterFile {
	routes = slices.DeleteFunc(slices.Clone(routes), func(r RouterRoute) bool { return len(r.Domains()) == 0 })
	ended := certificatesOfRoutes(routes, certificates)

	files := []RouterFile{
		{Name: routerConfigFile, Content: []byte(renderHAProxyConfig(routes, ended))},
		{Name: routerHostMapFile, Content: []byte(renderHostMap(routes))},
		{Name: routerNotFoundFile, Content: []byte(renderNotFound(routes))},
	}
	for _, r := range routes {
		files = append(files, RouterFile{Name: unavailableFile(r.Cluster), Content: []byte(renderUnavailable(r.Cluster))})
	}
	for _, r := range routes {
		if c, ok := ended[routeKey(r.Cluster)]; ok {
			certFile := routerCertificateFile(c.Domain)
			files = append(files,
				RouterFile{Name: certFile, Content: c.Certificate},
				RouterFile{Name: certFile + routerKeySuffix, Content: c.Key, Mode: 0o600})
		}
	}
	return files
}

// RouterCertificateDomains returns the domain of each route of routes
// whose TLS ends at the router, in order: the domains the router needs a
// certificate for.
func RouterCertificateDomains(routes []RouterRoute) []string {
	var domains []string
	for _, r := range routes {
		if r.TLSEndsAtRouter && r.IngressDomain != "" {
			domains = append(domains, r.IngressDomain)
		}
	}
	return domains
}

// certificatesOfRoutes returns, by route key, the certificate among
// certificates for the domain of each route of routes whose TLS ends at
// the router, leaving out a route with none.
func certificatesOfRoutes(routes []RouterRoute, certificates []RouterCertificate) map[string]RouterCertificate {
	byDomain := map[string]RouterCertificate{}
	for _, c := range certificates {
		byDomain[domainKey(c.Domain)] = c
	}
	ended := map[string]RouterCertificate{}
	for _, r := range routes {
		if !r.TLSEndsAtRouter {
			continue
		}
		if c, ok := byDomain[domainKey(r.IngressDomain)]; ok {
			ended[routeKey(r.Cluster)] = c
		}
	}
	return ended
}

// The router's certificates live in a folder of the router folder, from
// which the frontend that ends TLS loads every certificate: a file per
// domain, named for the domain, and its key beside it, named for the
// certificate file with routerKeySuffix, as HAProxy looks for it.
const (
	routerCertificatesDir = "certs"
	routerKeySuffix       = ".key"
)

// routerCertificateFile is the name of the file holding the certificate
// the router ends TLS with for domain.
func routerCertificateFile(domain string) string {
	return routerCertificatesDir + "/" + domainKey(domain) + ".pem"
}

// tlsEndedAddress is the internal address port 443 hands the connections
// whose TLS ends at the router to: an abstract socket, which only the
// router's own processes reach.
const tlsEndedAddress = "abns@educates-tls-ended"

// routerConfigPath is the path of the router folder's file name in the
// router container.
func routerConfigPath(name string) string {
	return RouterConfigDir + "/" + name
}

// routeKey is what names a local cluster's route in the router: its kind
// cluster name, which is unique, and a DNS label, where a named local
// cluster may itself be called default.
func routeKey(id Identity) string {
	return id.KindName
}

// unavailableFile is the name of the file holding the body of the 503 for
// the local cluster id.
func unavailableFile(id Identity) string {
	return "503-" + routeKey(id) + ".txt"
}

// haproxyHeader starts the router's configuration: the settings every
// route shares.
const haproxyHeader = `# Rendered by the educates CLI from the local clusters of its data home,
# into the router folder the router container mounts at ` + RouterConfigDir + `.
# Do not edit: the CLI renders it again as local clusters are created,
# started and deleted.

global
    log stdout format raw local0 notice
    # No hard-stop-after: after a reload the previous worker keeps its open
    # connections, such as terminals, until they close. mworker-max-reloads
    # (default 50) bounds how many reloads an old worker survives.

defaults
    log global
    timeout connect 5s
    timeout client 1m
    timeout server 1m
    timeout http-request 10s
    timeout http-keep-alive 1m
    timeout client-fin 30s
    # Upgraded HTTP connections, such as a terminal's websocket, and TLS
    # connections passed through on port 443 may stay idle for an hour.
    timeout tunnel 1h
    # Each node is found by its container name through Docker's embedded
    # DNS, starting with no address, so a stopped cluster never stops the
    # router from starting. Its IPv4 address is the one Envoy's host ports
    # listen on.
    default-server resolvers docker init-addr none resolve-prefer ipv4

# A node's address is resolved again every second, whatever the answer's
# lifetime, so a stopped node leaves within a second or two, and one that
# comes back on another address is reached there.
resolvers docker
    nameserver docker 127.0.0.11:53
    timeout resolve 1s
    timeout retry 1s
    hold nx 1s
    hold refused 1s
    hold timeout 1s
    hold other 1s
    hold valid 1s
`

// renderHAProxyConfig renders the router's haproxy.cfg serving routes,
// ended holding by route key the certificate of each route whose TLS
// ends at the router and that has one.
func renderHAProxyConfig(routes []RouterRoute, ended map[string]RouterCertificate) string {
	var b strings.Builder
	b.WriteString(haproxyHeader)
	fmt.Fprintf(&b, `
# Port 80: each request goes to the local cluster whose domain is the most
# specific to match its Host header, without its port. A name no domain
# matches gets the 404.
frontend http
    mode http
    bind :80
    http-request set-var(txn.host) req.hdr(host),host_only
    http-request set-var(txn.cluster) str(.),concat(,txn.host,),map_end(%s)
    http-request return status 404 content-type "text/plain; charset=utf-8" file %s unless { var(txn.cluster) -m found }
    use_backend http_%%[var(txn.cluster)]
`, routerConfigPath(routerHostMapFile), routerConfigPath(routerNotFoundFile))

	fmt.Fprintf(&b, `
# Port 443: each connection goes to the tls_ backend of the local cluster
# whose domain is the most specific to match the server name of its TLS
# handshake. A connection that is not TLS, names no server, or names one no
# domain matches is closed, since the router has no certificate to answer
# with.
frontend https
    mode tcp
    bind :443
    tcp-request inspect-delay 5s
    tcp-request content reject unless { req.ssl_hello_type 1 }
    tcp-request content set-var(sess.sni) req.ssl_sni,lower
    tcp-request content set-var(sess.cluster) str(.),concat(,sess.sni,),map_end(%s)
    tcp-request content reject unless { var(sess.cluster) -m found }
    use_backend tls_%%[var(sess.cluster)]
`, routerConfigPath(routerHostMapFile))

	if len(ended) > 0 {
		fmt.Fprintf(&b, `
# The local clusters whose TLS ends at the router: port 443 hands their
# connections to this frontend, which ends TLS with the certificate of the
# cluster's domain and refuses a server name no certificate covers. Each
# request goes on as plain HTTP to the port 80 backend of the local cluster
# whose domain is the most specific to match the server name, with its Host
# header unchanged, as a load balancer in front of a cloud cluster sends it.
# Every hop speaks HTTP/1.1, as port 80 does, so websockets upgrade as they
# do there.
frontend tls_ended
    mode http
    bind %s accept-proxy ssl crt %s/ strict-sni alpn http/1.1
    option forwardfor
    http-request set-header X-Forwarded-Proto https
    http-request set-var(txn.sni) ssl_fc_sni,lower
    http-request set-var(txn.cluster) str(.),concat(,txn.sni,),map_end(%s)
    use_backend http_%%[var(txn.cluster)]
`, tlsEndedAddress, routerConfigPath(routerCertificatesDir), routerConfigPath(routerHostMapFile))
	}

	for _, r := range routes {
		key := routeKey(r.Cluster)
		fmt.Fprintf(&b, `
# %s
backend http_%s
    mode http
    http-error status 503 content-type "text/plain; charset=utf-8" file %s
    server node %s:80
`, routeDescription(r.Cluster), key, routerConfigPath(unavailableFile(r.Cluster)), r.Node)
		_, certified := ended[key]
		b.WriteString(renderTLSBackend(r, certified))
	}
	return b.String()
}

// renderTLSBackend renders the backend port 443 sends the connections of
// route r to, by the server name of their TLS handshake, certified
// reporting whether the router holds the certificate of r's domain.
//
// When TLS passes through, the connection goes untouched to the node's
// port 443, where the cluster's ingress presents the certificate its own
// domain CA issued, and a node with no address, such as a stopped
// cluster's, closes the connection. When it ends at the router, the
// connection goes to frontend tls_ended, with the client's address in a
// PROXY protocol header for its X-Forwarded-For, for a name in r's domain
// alone, and is closed for every name when the router has no certificate.
func renderTLSBackend(r RouterRoute, certified bool) string {
	switch {
	case !r.TLSEndsAtRouter:
		return fmt.Sprintf(`
backend tls_%s
    mode tcp
    server node %s:443
`, routeKey(r.Cluster), r.Node)
	case !certified:
		return fmt.Sprintf(`
backend tls_%s
    mode tcp
    # TLS ends at the router for this cluster's domain, but the router has
    # no certificate for it: every connection is closed.
    tcp-request content reject
`, routeKey(r.Cluster))
	default:
		return fmt.Sprintf(`
backend tls_%s
    mode tcp
    # TLS ends at the router for this cluster's domain: frontend tls_ended
    # presents its certificate. A name under one of its extra domains, which
    # the certificate does not cover, is closed, as an unknown name is.
    tcp-request content reject unless { str(.),concat(,sess.sni,) -m end .%s }
    server tls_ended %s send-proxy-v2
`, routeKey(r.Cluster), domainKey(r.IngressDomain), tlsEndedAddress)
	}
}

// routeDescription names the local cluster id in the router's files.
func routeDescription(id Identity) string {
	if id.IsDefault() {
		return "The default local cluster"
	}
	return "Local cluster " + id.Name
}

// hostMapHeader starts the router's host map.
const hostMapHeader = `# Rendered by the educates CLI. Each line is a domain the router serves,
# with a leading dot, then the kind cluster of the local cluster it routes
# to. The router looks up the requested name with a leading dot, so a
# domain matches itself and every name under it, and it takes the first
# line that matches, so the most specific domains come first.
`

// renderHostMap renders the router's host map serving routes: a line for
// each name of each route, lowercase, from the most specific domain to
// the least, as the router takes the first line whose domain matches.
// A domain with more labels is more specific than every domain it is
// under, so names are ordered by their number of labels, most first.
func renderHostMap(routes []RouterRoute) string {
	type entry struct{ domain, key string }
	var entries []entry
	for _, r := range routes {
		for _, name := range r.Domains() {
			entries = append(entries, entry{domain: domainKey(name), key: routeKey(r.Cluster)})
		}
	}
	slices.SortStableFunc(entries, func(a, b entry) int {
		return cmp.Or(
			cmp.Compare(strings.Count(b.domain, "."), strings.Count(a.domain, ".")),
			cmp.Compare(a.domain, b.domain),
		)
	})

	var b strings.Builder
	b.WriteString(hostMapHeader)
	for _, e := range entries {
		fmt.Fprintf(&b, ".%s %s\n", e.domain, e.key)
	}
	return b.String()
}

// renderNotFound renders the body of the router's 404: the domains the
// router serves, each with its local cluster.
func renderNotFound(routes []RouterRoute) string {
	if len(routes) == 0 {
		return "No local cluster serves this name: the router serves no local cluster.\n"
	}
	var b strings.Builder
	b.WriteString("No local cluster serves this name. The router serves these domains,\n" +
		"each with every name under it:\n\n")
	for _, r := range routes {
		for _, name := range r.Domains() {
			fmt.Fprintf(&b, "  %s (%s)\n", name, r.Cluster.Description())
		}
	}
	return b.String()
}

// renderUnavailable renders the body of the 503 of the local cluster id,
// whose node does not answer.
func renderUnavailable(id Identity) string {
	return fmt.Sprintf("%s is not running, or its ingress is not answering yet. Start it with:\n\n"+
		"  educates local cluster start%s\n", capitalize(id.Description()), selectionFlagArg(id))
}
