package localclusters

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/config"
)

// writeDataHomeConfig writes body as the config at rel, a path relative to
// the data home dataHome, creating its folder.
func writeDataHomeConfig(t *testing.T, dataHome, rel, body string) {
	t.Helper()
	path := filepath.Join(dataHome, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// resolverInputs is what a test renders the resolver config from.
type resolverInputs struct {
	DataHome string
	HostIP   string
	Observed []NodeContainer
	Given    []ResolverInput
}

// renderResolver renders the resolver config of the inventory of in's
// data home and observed node containers. A config the resolver leaves
// out is an error, for the tests in which none should be.
func renderResolver(in resolverInputs) ([]byte, error) {
	content, leftOut, err := renderResolverLeavingOut(in)
	if err == nil && len(leftOut) > 0 {
		err = fmt.Errorf("left out configs: %w", errors.Join(leftOut...))
	}
	return content, err
}

// renderResolverLeavingOut renders the resolver config as renderResolver
// does, and returns the configs the resolver leaves out.
func renderResolverLeavingOut(in resolverInputs) ([]byte, []error, error) {
	return NewInventory(Observed{Nodes: in.Observed, HostIP: in.HostIP}, in.DataHome).ResolverConfig(in.Given...)
}

// renderAddresses renders the resolver config from in and returns its
// address lines, in order.
func renderAddresses(t *testing.T, in resolverInputs) []string {
	t.Helper()
	content, err := renderResolver(in)
	if err != nil {
		t.Fatalf("ResolverConfig: %v", err)
	}
	var lines []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "address=") {
			lines = append(lines, line)
		}
	}
	return lines
}

func wantAddresses(t *testing.T, got []string, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("address lines:\n got  %q\n want %q", got, want)
	}
}

func TestResolverConfigServesTheDefaultClusterDomainAtItsListenAddress(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.1\ningress:\n  domain: educates.test\n")

	content, err := renderResolver(resolverInputs{DataHome: dataHome, HostIP: "192.168.1.10"})
	if err != nil {
		t.Fatalf("ResolverConfig: %v", err)
	}

	want := `#log-queries
no-resolv
server=1.0.0.1
server=1.1.1.1
strict-order

# Default local cluster (config.yaml): 10.10.10.1, its listen address
address=/educates.test/10.10.10.1
`
	if got := string(content); got != want {
		t.Errorf("rendered config:\n%s\nwant:\n%s", got, want)
	}
}

// renderHeadings renders the resolver config from in and returns its
// cluster headings, in order.
func renderHeadings(t *testing.T, in resolverInputs) []string {
	t.Helper()
	content, err := renderResolver(in)
	if err != nil {
		t.Fatalf("ResolverConfig: %v", err)
	}
	var headings []string
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "# Default local cluster") || strings.HasPrefix(line, "# Local cluster") {
			headings = append(headings, line)
		}
	}
	return headings
}

func TestResolverConfigHeadingsNameEachClusterConfigAndAddressSource(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")
	observed := []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)}

	for _, tc := range []struct {
		name   string
		hostIP string
		want   []string
	}{
		{
			name:   "with the host's IP known",
			hostIP: "192.168.50.151",
			want: []string{
				"# Default local cluster (config.yaml): 192.168.50.151, the host's IP",
				"# Local cluster test2 (clusters/test2/config.yaml): 10.10.10.2, its listen address",
			},
		},
		{
			name: "with the host's IP unknown",
			want: []string{
				"# Default local cluster (config.yaml): 127.0.0.1, the loopback address as the host's IP is unknown",
				"# Local cluster test2 (clusters/test2/config.yaml): 10.10.10.2, its listen address",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := renderHeadings(t, resolverInputs{DataHome: dataHome, HostIP: tc.hostIP, Observed: observed})

			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("headings:\n got  %q\n want %q", got, tc.want)
			}
		})
	}
}

