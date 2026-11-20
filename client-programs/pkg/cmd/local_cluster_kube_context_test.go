package cmd

import (
	"bytes"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/deployer/progress"
	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
)

func TestKubeContextPrecedence(t *testing.T) {
	cases := []struct {
		name  string
		flags KubeconfigOptions
		sel   localclusters.Selection
		want  KubeconfigOptions
	}{
		{
			name: "neither a context nor a local cluster keeps the current context",
			want: KubeconfigOptions{},
		},
		{
			name:  "neither keeps the kubeconfig flag as given",
			flags: KubeconfigOptions{Kubeconfig: "/given/config"},
			want:  KubeconfigOptions{Kubeconfig: "/given/config"},
		},
		{
			name:  "--context wins over --local-cluster",
			flags: KubeconfigOptions{Context: "remote"},
			sel:   localclusters.Selection{FlagSet: true, Flag: "test2"},
			want:  KubeconfigOptions{Context: "remote"},
		},
		{
			name:  "--context wins over the environment variable",
			flags: KubeconfigOptions{Kubeconfig: "/given/config", Context: "remote"},
			sel:   localclusters.Selection{Env: "test2"},
			want:  KubeconfigOptions{Kubeconfig: "/given/config", Context: "remote"},
		},
		{
			name: "--local-cluster selects the named cluster's context",
			sel:  localclusters.Selection{FlagSet: true, Flag: "test2"},
			want: KubeconfigOptions{Kubeconfig: localClusterKubeconfig(""), Context: "kind-educates-test2"},
		},
		{
			name: "the environment variable selects the named cluster's context",
			sel:  localclusters.Selection{Env: "test2"},
			want: KubeconfigOptions{Kubeconfig: localClusterKubeconfig(""), Context: "kind-educates-test2"},
		},
		{
			name: "--local-cluster wins over the environment variable",
			sel:  localclusters.Selection{FlagSet: true, Flag: "test3", Env: "test2"},
			want: KubeconfigOptions{Kubeconfig: localClusterKubeconfig(""), Context: "kind-educates-test3"},
		},
		{
			name: "an explicitly empty --local-cluster selects the default cluster's context",
			sel:  localclusters.Selection{FlagSet: true, Env: "test2"},
			want: KubeconfigOptions{Kubeconfig: localClusterKubeconfig(""), Context: "kind-educates"},
		},
		{
			name:  "a selected cluster's context is looked up in the kubeconfig flag's file",
			flags: KubeconfigOptions{Kubeconfig: "/given/config"},
			sel:   localclusters.Selection{Env: "test2"},
			want:  KubeconfigOptions{Kubeconfig: localClusterKubeconfig("/given/config"), Context: "kind-educates-test2"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveKubeContext(tc.flags, tc.sel, t.TempDir())
			if err != nil {
				t.Fatalf("resolveKubeContext: %v", err)
			}
			if got != tc.want {
				t.Errorf("resolveKubeContext(%+v, %+v) = %+v, want %+v", tc.flags, tc.sel, got, tc.want)
			}
		})
	}
}

func TestExplicitlySelectedLocalCluster(t *testing.T) {
	cases := []struct {
		name         string
		args         []string
		env          string
		wantSelected bool
		want         string
	}{
		{name: "neither", wantSelected: false},
		{name: "the environment variable", env: "test2", wantSelected: true, want: "test2"},
		{name: "--local-cluster", args: []string{"--local-cluster", "test3"}, env: "test2", wantSelected: true, want: "test3"},
		{name: "an explicitly empty --local-cluster", args: []string{"--local-cluster="}, env: "test2", wantSelected: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
			t.Setenv("EDUCATES_LOCAL_CLUSTER", tc.env)
			c := (&ProjectInfo{}).NewLocalSecretsSyncCmd()
			if err := c.ParseFlags(tc.args); err != nil {
				t.Fatalf("ParseFlags(%q): %v", tc.args, err)
			}

			got, selected, err := explicitlySelectedLocalCluster(c)
			if err != nil {
				t.Fatalf("explicitlySelectedLocalCluster: %v", err)
			}
			if selected != tc.wantSelected {
				t.Fatalf("selected = %t, want %t", selected, tc.wantSelected)
			}
			want := localclusters.Identity{}
			if tc.wantSelected {
				want = localclusters.Default(dataHome)
			}
			if tc.want != "" {
				want, _ = localclusters.Named(tc.want, dataHome)
			}
			if got != want {
				t.Errorf("selected %+v, want %+v", got, want)
			}
		})
	}
}

