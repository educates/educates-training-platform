package localclusters

import (
	"strings"
	"testing"
)

func TestResolveSelection(t *testing.T) {
	cases := []struct {
		name string
		sel  Selection
		want string
	}{
		{
			name: "nothing selected means the default cluster",
			sel:  Selection{},
			want: "",
		},
		{
			name: "the environment variable selects a cluster",
			sel:  Selection{Env: "test2"},
			want: "test2",
		},
		{
			name: "the flag selects a cluster",
			sel:  Selection{FlagSet: true, Flag: "test3"},
			want: "test3",
		},
		{
			name: "the flag wins over the environment variable",
			sel:  Selection{FlagSet: true, Flag: "test3", Env: "test2"},
			want: "test3",
		},
		{
			name: "an explicitly empty flag selects the default cluster over the environment variable",
			sel:  Selection{FlagSet: true, Flag: "", Env: "test2"},
			want: "",
		},
		{
			name: "a flag value that was not given is ignored",
			sel:  Selection{Flag: "test3", Env: "test2"},
			want: "test2",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dataHome := t.TempDir()
			got, err := Resolve(tc.sel, dataHome)
			if err != nil {
				t.Fatalf("Resolve(%+v): %v", tc.sel, err)
			}
			want := Default(dataHome)
			if tc.want != "" {
				want, err = Named(tc.want, dataHome)
				if err != nil {
					t.Fatalf("Named(%q): %v", tc.want, err)
				}
			}
			if got != want {
				t.Errorf("Resolve(%+v) = %+v, want %+v", tc.sel, got, want)
			}
		})
	}
}

func TestSelectionIsSet(t *testing.T) {
	cases := []struct {
		name string
		sel  Selection
		want bool
	}{
		{name: "neither the flag nor the environment variable", sel: Selection{}, want: false},
		{name: "the environment variable", sel: Selection{Env: "test2"}, want: true},
		{name: "the flag", sel: Selection{FlagSet: true, Flag: "test3"}, want: true},
		{name: "an explicitly empty flag", sel: Selection{FlagSet: true}, want: true},
		{name: "a flag value that was not given", sel: Selection{Flag: "test3"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.sel.IsSet(); got != tc.want {
				t.Errorf("%+v.IsSet() = %t, want %t", tc.sel, got, tc.want)
			}
		})
	}
}

func TestResolveRejectsInvalidNamesNamingTheSource(t *testing.T) {
	cases := []struct {
		name   string
		sel    Selection
		source string
	}{
		{
			name:   "from the flag",
			sel:    Selection{FlagSet: true, Flag: "Bad_Name", Env: "test2"},
			source: "--local-cluster",
		},
		{
			name:   "from the environment variable",
			sel:    Selection{Env: "Bad_Name"},
			source: "EDUCATES_LOCAL_CLUSTER",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Resolve(tc.sel, t.TempDir())
			if err == nil {
				t.Fatalf("Resolve(%+v) succeeded, want an error", tc.sel)
			}
			msg := err.Error()
			for _, want := range []string{tc.source, `"Bad_Name"`, "lowercase DNS label"} {
				if !strings.Contains(msg, want) {
					t.Errorf("error %q does not mention %q", msg, want)
				}
			}
		})
	}
}