func TestResolverConfigHeadingNamesAGivenConfigByThePathGiven(t *testing.T) {
	dataHome := t.TempDir()
	def := givenConfig(t, Default(dataHome), localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.1\ningress:\n  domain: educates.test\n")
	def.Source = "my-config.yaml"

	got := renderHeadings(t, resolverInputs{DataHome: dataHome, HostIP: "192.168.1.10", Given: []ResolverInput{def}})

	want := []string{"# Default local cluster (my-config.yaml): 10.10.10.1, its listen address"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("headings:\n got  %q\n want %q", got, want)
	}
}

func TestResolverConfigPointsTheDefaultClusterAtItsSpecificListenAddress(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.1\ningress:\n  domain: educates.test\n"+
		"resolver:\n  extraDomains:\n  - example.test\n")

	got := renderAddresses(t, resolverInputs{DataHome: dataHome, HostIP: "192.168.1.10"})

	wantAddresses(t, got, "address=/educates.test/10.10.10.1", "address=/example.test/10.10.10.1")
}

func TestResolverConfigPointsTheDefaultClusterAtTheHostIPByDefault(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")

	got := renderAddresses(t, resolverInputs{DataHome: dataHome, HostIP: "192.168.1.10"})

	wantAddresses(t, got, "address=/educates.test/192.168.1.10")
}

func TestResolverConfigServesTheDefaultClusterHostIPNipDomainWhenItDeclaresNone(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.1\n")

	got := renderAddresses(t, resolverInputs{DataHome: dataHome, HostIP: "192.168.1.10"})

	wantAddresses(t, got, "address=/192-168-1-10.nip.io/10.10.10.1")
}

func TestResolverConfigPointsTheDefaultClusterAtLoopbackWithoutAHostIP(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"ingress:\n  domain: educates.test\nresolver:\n  extraDomains:\n  - example.test\n")

	got := renderAddresses(t, resolverInputs{DataHome: dataHome})

	wantAddresses(t, got, "address=/educates.test/127.0.0.1", "address=/example.test/127.0.0.1")
}

func TestResolverConfigServesNoDomainForADefaultClusterWithNoneAndNoHostIP(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"resolver:\n  extraDomains:\n  - example.test\n")

	got := renderAddresses(t, resolverInputs{DataHome: dataHome})

	wantAddresses(t, got, "address=/example.test/127.0.0.1")
}

func TestResolverConfigServesEveryClusterAtItsOwnListenAddress(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.1\ningress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test3/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.3\ningress:\n  domain: test3.example.test\n")

	got := renderAddresses(t, resolverInputs{
		DataHome: dataHome,
		HostIP:   "192.168.1.10",
		Observed: []NodeContainer{
			controlPlane("educates", "10.10.10.1", true),
			controlPlane("educates-test3", "10.10.10.3", true),
			controlPlane("educates-test2", "10.10.10.2", false),
		},
	})

	wantAddresses(t, got,
		"address=/educates.test/10.10.10.1",
		"address=/test2.educates.test/10.10.10.2",
		"address=/test3.example.test/10.10.10.3",
	)
}

func TestResolverConfigServesANamedClusterNipDomainWhenItDeclaresNone(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\n")

	got := renderAddresses(t, resolverInputs{
		DataHome: dataHome,
		HostIP:   "192.168.1.10",
		Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)},
	})

	wantAddresses(t, got, "address=/10-10-10-2.nip.io/10.10.10.2")
}

func TestResolverConfigLeavesOutNamedClustersThatDoNotExist(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test3/educates-cluster-config.yaml", "kind: Cluster\n")

	got := renderAddresses(t, resolverInputs{
		DataHome: dataHome,
		HostIP:   "192.168.1.10",
		Observed: []NodeContainer{
			controlPlane("educates-test3", "10.10.10.3", true),
			controlPlane("educates-other", "10.10.10.9", true),
		},
	})

	wantAddresses(t, got, "address=/educates.test/192.168.1.10")
}

