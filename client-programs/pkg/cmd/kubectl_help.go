package cmd

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"k8s.io/kubectl/pkg/util/term"
)

// The kubectl help templates the command groups render their help with
// assume kubectl's command tree, and print three things that are wrong
// for educates:
//
//   - " [options]" after the usage line of every command with flags, and
//     a closing pointer to a "<group> options" command listing global
//     flags, both for the "options" command kubectl has and educates does
//     not;
//   - the closing "Use "... <command> --help"" line of a command group,
//     naming the group by its own name alone, as "portal" for "educates
//     cluster portal", since every command group renders its help as the
//     root of templates of its own.
//
// optionsUsageSuffix, optionsCommandHint and subcommandHelpHint match them
// in rendered help.
var (
	optionsUsageSuffix = regexp.MustCompile(`(?m)^(  \S.*?) \[options\]$`)
	optionsCommandHint = regexp.MustCompile(`(?m)^Use "[^"]*" for a list of global command-line options \(applies to all commands\)\.$\n?`)
	subcommandHelpHint = regexp.MustCompile(`(?m)^Use "[^"]*<command> --help" for more information about a given command\.$`)
	subcommandHelpLine = `Use "%s <command> --help" for more information about a given command.`
)

// correctKubectlHelp makes root and every command under it print their
// help and usage as correctKubectlHelpText corrects them. It takes each
// command's help and usage functions before it wraps any, so that no
// command wraps a function its parent's wrapping already covers. It runs
// once the command tree is built: a command added later, such as cobra's
// completion command, takes the wrapped functions of its parent.
func correctKubectlHelp(root *cobra.Command) {
	type renderers struct {
		command *cobra.Command
		usage   func(*cobra.Command) error
		help    func(*cobra.Command, []string)
	}

	var all []renderers

	var collect func(c *cobra.Command)
	collect = func(c *cobra.Command) {
		all = append(all, renderers{command: c, usage: c.UsageFunc(), help: c.HelpFunc()})
		for _, child := range c.Commands() {
			collect(child)
		}
	}

	collect(root)

	for _, r := range all {
		usage, help := r.usage, r.help

		r.command.SetUsageFunc(func(c *cobra.Command) error {
			target := c.OutOrStderr()

			var err error

			text := renderedKubectlHelp(c, func() { err = usage(c) })

			if err != nil {
				return err
			}

			_, err = fmt.Fprint(term.NewResponsiveWriter(target), text)

			return err
		})

		r.command.SetHelpFunc(func(c *cobra.Command, args []string) {
			target := c.OutOrStdout()

			text := renderedKubectlHelp(c, func() { help(c, args) })

			fmt.Fprint(term.NewResponsiveWriter(target), text)
		})
	}
}

// renderedKubectlHelp runs render, which writes help or usage text of c to
// the output of c, captures the text, and returns it as
// correctKubectlHelpText corrects it. Rendered to a buffer the text is not
// wrapped to the terminal, which the caller does when it prints the text.
//
// The output of c is restored afterwards. Cobra does not say whether c has
// an output of its own: it has none, nor does any command above it, when
// its output and error output fall back to the standard ones, and the
// output is then unset again, so that c keeps writing usage to standard
// error.
func renderedKubectlHelp(c *cobra.Command, render func()) string {
	unset := c.OutOrStdout() == io.Writer(os.Stdout) && c.OutOrStderr() == io.Writer(os.Stderr)
	previous := c.OutOrStdout()

	var buf bytes.Buffer

	c.SetOut(&buf)

	defer func() {
		if unset {
			c.SetOut(nil)
		} else {
			c.SetOut(previous)
		}
	}()

	render()

	return correctKubectlHelpText(c, buf.String())
}

// correctKubectlHelpText returns the rendered help or usage text of c
// without the " [options]" usage suffix and the "options" command hint,
// with the full command path of c in the hint for help on its
// subcommands, and ending in a single newline, or empty when the text is.
func correctKubectlHelpText(c *cobra.Command, text string) string {
	text = optionsUsageSuffix.ReplaceAllString(text, "$1")
	text = optionsCommandHint.ReplaceAllString(text, "")
	text = subcommandHelpHint.ReplaceAllLiteralString(text, fmt.Sprintf(subcommandHelpLine, c.CommandPath()))

	text = strings.TrimRight(text, "\n")

	if text == "" {
		return ""
	}

	return text + "\n"
}
