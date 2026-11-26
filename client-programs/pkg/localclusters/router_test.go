package localclusters

import (
	"slices"
	"strings"
	"testing"
)

// renderedFile returns the content of the file name among the router files
// files, failing t when there is none.
func renderedFile(t *testing.T, files []RouterFile, name string) string {
	t.Helper()
	for _, f := range files {
		if f.Name == name {
			return string(f.Content)
		}
	}
	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	t.Fatalf("no router file %q among %q", name, names)
	return ""
}

// mapEntries returns the entries of the router's host map, its lines that
// are neither comments nor empty, in order.
func mapEntries(t *testing.T, files []RouterFile) []string {
	t.Helper()
	var entries []string
	for _, line := range strings.Split(renderedFile(t, files, "hosts.map"), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			entries = append(entries, line)
		}
	}
	return entries
}

// configSection returns the section of the router's haproxy.cfg that
// starts with the line header, up to the next empty line, failing t when
// there is none.
func configSection(t *testing.T, files []RouterFile, header string) string {
	t.Helper()
	cfg := renderedFile(t, files, "haproxy.cfg")
	start := strings.Index(cfg, "\n"+header+"\n")
	if start < 0 {
		t.Fatalf("haproxy.cfg has no section %q:\n%s", header, cfg)
	}
	section := cfg[start+1:]
	if end := strings.Index(section, "\n\n"); end >= 0 {
		section = section[:end+1]
	}
	return section
}

func wantEntries(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("host map entries:\n got  %q\n want %q", got, want)
	}
}

