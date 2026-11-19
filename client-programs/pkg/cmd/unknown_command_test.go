package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestRejectUnknownCommand checks that an argument after a command group
// which is none of its commands is rejected, with the group named and the
// commands it is close to suggested, and that every other command line,
// a group alone, its help, a command of it or an unknown command of the
// root, which cobra reports itself, is left to cobra.
func TestRejectUnknownCommand(t *testing.T) {
	cases := []struct {
		args   []string
		reject bool
		want   []string
	}{
		{args: []string{"admin", "platform", "config"}, reject: true, want: []string{
			`unknown command "config" for "educates admin platform"`,
			"Run 'educates admin platform --help' for usage.",
		}},
		{args: []string{"admin", "platform", "deplyo"}, reject: true, want: []string{"Did you mean this?\n\tdeploy\n"}},
		{args: []string{"local", "cluster", "--help", "strat"}, reject: true, want: []string{`unknown command "strat" for "educates local cluster"`, "\tstart\n"}},
		{args: []string{"admin", "platform"}},
		{args: []string{"admin", "platform", "--help"}},
		{args: []string{"admin", "platform", "deploy", "--diff"}},
		{args: []string{"admin", "platform", "--", "config"}},
		{args: []string{"nosuch"}},
		{args: []string{"help", "admin", "platform"}},
	}

	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			root := (&ProjectInfo{}).NewEducatesCmdGroup()
			var errOut bytes.Buffer
			root.SetErr(&errOut)

			if got := RejectUnknownCommand(root, tc.args); got != tc.reject {
				t.Fatalf("RejectUnknownCommand = %v, want %v; error output:\n%s", got, tc.reject, errOut.String())
			}
			for _, want := range tc.want {
				if !strings.Contains(errOut.String(), want) {
					t.Errorf("error output lacks %q:\n%s", want, errOut.String())
				}
			}
			if !tc.reject && errOut.Len() > 0 {
				t.Errorf("wrote %q for a command line it leaves to cobra", errOut.String())
			}
		})
	}
}
