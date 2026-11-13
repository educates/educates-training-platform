package localclusters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

const localConfigHeader = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\n"

// writeConfig writes body as a config file in a temporary directory and
// returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// wantErrorMentioning fails t unless err is an error whose message holds
// every one of parts.
func wantErrorMentioning(t *testing.T, err error, parts ...string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one mentioning %q", parts)
	}
	for _, part := range parts {
		if !strings.Contains(err.Error(), part) {
			t.Errorf("error %q does not mention %q", err, part)
		}
	}
}

func TestLoadNamedConfigRefusesAMissingListenAddress(t *testing.T) {
	path := writeConfig(t, localConfigHeader)

	_, err := LoadNamedConfig(path)

	wantErrorMentioning(t, err, path, "cluster.listenAddress", "must be set")
}

func TestLoadNamedConfigRefusesAListenAddressThatIsNotASpecificIP(t *testing.T) {
	cases := []struct {
		address string
		reason  []string
	}{
		{address: "0.0.0.0", reason: []string{"every host address", "specific IP"}},
		{address: "::", reason: []string{"every host address", "specific IP"}},
		{address: "::ffff:0.0.0.0", reason: []string{"every host address", "specific IP"}},
		{address: "127.0.0.1", reason: []string{"loopback", "pods resolve the cluster's ingress domain to it"}},
		{address: "127.10.10.2", reason: []string{"loopback", "pods resolve the cluster's ingress domain to it"}},
		{address: "::1", reason: []string{"loopback", "pods resolve the cluster's ingress domain to it"}},
		{address: "localhost", reason: []string{"not an IP address"}},
		{address: "10.10.10", reason: []string{"not an IP address"}},
	}
	for _, tc := range cases {
		t.Run(tc.address, func(t *testing.T) {
			path := writeConfig(t, localConfigHeader+"cluster:\n  listenAddress: '"+tc.address+"'\n")

			_, err := LoadNamedConfig(path)

			wantErrorMentioning(t, err, append([]string{path, "cluster.listenAddress", tc.address}, tc.reason...)...)
		})
	}
}

// loadLocal loads the named config at path and returns it as the
// EducatesLocalConfig it must be.
func loadLocal(t *testing.T, path string) *v1alpha1.EducatesLocalConfig {
	t.Helper()
	cfg, err := LoadNamedConfig(path)
	if err != nil {
		t.Fatalf("LoadNamedConfig: %v", err)
	}
	local, ok := cfg.(*v1alpha1.EducatesLocalConfig)
	if !ok {
		t.Fatalf("LoadNamedConfig returned %T, want *v1alpha1.EducatesLocalConfig", cfg)
	}
	return local
}

func TestLoadNamedConfigFallsBackToTheNipDomainOfTheListenAddress(t *testing.T) {
	path := writeConfig(t, localConfigHeader+"cluster:\n  listenAddress: 10.10.10.2\n")

	c := loadLocal(t, path)

	if got, want := c.Ingress.Domain, "10-10-10-2.nip.io"; got != want {
		t.Errorf("ingress.domain = %q, want %q", got, want)
	}
	if !c.Ingress.Insecure {
		t.Errorf("ingress.insecure = false, want true with the nip.io fallback")
	}
	if got, want := c.Cluster.ListenAddress, "10.10.10.2"; got != want {
		t.Errorf("cluster.listenAddress = %q, want %q", got, want)
	}
	if c.ClusterAdmin == nil || !*c.ClusterAdmin {
		t.Errorf("clusterAdmin = %v, want the static default true", c.ClusterAdmin)
	}
}

func TestLoadNamedConfigNeedsADomainForAnIPv6ListenAddress(t *testing.T) {
	path := writeConfig(t, localConfigHeader+"cluster:\n  listenAddress: 'fd00::2'\n")

	_, err := LoadNamedConfig(path)

	wantErrorMentioning(t, err, path, "fd00::2", "nip.io", "ingress.domain")

	path = writeConfig(t, localConfigHeader+
		"cluster:\n  listenAddress: 'fd00::2'\n"+
		"ingress:\n  domain: test2.educates.test\n")
	if got := loadLocal(t, path).Ingress.Domain; got != "test2.educates.test" {
		t.Errorf("ingress.domain = %q, want the declared test2.educates.test", got)
	}
}

const escapeConfigHeader = "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesConfig\n"

func TestLoadNamedConfigTakesAnEducatesConfigForKindWithoutAFallback(t *testing.T) {
	path := writeConfig(t, escapeConfigHeader+
		"target:\n  provider: kind\n  cluster:\n    listenAddress: 10.10.10.2\n"+
		"educatesClusterConfig:\n  mode: Managed\n")

	cfg, err := LoadNamedConfig(path)
	if err != nil {
		t.Fatalf("LoadNamedConfig: %v", err)
	}

	escape, ok := cfg.(*v1alpha1.EducatesConfig)
	if !ok {
		t.Fatalf("LoadNamedConfig returned %T, want *v1alpha1.EducatesConfig", cfg)
	}
	if got := escape.IngressDomain(); got != "" {
		t.Errorf("ingress domain = %q, want none: an EducatesConfig gets no nip.io fallback", got)
	}
	if got, want := escape.Target.Cluster.ListenAddress, "10.10.10.2"; got != want {
		t.Errorf("target.cluster.listenAddress = %q, want %q", got, want)
	}
}

func TestLoadNamedConfigRefusesAnEducatesConfigWithoutASpecificListenAddress(t *testing.T) {
	cases := []struct {
		name    string
		cluster string
		reason  []string
	}{
		{name: "missing", cluster: "{}", reason: []string{"must be set"}},
		{name: "every address", cluster: "{listenAddress: 0.0.0.0}", reason: []string{"0.0.0.0", "specific IP"}},
		{name: "loopback", cluster: "{listenAddress: 127.0.0.1}", reason: []string{"127.0.0.1", "loopback"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, escapeConfigHeader+"target:\n  provider: kind\n  cluster: "+tc.cluster+"\n")

			_, err := LoadNamedConfig(path)

			wantErrorMentioning(t, err, append([]string{path, "target.cluster.listenAddress"}, tc.reason...)...)
		})
	}
}

func TestLoadNamedConfigRefusesOtherConfigs(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{name: "EducatesConfig without a target", body: escapeConfigHeader},
		{
			name: "EducatesConfig without a provider",
			body: escapeConfigHeader + "target:\n  cluster:\n    listenAddress: 10.10.10.2\n",
		},
		{
			name: "a remote cluster kind",
			body: "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesGKEConfig\n" +
				"gcp:\n  project: p\ndomain: academy.example.com\nacme:\n  email: ops@example.com\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeConfig(t, tc.body)

			_, err := LoadNamedConfig(path)

			wantErrorMentioning(t, err, path, "EducatesLocalConfig", "EducatesConfig with target.provider: kind")
		})
	}
}

func TestLoadNamedConfigKeepsADeclaredDomain(t *testing.T) {
	path := writeConfig(t, localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\n"+
		"ingress:\n  domain: test2.educates.test\n")

	c := loadLocal(t, path)

	if got, want := c.Ingress.Domain, "test2.educates.test"; got != want {
		t.Errorf("ingress.domain = %q, want %q", got, want)
	}
	if c.Ingress.Insecure {
		t.Errorf("ingress.insecure = true, want false as declared")
	}
}
