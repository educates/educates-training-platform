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
// assume the "options" command kubectl has for listing global flags, which
// educates does not have. They add " [options]" to the usage line of every
// command with flags, and end the help of a command below a command group
// with a pointer to "<group> options", which runs no such command.
// optionsUsageSuffix and optionsCommandHint match the two in rendered help.
var (
	optionsUsageSuffix = regexp.MustCompile(`(?m)^(  \S.*?) \[options\]$`)
	optionsCommandHint = regexp.MustCompile(`(?m)^Use "[^"]*" for a list of global command-line options \(applies to all commands\)\.$\n?`)
)

// removeOptionsHints makes root and every command under it print their
// help and usage without the hints of the kubectl "options" command. It
// takes each command's help and usage functions before it wraps any, so
// that no command wraps a function its parent's wrapping already covers.
// It runs once the command tree is built: a command added later, such as
// cobra's completion command, takes the wrapped functions of its parent.
func removeOptionsHints(root *cobra.Command) {
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

			text := renderedWithoutOptionsHints(c, func() { err = usage(c) })

			if err != nil {
				return err
			}

			_, err = fmt.Fprint(term.NewResponsiveWriter(target), text)

			return err
		})

		r.command.SetHelpFunc(func(c *cobra.Command, args []string) {
			target := c.OutOrStdout()

			text := renderedWithoutOptionsHints(c, func() { help(c, args) })

			fmt.Fprint(term.NewResponsiveWriter(target), text)
		})
	}
}

// renderedWithoutOptionsHints runs render, which writes help or usage text
// to the output of c, captures the text, and returns it without the
// kubectl options hints and with a single trailing newline. Rendered to a
// buffer the text is not wrapped to the terminal, which the caller does
// when it prints the text.
//
// The output of c is restored afterwards. Cobra does not say whether c has
// an output of its own: it has none, nor does any command above it, when
// its output and error output fall back to the standard ones, and the
// output is then unset again, so that c keeps writing usage to standard
// error.
func renderedWithoutOptionsHints(c *cobra.Command, render func()) string {
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

	return withoutOptionsHints(buf.String())
}

// withoutOptionsHints returns the rendered help or usage text without the
// " [options]" usage suffix and the "options" command hint, ending in a
// single newline, or empty when the text is.
func withoutOptionsHints(text string) string {
	text = optionsUsageSuffix.ReplaceAllString(text, "$1")
	text = optionsCommandHint.ReplaceAllString(text, "")

	text = strings.TrimRight(text, "\n")

	if text == "" {
		return ""
	}

	return text + "\n"
}
