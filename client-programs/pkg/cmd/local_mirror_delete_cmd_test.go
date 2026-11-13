package cmd

import (
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

func TestMirrorDeclaredWarningNamesTheClustersThatCreateTheMirrorAgain(t *testing.T) {
	dataHome := t.TempDir()
	test2, err := localclusters.Named("test2", dataHome)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		ids  []localclusters.Identity
		want []string
	}{
		{
			name: "one cluster",
			ids:  []localclusters.Identity{localclusters.Default(dataHome)},
			want: []string{"the config of the default local cluster declares registry mirror docker.io",
				"'educates local cluster start'", "creates the mirror again", "cluster.registryMirrors"},
		},
		{
			name: "several clusters",
			ids:  []localclusters.Identity{localclusters.Default(dataHome), test2},
			want: []string{`the configs of the default local cluster and local cluster "test2" declare registry mirror docker.io`,
				"'educates local cluster start'", "creates the mirror again"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mirrorDeclaredWarning("docker.io", tc.ids)

			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("mirrorDeclaredWarning() = %q, want it to mention %q", got, want)
				}
			}
		})
	}
}
