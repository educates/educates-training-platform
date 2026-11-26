package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/router"
)

// fakeRouterCertificates gives the router a certificate for each domain,
// numbered in the order asked for, records the certificate each request
// gave as current, and fails as err says.
type fakeRouterCertificates struct {
	asked   []*localclusters.RouterCertificate
	issued  int
	err     error
	changes []string
}

func (f *fakeRouterCertificates) certificate(domain string, current *localclusters.RouterCertificate) (localclusters.RouterCertificate, []string, error) {
	f.asked = append(f.asked, current)
	if f.err != nil {
		return localclusters.RouterCertificate{}, nil, f.err
	}
	f.issued++
	return localclusters.RouterCertificate{
		Domain:      domain,
		Certificate: []byte(fmt.Sprintf("certificate %d of %s\n", f.issued, domain)),
		Key:         []byte(fmt.Sprintf("key %d of %s\n", f.issued, domain)),
	}, f.changes, nil
}

// tlsEndedDataHome is routerDataHome with a named local cluster tls1 on
// tls1.test whose TLS ends at the router, and the inventory of it with
// both clusters running and a running router.
func tlsEndedDataHome(t *testing.T) (string, localclusters.Inventory) {
	t.Helper()
	dataHome := routerDataHome(t)
	writeLocalClusterListFile(t, filepath.Join(dataHome, "clusters", "tls1", "config.yaml"),
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"+
			"ingress:\n  domain: tls1.test\n  externalTLSTermination: true\n")
	return dataHome, routerInventory(dataHome, []localclusters.NodeContainer{
		{Name: "educates-control-plane", KindCluster: "educates", Role: "control-plane", Running: true},
		{Name: "educates-tls1-control-plane", KindCluster: "educates-tls1", Role: "control-plane", Running: true},
	}, routerOn(router.Image, true))
}

// refreshRouterWithCertificates refreshes the router of dataHome, as
// observed in inv, with its certificates from certs, and returns what it
// reported.
func refreshRouterWithCertificates(f *fakeRouterContainer, certs *fakeRouterCertificates, dataHome string, inv localclusters.Inventory) string {
	var out bytes.Buffer
	rc := routerChangeForTest(f, dataHome, nil)
	rc.certificate = certs.certificate
	rc.refreshRouter(context.Background(), progress.New(&out, 0, false),
		func(context.Context) (localclusters.Inventory, error) { return inv, nil })
	return out.String()
}

// The router serves the certificate of the domain of a cluster whose TLS
// ends at the router from its folder, its key readable only by the user,
// and each refresh asks again for it with the one it serves, so it is
// issued again near its expiry.
func TestRefreshRouterServesTheCertificateOfAClusterWhoseTLSEndsAtTheRouter(t *testing.T) {
	dataHome, inv := tlsEndedDataHome(t)
	f := &fakeRouterContainer{}
	certs := &fakeRouterCertificates{changes: []string{"Issued the router's certificate for ingress domain \"tls1.test\"."}}

	out := refreshRouterWithCertificates(f, certs, dataHome, inv)

	certsDir := filepath.Join(config.LocalRouterDir(dataHome), "certs")
	cert, err := os.ReadFile(filepath.Join(certsDir, "tls1.test.pem"))
	if err != nil || string(cert) != "certificate 1 of tls1.test\n" {
		t.Errorf("router folder holds certificate %q, %v; want the one issued", cert, err)
	}
	if info, err := os.Stat(filepath.Join(certsDir, "tls1.test.pem.key")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("certificate key: %v, %v; want it readable only by the user", info, err)
	}
	if !strings.Contains(out, "Issued the router's certificate") || strings.Contains(out, "⚠") {
		t.Errorf("refresh reported:\n%s\nwant the certificate issued noted, and no warning", out)
	}

	refreshRouterWithCertificates(f, certs, dataHome, inv)

	want := []*localclusters.RouterCertificate{nil, {
		Domain:      "tls1.test",
		Certificate: []byte("certificate 1 of tls1.test\n"),
		Key:         []byte("key 1 of tls1.test\n"),
	}}
	if !reflect.DeepEqual(certs.asked, want) {
		t.Errorf("certificates asked for with current %+v, want %+v", certs.asked, want)
	}
}

// A certificate the router can't get is a warning naming the domain, and
// the router is refreshed without it.
func TestRefreshRouterWarnsWhenItHasNoCertificateForADomainWhoseTLSEndsThere(t *testing.T) {
	dataHome, inv := tlsEndedDataHome(t)
	f := &fakeRouterContainer{}
	certs := &fakeRouterCertificates{err: errors.New("no root CA")}

	out := refreshRouterWithCertificates(f, certs, dataHome, inv)

	if !strings.Contains(out, `"tls1.test"`) || !strings.Contains(out, "no root CA") {
		t.Errorf("refresh reported:\n%s\nwant a warning naming the domain and why", out)
	}
	if want := []string{"join", "reload"}; !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls = %q, want %q", f.calls, want)
	}
	if _, err := os.Stat(filepath.Join(config.LocalRouterDir(dataHome), "certs")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("router folder holds certificates: %v", err)
	}
}
