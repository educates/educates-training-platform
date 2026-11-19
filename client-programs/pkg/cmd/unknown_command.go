package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// RejectUnknownCommand reports whether args, the command line of root,
// name a command group followed by an argument that is none of its
// commands, and then writes the error to the error output of root, as
// cobra does for an unknown command of root itself, with the group's
// commands the argument is close to. A command group runs no command of
// its own, so cobra would otherwise show the group's help and succeed,
// and a script calling a mistyped or removed command would report
// success. Every other command line is left to cobra.
//
// The command groups take no option but --help, so an argument after the
// group that is not an option is the unknown command.
func RejectUnknownCommand(root *cobra.Command, args []string) bool {
	group, rest, err := root.Find(args)
	if err != nil || group == root || group.Runnable() || !group.HasSubCommands() {
		return false
	}

	for _, arg := range rest {
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}

		message := fmt.Sprintf("unknown command %q for %q", arg, group.CommandPath())
		// Cobra sets the edit distance it suggests within, 2 by default,
		// only on its own path for root.
		if group.SuggestionsMinimumDistance <= 0 {
			group.SuggestionsMinimumDistance = 2
		}
		if suggestions := group.SuggestionsFor(arg); len(suggestions) > 0 {
			message += "\n\nDid you mean this?\n\t" + strings.Join(suggestions, "\n\t") + "\n"
		}
		root.PrintErrln(root.ErrPrefix(), message)
		root.PrintErrf("Run '%v --help' for usage.\n", group.CommandPath())
		return true
	}
	return false
}