func TestResolverConfigRefusesADomainTwoClustersClaim(t *testing.T) {
	cases := []struct {
		name          string
		defaultConfig string
		test2Config   string
		test3Config   string
		claimants     []string
		domain        string
	}{
		{
			name:          "the default and a named cluster's ingress domains",
			defaultConfig: "ingress:\n  domain: educates.test\n",
			test2Config:   "cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: educates.test\n",
			claimants:     []string{"the default local cluster", `local cluster "test2"`},
			domain:        "educates.test",
		},
		{
			name:          "a named cluster's derived domain and the default cluster's extra domain",
			defaultConfig: "ingress:\n  domain: educates.test\nresolver:\n  extraDomains:\n  - 10-10-10-2.nip.io\n",
			test2Config:   "cluster:\n  listenAddress: 10.10.10.2\n",
			claimants:     []string{"the default local cluster", `local cluster "test2"`},
			domain:        "10-10-10-2.nip.io",
		},
		{
			name:        "two named clusters' domains, differing in case",
			test2Config: "cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test.educates.test\n",
			test3Config: "cluster:\n  listenAddress: 10.10.10.3\ningress:\n  domain: Test.Educates.Test\n",
			claimants:   []string{`local cluster "test2"`, `local cluster "test3"`},
			domain:      "test.educates.test",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			for rel, body := range map[string]string{
				"config.yaml":                tc.defaultConfig,
				"clusters/test2/config.yaml": tc.test2Config,
				"clusters/test3/config.yaml": tc.test3Config,
			} {
				if body != "" {
					writeDataHomeConfig(t, dataHome, rel, localConfigHeader+body)
				}
			}

			_, err := renderResolver(resolverInputs{
				DataHome: dataHome,
				HostIP:   "192.168.1.10",
				Observed: []NodeContainer{
					controlPlane("educates-test2", "10.10.10.2", true),
					controlPlane("educates-test3", "10.10.10.3", false),
				},
			})

			wantErrorMentioning(t, err, append(tc.claimants, `"`+tc.domain+`"`)...)
		})
	}
}

func TestResolverConfigServesExtraDomainsOncePerCluster(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n"+
		"resolver:\n  extraDomains:\n  - example.test\n  - test2.educates.test\n  - Example.Test\n  - other.test\n")

	got := renderAddresses(t, resolverInputs{
		DataHome: dataHome,
		Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)},
	})

	wantAddresses(t, got,
		"address=/test2.educates.test/10.10.10.2",
		"address=/example.test/10.10.10.2",
		"address=/other.test/10.10.10.2",
	)
}

// escapeConfig is an EducatesConfig with target.provider: kind whose
// target block holds target, and whose cluster config declares the
// ingress domain domain. With domain empty it declares no cluster config,
// so no domain.
func escapeConfig(target, domain string) string {
	body := "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesConfig\ntarget:\n  provider: kind\n" + target
	if domain == "" {
		return body
	}
	return body + "educatesClusterConfig:\n  mode: Managed\n  ingress:\n    domain: " + domain + "\n" +
		"    ingressClassName: contour\n    controller:\n      provider: BundledContour\n" +
		"    certificates:\n      provider: None\n"
}

func TestResolverConfigServesTheDomainAnEducatesConfigDeclares(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", escapeConfig(
		"  resolver:\n    extraDomains:\n    - extra.example.test\n", "workshops.example.test"))
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", escapeConfig(
		"  cluster:\n    listenAddress: 10.10.10.2\n", "test2.example.test"))
	writeDataHomeConfig(t, dataHome, "clusters/test3/config.yaml", escapeConfig(
		"  cluster:\n    listenAddress: 10.10.10.3\n  resolver:\n    extraDomains:\n    - extra3.example.test\n", "test3.example.test"))

	got := renderAddresses(t, resolverInputs{
		DataHome: dataHome,
		HostIP:   "192.168.1.10",
		Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true), controlPlane("educates-test3", "10.10.10.3", true)},
	})

	wantAddresses(t, got,
		"address=/workshops.example.test/192.168.1.10",
		"address=/extra.example.test/192.168.1.10",
		"address=/test2.example.test/10.10.10.2",
		"address=/test3.example.test/10.10.10.3",
		"address=/extra3.example.test/10.10.10.3",
	)
}

func TestResolverConfigServesNoDomainForAnEducatesConfigThatDeclaresNone(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", escapeConfig("", ""))
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", escapeConfig(
		"  cluster:\n    listenAddress: 10.10.10.2\n  resolver:\n    extraDomains:\n    - extra.example.test\n", ""))

	got := renderAddresses(t, resolverInputs{
		DataHome: dataHome,
		HostIP:   "192.168.1.10",
		Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)},
	})

	wantAddresses(t, got, "address=/extra.example.test/10.10.10.2")
}

