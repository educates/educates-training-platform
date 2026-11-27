package localclusters

import (
	"testing"
)

// clashDataHome returns a data home whose router address is 10.10.10.1,
// holding the default local cluster's config on educates.test, with the
// extra domain apps.example.test, and the named local cluster test3's,
// on test3.educates.test with the extra domain Shared.Example.Test.
func clashDataHome(t *testing.T) string {
	t.Helper()
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"ingress:\n  domain: educates.test\nresolver:\n  extraDomains:\n  - apps.example.test\n")
	writeDataHomeConfig(t, dataHome, "clusters/test3/config.yaml", localConfigHeader+
		"ingress:\n  domain: test3.educates.test\nresolver:\n  extraDomains:\n  - Shared.Example.Test\n")
	return dataHome
}

func TestCheckCreateDomainsRefusesADomainAnotherClusterClaimsRunningOrStopped(t *testing.T) {
	cases := []struct {
		name    string
		config  string
		claimer string
		state   string
		domain  string
	}{
		{name: "the running default cluster's domain", config: "ingress:\n  domain: educates.test\n",
			claimer: "the default local cluster", state: "running", domain: `"educates.test"`},
		{name: "the default cluster's extra domain, as an extra domain",
			config:  "ingress:\n  domain: test2.educates.test\nresolver:\n  extraDomains:\n  - apps.example.test\n",
			claimer: "the default local cluster", state: "running", domain: `"apps.example.test"`},
		{name: "a stopped cluster's extra domain, in another case", config: "ingress:\n  domain: shared.example.test\n",
			claimer: `local cluster "test3"`, state: "stopped", domain: `"shared.example.test"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := clashDataHome(t)
			inv := NewInventory(Observed{Nodes: []NodeContainer{
				kindControlPlane("educates", true),
				kindControlPlane("educates-test3", false),
			}}, dataHome)
			created := givenLoadedConfig(t, namedID(t, "test2", dataHome), localConfigHeader+tc.config)

			err := inv.CheckCreateDomains(created)

			wantErrorMentioning(t, err, `local cluster "test2"`, tc.claimer, tc.state, tc.domain)
		})
	}
}

// A derived domain is claimed like a declared one.
func TestCheckCreateDomainsRefusesADerivedDomainAnotherClusterClaims(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeDataHomeConfig(t, dataHome, "config.yaml", localConfigHeader+
		"resolver:\n  extraDomains:\n  - test2.10-10-10-1.nip.io\n")
	inv := NewInventory(Observed{Nodes: []NodeContainer{kindControlPlane("educates", false)}}, dataHome)

	err := inv.CheckCreateDomains(givenLoadedConfig(t, namedID(t, "test2", dataHome), localConfigHeader))

	wantErrorMentioning(t, err, `local cluster "test2"`, "the default local cluster", `"test2.10-10-10-1.nip.io"`)
}

// The most specific domain wins, so a subdomain of another cluster's
// domain is no clash, and neither is a domain only a cluster that does not
// exist, or whose config does not load, claims.
func TestCheckCreateDomainsTakesADomainNoOtherEducatesClusterClaims(t *testing.T) {
	cases := map[string]string{
		"a subdomain of the default cluster's":        "ingress:\n  domain: test2.educates.test\n",
		"its name under the router address nip.io":    "# No ingress domain.\n",
		"the domain of a cluster that does not exist": "ingress:\n  domain: test3.educates.test\n",
		"the domain of a config that does not load":   "ingress:\n  domain: test4.educates.test\n",
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			dataHome := clashDataHome(t)
			writeDataHomeConfig(t, dataHome, "clusters/test4/config.yaml", twoControlPlanesConfig)
			inv := NewInventory(Observed{Nodes: []NodeContainer{
				kindControlPlane("educates", true),
				kindControlPlane("educates-test4", true),
			}}, dataHome)

			err := inv.CheckCreateDomains(givenLoadedConfig(t, namedID(t, "test2", dataHome), localConfigHeader+config))
			if err != nil {
				t.Errorf("CheckCreateDomains: %v", err)
			}
		})
	}
}

func TestCheckStartDomainsRefusesADomainAnotherClusterClaimsRunningOrStopped(t *testing.T) {
	dataHome := clashDataHome(t)
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader+
		"ingress:\n  domain: Test3.Educates.Test\n")
	inv := NewInventory(Observed{Nodes: []NodeContainer{
		kindControlPlane("educates", true),
		kindControlPlane("educates-test2", false),
		kindControlPlane("educates-test3", false),
	}}, dataHome)

	err := inv.CheckStartDomains(namedID(t, "test2", dataHome))

	wantErrorMentioning(t, err, `local cluster "test2"`, `local cluster "test3"`, "stopped", `"test3.educates.test"`)
}

// Starting a cluster checks its own domains only: a clash between two
// other clusters is not its to report.
func TestCheckStartDomainsTakesAClusterWhoseDomainsNoOtherClusterClaims(t *testing.T) {
	dataHome := clashDataHome(t)
	writeDataHomeConfig(t, dataHome, "clusters/test2/config.yaml", localConfigHeader)
	writeDataHomeConfig(t, dataHome, "clusters/test4/config.yaml", localConfigHeader+
		"ingress:\n  domain: test3.educates.test\n")
	inv := NewInventory(Observed{Nodes: []NodeContainer{
		kindControlPlane("educates", true),
		kindControlPlane("educates-test2", false),
		kindControlPlane("educates-test3", false),
		kindControlPlane("educates-test4", false),
	}}, dataHome)

	if err := inv.CheckStartDomains(namedID(t, "test2", dataHome)); err != nil {
		t.Errorf("CheckStartDomains: %v", err)
	}
}

// A cluster whose config is missing, as for a cluster created with
// --config, or does not load, claims nothing the router or the resolver
// serves: they leave it out, and say why.
func TestCheckStartDomainsTakesAClusterWithoutAConfigItCanLoad(t *testing.T) {
	dataHome := clashDataHome(t)
	addClusterFolder(t, dataHome, "test2")
	writeDataHomeConfig(t, dataHome, "clusters/test4/config.yaml", twoControlPlanesConfig)
	inv := NewInventory(Observed{Nodes: []NodeContainer{
		kindControlPlane("educates", true),
		kindControlPlane("educates-test2", false),
		kindControlPlane("educates-test4", false),
	}}, dataHome)

	for _, name := range []string{"test2", "test4"} {
		if err := inv.CheckStartDomains(namedID(t, name, dataHome)); err != nil {
			t.Errorf("CheckStartDomains(%s): %v", name, err)
		}
	}
}
