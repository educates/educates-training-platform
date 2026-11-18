package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// helpOf runs the educates command tree with the args of command and
// --help, and returns what it prints.
func helpOf(t *testing.T, command []string) string {
	t.Helper()

	root := (&ProjectInfo{}).NewEducatesCmdGroup()

	var out bytes.Buffer

	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append(command, "--help"))

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	return out.String()
}

// TestHelpHasNoOptionsCommandHints checks that the help of a command ends
// with its usage line, without the " [options]" suffix and the pointer to
// a "<group> options" command the kubectl help templates add, for a
// command below a group, a top-level command, and cobra's completion
// command, which cobra adds only when the command tree runs.
func TestHelpHasNoOptionsCommandHints(t *testing.T) {
	cases := [][]string{
		{"cluster", "portal", "list"},
		{"local", "cluster", "create"},
		{"version"},
		{"completion", "zsh"},
	}

	for _, command := range cases {
		t.Run(strings.Join(command, " "), func(t *testing.T) {
			help := helpOf(t, command)

			wantEnd := "Usage:\n  educates " + strings.Join(command, " ") + " [flags]\n"
			if !strings.HasSuffix(help, wantEnd) {
				t.Errorf("help does not end with %q:\n%s", wantEnd, help)
			}

			for _, hint := range []string{"[options]", "global command-line options"} {
				if strings.Contains(help, hint) {
					t.Errorf("help still holds %q:\n%s", hint, help)
				}
			}
		})
	}
}

// TestGroupHelpNamesTheFullCommandPath checks that the help of a command
// group ends by naming the group by its full command path, which the
// kubectl help templates, rendered with the group as their root, cut to
// the group's own name.
func TestGroupHelpNamesTheFullCommandPath(t *testing.T) {
	cases := [][]string{
		{},
		{"cluster"},
		{"cluster", "portal"},
		{"local", "secrets", "add"},
	}

	for _, command := range cases {
		path := strings.Join(append([]string{"educates"}, command...), " ")

		t.Run(path, func(t *testing.T) {
			help := helpOf(t, command)

			wantEnd := `Use "` + path + ` <command> --help" for more information about a given command.` + "\n"
			if !strings.HasSuffix(help, wantEnd) {
				t.Errorf("help does not end with %q:\n%s", wantEnd, help)
			}
		})
	}
}