// unservableDefaultConfigs are default local cluster configs the local
// DNS resolver can't serve, with why.
var unservableDefaultConfigs = []struct {
	name   string
	body   string
	reason string
}{
	{
		name: "an EducatesConfig without a target",
		body: "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesConfig\neducatesClusterConfig:\n  mode: Managed\n" +
			"  ingress:\n    domain: workshops.example.test\n    ingressClassName: contour\n" +
			"    controller:\n      provider: BundledContour\n    certificates:\n      provider: None\n",
		reason: "no target",
	},
	{
		name: "an EducatesInlineConfig",
		body: "apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesInlineConfig\ndomain: workshops.example.test\n" +
			"ingressClassName: contour\nwildcardCertificateSecret: educates-wildcard-tls\n",
		reason: "EducatesInlineConfig",
	},
}

func TestResolverConfigRefusesAGivenDefaultConfigItCannotServe(t *testing.T) {
	for _, tc := range unservableDefaultConfigs {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			def := givenConfig(t, Default(dataHome), tc.body)

			_, _, err := renderResolverLeavingOut(resolverInputs{DataHome: dataHome, HostIP: "192.168.1.10", Given: []ResolverInput{def}})

			wantErrorMentioning(t, err, def.Source, tc.reason)
		})
	}
}

func TestResolverConfigLeavesOutADataHomeDefaultConfigItCannotServe(t *testing.T) {
	for _, tc := range unservableDefaultConfigs {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			writeDataHomeConfig(t, dataHome, "config.yaml", tc.body)
			writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
				"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")

			content, leftOut, err := renderResolverLeavingOut(resolverInputs{
				DataHome: dataHome,
				HostIP:   "192.168.1.10",
				Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)},
			})
			if err != nil {
				t.Fatalf("ResolverConfig: %v", err)
			}

			if !strings.Contains(string(content), "address=/test2.educates.test/10.10.10.2\n") {
				t.Errorf("rendered config does not serve test2:\n%s", content)
			}
			if strings.Contains(string(content), "Default local cluster") {
				t.Errorf("rendered config serves the default local cluster it can't serve:\n%s", content)
			}
			if len(leftOut) != 1 {
				t.Fatalf("left out %v, want the default local cluster's config only", leftOut)
			}
			wantErrorMentioning(t, leftOut[0], "the default local cluster", filepath.Join(dataHome, "config.yaml"), tc.reason)
		})
	}
}

func TestResolverConfigLeavesOutAnotherClustersConfigThatDoesNotLoad(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+"cluster:\n  listenAddress: 127.0.0.1\n")
	writeDataHomeConfig(t, dataHome, "clusters/test3/config.yaml", localConfigHeader+"cluster:\n  listenAddress: 10.10.10.3\n")
	observed := []NodeContainer{
		controlPlane("educates-test2", "10.10.10.2", true),
		controlPlane("educates-test3", "10.10.10.3", true),
	}
	for _, tc := range []struct {
		name  string
		given []ResolverInput
	}{
		{name: "refreshing the resolver"},
		{name: "creating another cluster", given: []ResolverInput{givenConfig(t, namedID(t, "test4", dataHome),
			localConfigHeader+"cluster:\n  listenAddress: 10.10.10.4\n")}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			content, leftOut, err := renderResolverLeavingOut(resolverInputs{
				DataHome: dataHome, HostIP: "192.168.1.10", Observed: observed, Given: tc.given,
			})
			if err != nil {
				t.Fatalf("ResolverConfig: %v", err)
			}

			for _, line := range []string{"address=/educates.test/192.168.1.10\n", "address=/10-10-10-3.nip.io/10.10.10.3\n"} {
				if !strings.Contains(string(content), line) {
					t.Errorf("rendered config has no line %q:\n%s", line, content)
				}
			}
			if strings.Contains(string(content), "Local cluster test2") {
				t.Errorf("rendered config serves test2, whose config does not load:\n%s", content)
			}
			if len(leftOut) != 1 {
				t.Fatalf("left out %v, want test2's config only", leftOut)
			}
			wantErrorMentioning(t, leftOut[0], `local cluster "test2"`,
				filepath.Join(dataHome, "clusters", "test2", "config.yaml"), "loopback")
		})
	}
}

