package localclusters

import (
	"testing"
)

// The router serves the API server of a local cluster on port 6443 of the
// router address, to a client that names the cluster's control-plane node
// as its TLS server name, whether the cluster declares its domain or
// derives the nip.io name of the router address.
func TestRouterAPIServerOfIsPort6443OfTheRouterAddressByTheNodeName(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"ingress:\n  domain: educates.test\n")
	writeClusterConfig(t, dataHome, "test2", localConfigHeader)

	for _, tc := range []struct {
		id   Identity
		want RouterAPIServer
	}{
		{Default(dataHome), RouterAPIServer{URL: "https://10.10.10.1:6443", ServerName: "educates-control-plane"}},
		{namedID(t, "test2", dataHome), RouterAPIServer{URL: "https://10.10.10.1:6443", ServerName: "educates-test2-control-plane"}},
	} {
		t.Run(tc.id.KindName, func(t *testing.T) {
			got, err := RouterAPIServerOf(tc.id)
			if err != nil || got != tc.want {
				t.Errorf("RouterAPIServerOf() = %+v, %v; want %+v, nil", got, err, tc.want)
			}
		})
	}
}

func TestRouterAPIServerOfBracketsAnIPv6RouterAddress(t *testing.T) {
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: fd00::1\n")
	writeClusterConfig(t, dataHome, "", localConfigHeader+"ingress:\n  domain: educates.test\n")

	got, err := RouterAPIServerOf(Default(dataHome))

	if want := "https://[fd00::1]:6443"; err != nil || got.URL != want {
		t.Errorf("RouterAPIServerOf() = %+v, %v; want URL %s", got, err, want)
	}
}

// The router serves nothing of a cluster whose config is missing, or
// declares no domain at all, and the error says why.
func TestRouterAPIServerOfSaysWhyTheRouterDoesNotServeACluster(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		parts  []string
	}{
		{name: "no config", parts: []string{"the router leaves out", `local cluster "test2"`}},
		{name: "no domain", config: escapeConfig("", ""), parts: []string{"neither an ingress domain nor an extra domain"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
			id := namedForTest(t, dataHome, "test2")
			if tc.config != "" {
				writeClusterConfig(t, dataHome, "test2", tc.config)
			}

			_, err := RouterAPIServerOf(id)

			wantErrorMentioning(t, err, tc.parts...)
		})
	}
}

func TestRouterAPIServerOfNeedsTheRouterFile(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "", localConfigHeader+"ingress:\n  domain: educates.test\n")

	_, err := RouterAPIServerOf(Default(dataHome))

	wantErrorMentioning(t, err, "router.yaml")
}

// kind must not publish a cluster's API server on port 6443 of the router
// address, where the router publishes it for every cluster's API server:
// on the router address, or on every host address, it would take it.
func TestCheckCreateAPIServerPortRefusesTheRouterPortOnTheRouterAddress(t *testing.T) {
	for _, tc := range []struct {
		name   string
		config string
		parts  []string
	}{
		{
			name:   "on the router address",
			config: localConfigHeader + "ingress:\n  domain: educates.test\ncluster:\n  apiServer:\n    address: 10.10.10.1\n    port: 6443\n",
			parts:  []string{"cluster.apiServer.port", "cluster.apiServer.address", "port 6443 of the router address 10.10.10.1"},
		},
		{
			name:   "on every host address",
			config: localConfigHeader + "ingress:\n  domain: educates.test\ncluster:\n  apiServer:\n    address: 0.0.0.0\n    port: 6443\n",
			parts:  []string{"0.0.0.0, every host address", "cluster.apiServer.port"},
		},
		{
			name:   "of an EducatesConfig",
			config: escapeConfig("  cluster:\n    apiServer:\n      address: 0.0.0.0\n      port: 6443\n", "educates.test"),
			parts:  []string{"target.cluster.apiServer.port"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := checkCreateAPIServerPort(t, tc.config)

			wantErrorMentioning(t, err, tc.parts...)
		})
	}
}

// kind publishes the API server on 127.0.0.1 when the config names no
// address, which leaves the router address free, as does another port or
// another specific address.
func TestCheckCreateAPIServerPortAllowsAPortThatLeavesTheRouterPortFree(t *testing.T) {
	for name, apiServer := range map[string]string{
		"no apiServer block":     "",
		"port 6443, no address":  "cluster:\n  apiServer:\n    port: 6443\n",
		"port 6443 on loopback":  "cluster:\n  apiServer:\n    address: 127.0.0.1\n    port: 6443\n",
		"another port":           "cluster:\n  apiServer:\n    address: 10.10.10.1\n    port: 6444\n",
		"another address":        "cluster:\n  apiServer:\n    address: 192.168.1.10\n    port: 6443\n",
		"every address, no port": "cluster:\n  apiServer:\n    address: 0.0.0.0\n",
	} {
		t.Run(name, func(t *testing.T) {
			if err := checkCreateAPIServerPort(t, localConfigHeader+"ingress:\n  domain: educates.test\n"+apiServer); err != nil {
				t.Errorf("CheckCreateAPIServerPort() = %v, want nil", err)
			}
		})
	}
}

// checkCreateAPIServerPort runs CheckCreateAPIServerPort for the default
// local cluster created from the config body, in a data home whose router
// address is 10.10.10.1.
func checkCreateAPIServerPort(t *testing.T, body string) error {
	t.Helper()
	dataHome := t.TempDir()
	routerFile(t, dataHome, routerHeader+"address: 10.10.10.1\n")
	id := Default(dataHome)
	path := writeConfig(t, body)
	cfg, err := LoadConfigFile(id, path)
	if err != nil {
		t.Fatalf("LoadConfigFile: %v", err)
	}
	return NewInventory(Observed{}, dataHome).CheckCreateAPIServerPort(LoadedConfig{Identity: id, Config: cfg, Source: path})
}
