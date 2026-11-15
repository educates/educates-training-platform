package localcerts_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/localcerts"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

func namedCluster(t *testing.T, name string) localclusters.Identity {
	t.Helper()
	id, err := localclusters.Named(name, t.TempDir())
	if err != nil {
		t.Fatalf("named cluster %q: %v", name, err)
	}
	return id
}

func uses(id localclusters.Identity, names ...string) localcerts.ClusterCAKeyPairs {
	return localcerts.ClusterCAKeyPairs{Identity: id, Known: true, Names: names}
}

func unknown(id localclusters.Identity, why string) localcerts.ClusterCAKeyPairs {
	return localcerts.ClusterCAKeyPairs{Identity: id, Unknown: why}
}

// rootCA is a root CA in the cache, as its CA trust Secret and CA root
// key pair.
var rootCA = []string{"educates-ca", "educates-ca-root-key-pair"}

func cache(names ...string) []string {
	return append(slices.Clone(rootCA), names...)
}

func TestPlanCAKeyPairCleanup(t *testing.T) {
	deleted := localclusters.Default(t.TempDir())
	test2 := namedCluster(t, "test2")
	test3 := namedCluster(t, "test3")

	tests := []struct {
		name        string
		deleted     localcerts.ClusterCAKeyPairs
		others      []localcerts.ClusterCAKeyPairs
		cached      []string
		wantRemove  []string
		wantNotices [][]string
	}{
		{
			name:       "removes the deleted cluster's key pair when no other cluster uses it",
			deleted:    uses(deleted, "a.test-ca-key-pair"),
			others:     []localcerts.ClusterCAKeyPairs{uses(test2, "b.test-ca-key-pair"), uses(test3)},
			cached:     cache("a.test-ca-key-pair", "b.test-ca-key-pair"),
			wantRemove: []string{"a.test-ca-key-pair"},
		},
		{
			name:       "removes the deleted cluster's key pair when it was the only cluster",
			deleted:    uses(deleted, "a.test-ca-key-pair"),
			cached:     cache("a.test-ca-key-pair"),
			wantRemove: []string{"a.test-ca-key-pair"},
		},
		{
			name:    "keeps a key pair another cluster uses",
			deleted: uses(deleted, "a.test-ca-key-pair"),
			others:  []localcerts.ClusterCAKeyPairs{uses(test2, "a.test-ca-key-pair"), uses(test3, "a.test-ca-key-pair")},
			cached:  cache("a.test-ca-key-pair"),
			wantNotices: [][]string{{
				`Kept CA key pair "a.test-ca-key-pair"`,
				`still used by local cluster "test2" and local cluster "test3"`,
			}},
		},
		{
			name:    "keeps a key pair another cluster may use",
			deleted: uses(deleted, "a.test-ca-key-pair"),
			others: []localcerts.ClusterCAKeyPairs{
				uses(test2, "b.test-ca-key-pair"),
				unknown(test3, "its cluster did not answer and the data home has no config for it"),
			},
			cached: cache("a.test-ca-key-pair", "b.test-ca-key-pair"),
			wantNotices: [][]string{{
				`Kept CA key pair "a.test-ca-key-pair"`,
				`can't tell whether local cluster "test3" uses it, since its cluster did not answer and the data home has no config for it`,
			}},
		},
		{
			name:    "says nothing of a key pair the cache does not hold",
			deleted: uses(deleted, "a.test-ca-key-pair"),
			others:  []localcerts.ClusterCAKeyPairs{uses(test2, "a.test-ca-key-pair")},
			cached:  cache("b.test-ca-key-pair"),
		},
		{
			name:    "says nothing when the deleted cluster used no key pair",
			deleted: uses(deleted),
			others:  []localcerts.ClusterCAKeyPairs{unknown(test2, "its config in the data home does not load")},
			cached:  cache("a.test-ca-key-pair"),
		},
		{
			name:    "never removes the root CA, a TLS certificate or another Secret the deleted cluster referenced",
			deleted: uses(deleted, "educates-ca", "educates-ca-root-key-pair", "a.test-tls", "my-ca"),
			cached:  cache("a.test-tls", "my-ca"),
		},
		{
			name:    "removes nothing when it can't tell which key pair the deleted cluster used",
			deleted: unknown(deleted, "its cluster did not answer and the data home has no config for it"),
			cached:  cache("a.test-ca-key-pair"),
			wantNotices: [][]string{{
				"Kept the CA key pairs in the local secrets cache",
				"can't tell which one the default local cluster used, since its cluster did not answer and the data home has no config for it",
				"educates local secrets remove NAME",
			}},
		},
		{
			name:    "says nothing when it can't tell which key pair the deleted cluster used and the cache holds none",
			deleted: unknown(deleted, "its cluster did not answer and the data home has no config for it"),
			cached:  cache("a.test-tls"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := localcerts.PlanCAKeyPairCleanup(tt.deleted, tt.others, tt.cached)

			if !slices.Equal(got.Remove, tt.wantRemove) {
				t.Errorf("Remove = %q, want %q", got.Remove, tt.wantRemove)
			}

			if len(got.Notices) != len(tt.wantNotices) {
				t.Fatalf("Notices = %q, want %d notices", got.Notices, len(tt.wantNotices))
			}
			for i, parts := range tt.wantNotices {
				for _, part := range parts {
					if !strings.Contains(got.Notices[i], part) {
						t.Errorf("notice %d = %q, want it to contain %q", i, got.Notices[i], part)
					}
				}
			}
		})
	}
}

func TestUsedCachedCAKeyPairs(t *testing.T) {
	id := localclusters.Default(t.TempDir())

	got := localcerts.UsedCachedCAKeyPairs(
		uses(id, "b.test-ca-key-pair", "a.test-ca-key-pair", "c.test-ca-key-pair", "educates-ca-root-key-pair", "a.test-ca-key-pair"),
		cache("a.test-ca-key-pair", "b.test-ca-key-pair"),
	)

	if want := []string{"a.test-ca-key-pair", "b.test-ca-key-pair"}; !slices.Equal(got, want) {
		t.Errorf("UsedCachedCAKeyPairs = %q, want %q", got, want)
	}

	if got := localcerts.UsedCachedCAKeyPairs(unknown(id, "why"), cache("a.test-ca-key-pair")); got != nil {
		t.Errorf("UsedCachedCAKeyPairs of an unknown cluster = %q, want none", got)
	}
}