func TestResolverConfigStillRefusesADomainTwoClustersClaimWhenItLeavesOutAConfig(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+"cluster:\n  listenAddress: 127.0.0.1\n")
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")

	_, _, err := renderResolverLeavingOut(resolverInputs{
		DataHome: dataHome,
		Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)},
		Given: []ResolverInput{givenConfig(t, namedID(t, "test3", dataHome), localConfigHeader+
			"cluster:\n  listenAddress: 10.10.10.3\ningress:\n  domain: educates.test\n")},
	})

	wantErrorMentioning(t, err, "the default local cluster", `local cluster "test3"`, `"educates.test"`)
}

// givenConfig loads body, written outside the data home, as the config of
// the local cluster id, the way a command given a config would.
func givenConfig(t *testing.T, id Identity, body string) ResolverInput {
	t.Helper()
	path := writeConfig(t, body)
	if id.IsDefault() {
		cfg, err := config.Load(path)
		if err != nil {
			t.Fatalf("config.Load: %v", err)
		}
		return ResolverInput{LoadedConfig: LoadedConfig{Identity: id, Config: cfg, Source: path}}
	}
	cfg, err := LoadNamedConfig(path)
	if err != nil {
		t.Fatalf("LoadNamedConfig: %v", err)
	}
	return ResolverInput{LoadedConfig: LoadedConfig{Identity: id, Config: cfg, Source: path}}
}

// namedID is the identity of the named local cluster name of dataHome.
func namedID(t *testing.T, name, dataHome string) Identity {
	t.Helper()
	id, err := Named(name, dataHome)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestResolverConfigServesAClusterBeingCreatedFromItsGivenConfig(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: old.educates.test\n")

	got := renderAddresses(t, resolverInputs{
		DataHome: dataHome,
		HostIP:   "192.168.1.10",
		Given: []ResolverInput{givenConfig(t, namedID(t, "test2", dataHome), localConfigHeader+
			"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")},
	})

	wantAddresses(t, got,
		"address=/educates.test/192.168.1.10",
		"address=/test2.educates.test/10.10.10.2",
	)
}

func TestResolverConfigRefusesCreatingAClusterOnADomainAnotherClaims(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")

	_, err := renderResolver(resolverInputs{
		DataHome: dataHome,
		Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", false)},
		Given: []ResolverInput{givenConfig(t, namedID(t, "test3", dataHome), localConfigHeader+
			"cluster:\n  listenAddress: 10.10.10.3\ningress:\n  domain: test2.educates.test\n")},
	})

	wantErrorMentioning(t, err, `local cluster "test2"`, `local cluster "test3"`, `"test2.educates.test"`)
}

func TestResolverConfigOverridesOnlyTheDefaultClusterDomain(t *testing.T) {
	dataHome := t.TempDir()
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+"ingress:\n  domain: educates.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"cluster:\n  listenAddress: 10.10.10.2\ningress:\n  domain: test2.educates.test\n")
	def := givenConfig(t, Default(dataHome), localConfigHeader+
		"ingress:\n  domain: given.test\nresolver:\n  extraDomains:\n  - override.test\n  - extra.test\n")
	def.ResolverDomainOverride = "override.test"

	got := renderAddresses(t, resolverInputs{
		DataHome: dataHome,
		HostIP:   "192.168.1.10",
		Observed: []NodeContainer{controlPlane("educates-test2", "10.10.10.2", true)},
		Given:    []ResolverInput{def},
	})

	wantAddresses(t, got,
		"address=/override.test/192.168.1.10",
		"address=/extra.test/192.168.1.10",
		"address=/test2.educates.test/10.10.10.2",
	)
}

func TestResolverConfigServesNothingForADataHomeWithNoConfig(t *testing.T) {
	got := renderAddresses(t, resolverInputs{DataHome: t.TempDir(), HostIP: "192.168.1.10"})

	wantAddresses(t, got)
}