func TestRenderRouterRoutesAClusterDomainToItsControlPlaneNodeOnPort80(t *testing.T) {
	dataHome := t.TempDir()

	files := RenderRouter([]RouterRoute{{
		Cluster:       Default(dataHome),
		IngressDomain: "educates.test",
		Node:          "educates-control-plane",
		State:         Running,
	}})

	wantEntries(t, mapEntries(t, files), ".educates.test educates")
	want := `backend http_educates
    mode http
    http-error status 503 content-type "text/plain; charset=utf-8" file /usr/local/etc/haproxy/503-educates.txt
    server node educates-control-plane:80
`
	if got := configSection(t, files, "backend http_educates"); got != want {
		t.Errorf("backend:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderRouterCapsTheConnectionsEachWorkerReservesMemoryFor(t *testing.T) {
	global := configSection(t, RenderRouter(nil), "global")

	if !strings.Contains(global, "\n    maxconn 4096\n") {
		t.Errorf("global section sets no maxconn 4096:\n%s", global)
	}
}

func TestRenderRouterOrdersTheMostSpecificDomainFirstAcrossClusters(t *testing.T) {
	dataHome := t.TempDir()
	test2 := namedID(t, "test2", dataHome)

	files := RenderRouter([]RouterRoute{
		{Cluster: Default(dataHome), IngressDomain: "educates.test", ExtraDomains: []string{"Apps.Example.Test"}, Node: "educates-control-plane", State: Running},
		{Cluster: test2, IngressDomain: "test2.educates.test", ExtraDomains: []string{"example.test"}, Node: "educates-test2-control-plane", State: Running},
	})

	// Domains of as many labels are in no particular order to the router,
	// so they come alphabetically.
	wantEntries(t, mapEntries(t, files),
		".apps.example.test educates",
		".test2.educates.test educates-test2",
		".educates.test educates",
		".example.test educates-test2",
	)
	for _, backend := range []string{
		"backend http_educates\n    mode http\n" +
			"    http-error status 503 content-type \"text/plain; charset=utf-8\" file /usr/local/etc/haproxy/503-educates.txt\n" +
			"    server node educates-control-plane:80\n",
		"backend http_educates-test2\n    mode http\n" +
			"    http-error status 503 content-type \"text/plain; charset=utf-8\" file /usr/local/etc/haproxy/503-educates-test2.txt\n" +
			"    server node educates-test2-control-plane:80\n",
	} {
		header, _, _ := strings.Cut(backend, "\n")
		if got := configSection(t, files, header); got != backend {
			t.Errorf("backend:\n%s\nwant:\n%s", got, backend)
		}
	}
}

func TestRenderRouterListsEveryServedDomainInThe404(t *testing.T) {
	dataHome := t.TempDir()

	files := RenderRouter([]RouterRoute{
		{Cluster: Default(dataHome), IngressDomain: "educates.test", ExtraDomains: []string{"example.test"}, Node: "educates-control-plane", State: Running},
		{Cluster: namedID(t, "test2", dataHome), IngressDomain: "test2.10-10-10-1.nip.io", Node: "educates-test2-control-plane", State: Stopped},
	})

	want := `No local cluster serves this name. The router serves these domains,
each with every name under it:

  educates.test (the default local cluster)
  example.test (the default local cluster)
  test2.10-10-10-1.nip.io (local cluster "test2")
`
	if got := renderedFile(t, files, "404.txt"); got != want {
		t.Errorf("404.txt:\n%s\nwant:\n%s", got, want)
	}
}

func TestRenderRouterSaysTheRouterServesNoClusterInThe404WithNoRoutes(t *testing.T) {
	files := RenderRouter(nil)

	want := "No local cluster serves this name: the router serves no local cluster.\n"
	if got := renderedFile(t, files, "404.txt"); got != want {
		t.Errorf("404.txt = %q, want %q", got, want)
	}
	wantEntries(t, mapEntries(t, files))
}

func TestRenderRouterNamesEachClusterAndItsStartCommandInIts503(t *testing.T) {
	dataHome := t.TempDir()

	files := RenderRouter([]RouterRoute{
		{Cluster: Default(dataHome), IngressDomain: "10-10-10-1.nip.io", Node: "educates-control-plane", State: Running},
		{Cluster: namedID(t, "test2", dataHome), IngressDomain: "test2.educates.test", Node: "educates-test2-control-plane", State: Stopped},
	})

	for name, want := range map[string]string{
		"503-educates.txt": "The default local cluster is not running, or its ingress is not answering yet. Start it with:\n\n" +
			"  educates local cluster start\n",
		"503-educates-test2.txt": "Local cluster \"test2\" is not running, or its ingress is not answering yet. Start it with:\n\n" +
			"  educates local cluster start --local-cluster test2\n",
	} {
		if got := renderedFile(t, files, name); got != want {
			t.Errorf("%s:\n%s\nwant:\n%s", name, got, want)
		}
	}
}

// A stopped cluster keeps its route, so that stopping and starting a
// cluster needs no change to the router: its node's name stops resolving,
// and the router answers with the cluster's 503.
func TestRenderRouterRendersTheSameFilesForAStoppedCluster(t *testing.T) {
	dataHome := t.TempDir()
	route := RouterRoute{Cluster: Default(dataHome), IngressDomain: "educates.test", Node: "educates-control-plane", State: Running}
	stopped := route
	stopped.State = Stopped

	running, notRunning := RenderRouter([]RouterRoute{route}), RenderRouter([]RouterRoute{stopped})

	if !slices.EqualFunc(running, notRunning, func(a, b RouterFile) bool {
		return a.Name == b.Name && string(a.Content) == string(b.Content)
	}) {
		t.Errorf("RenderRouter of a stopped cluster differs from a running one's:\n%q\n%q", notRunning, running)
	}
}

func TestRenderRouterLeavesOutARouteWithNoDomains(t *testing.T) {
	dataHome := t.TempDir()

	files := RenderRouter([]RouterRoute{
		{Cluster: Default(dataHome), IngressDomain: "educates.test", Node: "educates-control-plane", State: Running},
		{Cluster: namedID(t, "test2", dataHome), Node: "educates-test2-control-plane", State: Running},
	})

	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	want := []string{"haproxy.cfg", "hosts.map", "404.txt", "503-educates.txt"}
	if !slices.Equal(names, want) {
		t.Errorf("files = %q, want %q", names, want)
	}
	if cfg := renderedFile(t, files, "haproxy.cfg"); strings.Contains(cfg, "educates-test2") {
		t.Errorf("haproxy.cfg routes the cluster with no names:\n%s", cfg)
	}
}

// Port 443 routes each connection by the server name of its TLS handshake,
// lowercased and looked up with a leading dot in the same host map as port
// 80, and refuses a connection that is not TLS, that names no server, or
// whose server name no route matches, since the router has no certificate
// to answer with.
func TestRenderRouterRoutesPort443ByTheTLSServerName(t *testing.T) {
	files := RenderRouter([]RouterRoute{{Cluster: Default(t.TempDir()), IngressDomain: "educates.test", Node: "educates-control-plane"}})

	want := `frontend https
    mode tcp
    bind :443
    tcp-request inspect-delay 5s
    tcp-request content reject unless { req.ssl_hello_type 1 }
    tcp-request content set-var(sess.sni) req.ssl_sni,lower
    tcp-request content set-var(sess.cluster) str(.),concat(,sess.sni,),map_end(/usr/local/etc/haproxy/hosts.map)
    tcp-request content reject unless { var(sess.cluster) -m found }
    use_backend tls_%[var(sess.cluster)]
`
	if got := configSection(t, files, "frontend https"); got != want {
		t.Errorf("443 frontend:\n%s\nwant:\n%s", got, want)
	}
}

// A cluster's TLS passes through the router untouched to its control-plane
// node's port 443, where its own ingress presents the certificate its
// domain CA issued: the backend is TCP, with no certificate of the
// router's.
func TestRenderRouterPassesEachClusterTLSThroughToItsControlPlaneNodeOnPort443(t *testing.T) {
	dataHome := t.TempDir()

	files := RenderRouter([]RouterRoute{
		{Cluster: Default(dataHome), IngressDomain: "educates.test", Node: "educates-control-plane", State: Running},
		{Cluster: namedID(t, "test2", dataHome), IngressDomain: "test2.educates.test", Node: "educates-test2-control-plane", State: Stopped},
	})

	for header, want := range map[string]string{
		"backend tls_educates":       "backend tls_educates\n    mode tcp\n    server node educates-control-plane:443\n",
		"backend tls_educates-test2": "backend tls_educates-test2\n    mode tcp\n    server node educates-test2-control-plane:443\n",
	} {
		if got := configSection(t, files, header); got != want {
			t.Errorf("backend:\n%s\nwant:\n%s", got, want)
		}
	}
	if cfg := renderedFile(t, files, "haproxy.cfg"); strings.Contains(cfg, " ssl") || strings.Contains(cfg, " crt ") {
		t.Errorf("haproxy.cfg ends TLS for a cluster whose TLS passes through:\n%s", cfg)
	}
}

// Port 80 routes by the Host header without its port, looked up with a
// leading dot so a domain matches itself and every name under it, answers
// a name no route matches with the 404, and never redirects to https.
func TestRenderRouterRoutesPort80ByTheHostHeader(t *testing.T) {
	files := RenderRouter([]RouterRoute{{Cluster: Default(t.TempDir()), IngressDomain: "educates.test", Node: "educates-control-plane"}})

	want := `frontend http
    mode http
    bind :80
    http-request set-var(txn.host) req.hdr(host),host_only
    http-request set-var(txn.cluster) str(.),concat(,txn.host,),map_end(/usr/local/etc/haproxy/hosts.map)
    http-request return status 404 content-type "text/plain; charset=utf-8" file /usr/local/etc/haproxy/404.txt unless { var(txn.cluster) -m found }
    use_backend http_%[var(txn.cluster)]
`
	if got := configSection(t, files, "frontend http"); got != want {
		t.Errorf("80 frontend:\n%s\nwant:\n%s", got, want)
	}
	cfg := renderedFile(t, files, "haproxy.cfg")
	for _, setting := range []string{"    timeout tunnel 1h\n", "    default-server resolvers docker init-addr none resolve-prefer ipv4\n", "    hold nx 1s\n"} {
		if !strings.Contains(cfg, setting) {
			t.Errorf("haproxy.cfg has no %q:\n%s", setting, cfg)
		}
	}
	for _, unwanted := range []string{"redirect", "hard-stop-after"} {
		if strings.Contains(cfg, "    "+unwanted+" ") {
			t.Errorf("haproxy.cfg sets %s:\n%s", unwanted, cfg)
		}
	}
}
