package localclusters

import (
	"testing"
)

func TestConfiguredListenAddress(t *testing.T) {
	cases := []struct {
		name    string
		cluster string
		config  string
		want    string
	}{
		{
			name:    "a named cluster's cluster.listenAddress",
			cluster: "test2",
			config:  localConfigHeader + "cluster:\n  listenAddress: 10.10.10.2\n",
			want:    "10.10.10.2",
		},
		{
			name:    "a named cluster's target.cluster.listenAddress",
			cluster: "test2",
			config: escapeConfigHeader + "target:\n  provider: kind\n  cluster:\n    listenAddress: 10.10.10.2\n" +
				"educatesClusterConfig:\n  mode: Managed\n",
			want: "10.10.10.2",
		},
		{
			name:   "the default cluster's cluster.listenAddress",
			config: localConfigHeader + "cluster:\n  listenAddress: 10.10.10.1\n",
			want:   "10.10.10.1",
		},
		{
			name:   "the default cluster's default when its config declares none",
			config: localConfigHeader,
			want:   "127.0.0.1",
		},
		{
			name: "the default cluster's target.cluster.listenAddress",
			config: escapeConfigHeader + "target:\n  provider: kind\n  cluster:\n    listenAddress: 10.10.10.1\n" +
				"educatesClusterConfig:\n  mode: Managed\n",
			want: "10.10.10.1",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			writeClusterConfig(t, dataHome, tc.cluster, tc.config)

			got, err := ConfiguredListenAddress(identityForTest(t, dataHome, tc.cluster))
			if err != nil {
				t.Fatalf("ConfiguredListenAddress: %v", err)
			}
			if got != tc.want {
				t.Errorf("ConfiguredListenAddress = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestConfiguredListenAddressOfAMissingConfigIsNone(t *testing.T) {
	for _, name := range []string{"", "test2"} {
		got, err := ConfiguredListenAddress(identityForTest(t, t.TempDir(), name))
		if err != nil {
			t.Errorf("ConfiguredListenAddress of cluster %q: %v", name, err)
		}
		if got != "" {
			t.Errorf("ConfiguredListenAddress of cluster %q = %q, want none", name, got)
		}
	}
}

func TestConfiguredListenAddressReportsAConfigThatDoesNotLoad(t *testing.T) {
	dataHome := t.TempDir()
	writeClusterConfig(t, dataHome, "test2", localConfigHeader+"cluster:\n  listenAddress: 127.0.0.1\n")
	id := identityForTest(t, dataHome, "test2")

	_, err := ConfiguredListenAddress(id)

	wantErrorMentioning(t, err, id.ConfigPath, "loopback")
}

// identityForTest returns the identity of the local cluster named name in
// the data home dataHome, the default local cluster's when name is empty.
func identityForTest(t *testing.T, dataHome, name string) Identity {
	t.Helper()
	if name == "" {
		return Default(dataHome)
	}
	id, err := Named(name, dataHome)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
