package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

func TestLocalClusterCommandsSelectTheLocalCluster(t *testing.T) {
	p := &ProjectInfo{}
	commands := []struct {
		name string
		new  func() *cobra.Command
	}{
		{"create", p.NewLocalClusterCreateCmd},
		{"delete", p.NewLocalClusterDeleteCmd},
		{"start", p.NewLocalClusterStartCmd},
		{"stop", p.NewLocalClusterStopCmd},
		{"status", p.NewLocalClusterStatusCmd},
	}
	cases := []struct {
		name string
		args []string
		env  string
		want string
	}{
		{name: "the flag", args: []string{"--local-cluster", "test2"}, want: "test2"},
		{name: "the environment variable", env: "test2", want: "test2"},
		{name: "the flag over the environment variable", args: []string{"--local-cluster=test3"}, env: "test2", want: "test3"},
		{name: "an empty flag over the environment variable", args: []string{"--local-cluster="}, env: "test2", want: ""},
		{name: "neither", want: ""},
	}
	for _, command := range commands {
		for _, tc := range cases {
			t.Run(command.name+" with "+tc.name, func(t *testing.T) {
				dataHome := t.TempDir()
				t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
				t.Setenv("EDUCATES_LOCAL_CLUSTER", tc.env)
				c := command.new()
				if err := c.ParseFlags(tc.args); err != nil {
					t.Fatalf("ParseFlags(%q): %v", tc.args, err)
				}

				got, err := selectedLocalCluster(c)
				if err != nil {
					t.Fatalf("selectedLocalCluster: %v", err)
				}

				want := localclusters.Default(dataHome)
				if tc.want != "" {
					want, _ = localclusters.Named(tc.want, dataHome)
				}
				if got != want {
					t.Errorf("selected %+v, want %+v", got, want)
				}
			})
		}
	}
}

func TestCommandsOnAnExistingLocalClusterRefuseANamedClusterWithoutAFolder(t *testing.T) {
	p := &ProjectInfo{}
	for name, newCmd := range map[string]func() *cobra.Command{
		"delete": p.NewLocalClusterDeleteCmd,
		"start":  p.NewLocalClusterStartCmd,
		"stop":   p.NewLocalClusterStopCmd,
		"status": p.NewLocalClusterStatusCmd,
	} {
		t.Run(name, func(t *testing.T) {
			dataHome := t.TempDir()
			t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
			t.Setenv("EDUCATES_LOCAL_CLUSTER", "")
			if err := os.MkdirAll(filepath.Join(dataHome, "clusters", "test2"), 0o755); err != nil {
				t.Fatal(err)
			}

			for _, args := range [][]string{{"--local-cluster", "test2"}, {"--local-cluster="}, {}} {
				c := newCmd()
				if err := c.ParseFlags(args); err != nil {
					t.Fatal(err)
				}
				if _, err := selectedEducatesCluster(c); err != nil {
					t.Errorf("%s %q: selectedEducatesCluster = %v, want an Educates cluster", name, args, err)
				}
			}

			c := newCmd()
			if err := c.ParseFlags([]string{"--local-cluster", "foo"}); err != nil {
				t.Fatal(err)
			}
			_, err := selectedEducatesCluster(c)
			wantError(t, err, `local cluster "foo"`, filepath.Join(dataHome, "clusters", "foo"), "educates-foo")
		})
	}
}

func TestLocalClusterCommandsRefuseAnInvalidName(t *testing.T) {
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
	t.Setenv("EDUCATES_LOCAL_CLUSTER", "")
	c := (&ProjectInfo{}).NewLocalClusterStatusCmd()
	if err := c.ParseFlags([]string{"--local-cluster", "Bad_Name"}); err != nil {
		t.Fatal(err)
	}

	_, err := selectedLocalCluster(c)

	wantError(t, err, "--local-cluster", `"Bad_Name"`, "lowercase DNS label")
}
