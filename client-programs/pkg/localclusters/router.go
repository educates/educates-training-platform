package localclusters

import (
	"cmp"
	"fmt"
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

// RouterRoute is what the router serves for one local cluster: the names
// it sends to the cluster, and where it sends them.
type RouterRoute struct {
	// Cluster is the local cluster's identity.
	Cluster Identity

	// Names are the domains the router sends to the cluster: its ingress
	// domain, declared or derived, then its resolver.extraDomains, each
	// once. A domain matches itself and every name under it.
	Names []string

	// Node is the name of the container of the cluster's control-plane
	// node, which the router reaches through Docker's DNS: Envoy listens
	// on ports 80 and 443 of every node.
	Node string

	// State is whether the cluster's nodes run. The router's configuration
	// does not depend on it: a stopped cluster keeps its route, and the
	// router answers its names with a 503 until its node runs again.
	State State
}

// RouterFile is a file of the router folder, config.LocalRouterDir, as
// RenderRouter renders it.
type RouterFile struct {
	// Name is the file's path in the router folder, with slashes.
	Name string

	// Content is the file's content.
	Content []byte
}

// RenderRouter returns the files of the router folder that configure the
// router to serve routes: haproxy.cfg, the host map, the body of the 404
// for a name no route matches, and the body of each route's 503.
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
// Port 443 is published, but no route serves it: every connection is
// closed.
//
// Each node is found by its container name through Docker's embedded DNS,
// and its address resolved again every second, so a stopped cluster never
// stops the router from starting, and a node that comes back on another
// address is reached there. A route with no names is left out.
func RenderRouter(routes []RouterRoute) []RouterFile {
	routes = slices.DeleteFunc(slices.Clone(routes), func(r RouterRoute) bool { return len(r.Names) == 0 })

	files := []RouterFile{
		{Name: routerConfigFile, Content: []byte(renderHAProxyConfig(routes))},
		{Name: routerHostMapFile, Content: []byte(renderHostMap(routes))},
		{Name: routerNotFoundFile, Content: []byte(renderNotFound(routes))},
	}
	for _, r := range routes {
		files = append(files, RouterFile{Name: unavailableFile(r.Cluster), Content: []byte(renderUnavailable(r.Cluster))})
	}
	return files
}

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
    # Upgraded HTTP connections, such as a terminal's websocket, may stay
    # idle for an hour.
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

// renderHAProxyConfig renders the router's haproxy.cfg serving routes.
func renderHAProxyConfig(routes []RouterRoute) string {
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

# Port 443: no route serves it, so every connection is closed.
frontend https
    mode tcp
    bind :443
    tcp-request connection reject
`, routerConfigPath(routerHostMapFile), routerConfigPath(routerNotFoundFile))

	for _, r := range routes {
		key := routeKey(r.Cluster)
		fmt.Fprintf(&b, `
# %s
backend http_%s
    mode http
    http-error status 503 content-type "text/plain; charset=utf-8" file %s
    server node %s:80
`, routeDescription(r.Cluster), key, routerConfigPath(unavailableFile(r.Cluster)), r.Node)
	}
	return b.String()
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
		for _, name := range r.Names {
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
		for _, name := range r.Names {
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
