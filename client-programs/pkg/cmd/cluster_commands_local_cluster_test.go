package cmd

import (
	"testing"

	"github.com/spf13/cobra"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

// commandsWithContextFlag returns every command of the CLI that takes a
// --context flag, by command path.
func commandsWithContextFlag() map[string]*cobra.Command {
	found := map[string]*cobra.Command{}
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		if c.Flags().Lookup("context") != nil {
			found[c.CommandPath()] = c
		}
		for _, sub := range c.Commands() {
			walk(sub)
		}
	}
	walk((&ProjectInfo{}).NewEducatesCmdGroup())
	return found
}

// commandsWithOwnContextRules take --context with rules of their own:
// create deploys into the cluster it creates, and the local registry is
// shared by every local cluster.
var commandsWithOwnContextRules = []string{
	"educates local cluster create",
	"educates create-cluster",
	"educates local registry deploy",
}

func TestEveryClusterCommandFollowsTheSelectedLocalCluster(t *testing.T) {
	commands := commandsWithContextFlag()
	for _, path := range commandsWithOwnContextRules {
		delete(commands, path)
	}
	for _, path := range []string{
		"educates cluster workshop deploy",
		"educates cluster workshop serve",
		"educates cluster portal create",
		"educates cluster session list",
		"educates admin platform deploy",
		"educates admin platform delete",
		"educates admin diagnostics collect",
		"educates admin lookup kubeconfig",
		"educates local secrets sync",
		"educates deploy-workshop",
		"educates delete-platform",
	} {
		if commands[path] == nil {
			t.Errorf("%q takes no --context flag, or is missing", path)
		}
	}

	cases := []struct {
		name string
		args []string
		env  string
		want KubeconfigOptions
	}{
		{
			name: "--local-cluster",
			args: []string{"--local-cluster", "test2"},
			want: KubeconfigOptions{Kubeconfig: localClusterKubeconfig(""), Context: "kind-educates-test2"},
		},
		{
			name: "the environment variable",
			env:  "test2",
			want: KubeconfigOptions{Kubeconfig: localClusterKubeconfig(""), Context: "kind-educates-test2"},
		},
		{
			name: "an explicitly empty --local-cluster",
			args: []string{"--local-cluster="},
			env:  "test2",
			want: KubeconfigOptions{Kubeconfig: localClusterKubeconfig(""), Context: "kind-educates"},
		},
		{
			name: "--context over --local-cluster",
			args: []string{"--context", "remote", "--local-cluster", "test2"},
			want: KubeconfigOptions{Context: "remote"},
		},
		{
			name: "neither",
			args: []string{"--kubeconfig", "/given/config"},
			want: KubeconfigOptions{Kubeconfig: "/given/config"},
		},
	}
	for path := range commands {
		for _, tc := range cases {
			t.Run(path+" with "+tc.name, func(t *testing.T) {
				t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
				t.Setenv("EDUCATES_LOCAL_CLUSTER", tc.env)
				c := commandsWithContextFlag()[path]
				if c.Flags().Lookup(localclusters.FlagName) == nil || c.PreRunE == nil {
					t.Fatalf("%q does not take --%s as a kube context selector", path, localclusters.FlagName)
				}
				if err := c.ParseFlags(tc.args); err != nil {
					t.Fatalf("ParseFlags(%q): %v", tc.args, err)
				}

				if err := c.PreRunE(c, nil); err != nil {
					t.Fatalf("PreRunE: %v", err)
				}

				// The kube flags are bound to the options the command runs
				// with, so their values are what it reaches its cluster
				// through.
				got := KubeconfigOptions{
					Kubeconfig: c.Flags().Lookup("kubeconfig").Value.String(),
					Context:    c.Flags().Lookup("context").Value.String(),
				}
				if got != tc.want {
					t.Errorf("reaches its cluster through %+v, want %+v", got, tc.want)
				}
			})
		}
	}
}