func TestExplicitlySelectedLocalClusterRefusesAnInvalidName(t *testing.T) {
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
	t.Setenv("EDUCATES_LOCAL_CLUSTER", "Bad_Name")
	c := (&ProjectInfo{}).NewLocalSecretsSyncCmd()

	_, selected, err := explicitlySelectedLocalCluster(c)

	wantError(t, err, "EDUCATES_LOCAL_CLUSTER", `"Bad_Name"`)
	if selected {
		t.Errorf("selected = true with an error")
	}
}

func TestKubeContextRefusesAnInvalidLocalClusterName(t *testing.T) {
	_, err := resolveKubeContext(KubeconfigOptions{}, localclusters.Selection{Env: "Bad_Name"}, t.TempDir())

	wantError(t, err, "EDUCATES_LOCAL_CLUSTER", `"Bad_Name"`, "lowercase DNS label")
}

func TestKubeContextIgnoresTheLocalClusterSelectionWithAContext(t *testing.T) {
	flags := KubeconfigOptions{Context: "remote"}

	got, err := resolveKubeContext(flags, localclusters.Selection{Env: "Bad_Name"}, t.TempDir())
	if err != nil {
		t.Fatalf("resolveKubeContext: %v", err)
	}
	if got != flags {
		t.Errorf("resolveKubeContext = %+v, want %+v", got, flags)
	}
}

func TestKubeContextUsage(t *testing.T) {
	cases := []struct {
		name       string
		kubeconfig string
		context    string
		namesFile  bool
		want       string
	}{
		{
			name:       "the file kubectl reads by default is left out",
			kubeconfig: "/home/user/.kube/config",
			context:    "kind-educates",
			want:       "kubectl cluster-info --context kind-educates",
		},
		{
			name:       "another file is named",
			kubeconfig: "/given/config",
			context:    "kind-educates-test2",
			namesFile:  true,
			want:       "kubectl cluster-info --context kind-educates-test2 --kubeconfig /given/config",
		},
		{
			name:       "a file with a space in its path is quoted",
			kubeconfig: "/given/my config",
			context:    "kind-educates",
			namesFile:  true,
			want:       "kubectl cluster-info --context kind-educates --kubeconfig '/given/my config'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := kubeContextUsage(tc.kubeconfig, tc.context, tc.namesFile); got != tc.want {
				t.Errorf("kubeContextUsage = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestNoteKubeContext(t *testing.T) {
	test2, err := localclusters.Named("test2", t.TempDir())
	if err != nil {
		t.Fatalf("Named: %v", err)
	}
	cases := []struct {
		name           string
		kubeconfigFlag string
		kubeconfigEnv  string
		wantCommand    string
	}{
		{
			name:        "the default kubeconfig",
			wantCommand: "kubectl cluster-info --context kind-educates-test2",
		},
		{
			name:           "--kubeconfig names the file",
			kubeconfigFlag: "/home/user/.kube/config",
			wantCommand:    "kubectl cluster-info --context kind-educates-test2 --kubeconfig /home/user/.kube/config",
		},
		{
			name:          "$KUBECONFIG names the file",
			kubeconfigEnv: "/other/config",
			wantCommand:   "kubectl cluster-info --context kind-educates-test2 --kubeconfig /home/user/.kube/config",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("KUBECONFIG", tc.kubeconfigEnv)
			var out bytes.Buffer

			noteKubeContext(progress.New(&out, 0, false), &out, tc.kubeconfigFlag, "/home/user/.kube/config", test2)

			want := "· The current context of /home/user/.kube/config is now kind-educates-test2. Use the cluster with:\n\n    " + tc.wantCommand + "\n"
			if out.String() != want {
				t.Errorf("noteKubeContext wrote %q, want %q", out.String(), want)
			}
		})
	}
}
