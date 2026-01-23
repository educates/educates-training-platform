package logger

import (
	"fmt"
	"io"
	"os"

	"github.com/cppforlife/go-cli-ui/ui"
	"github.com/cppforlife/go-cli-ui/ui/table"
)

// CarvelUI is a minimal implementation of ui.UI for use with Carvel tools
// (imgpkg, vendir, kapp, kbld). It provides simple stdout/stderr output
// without the complexity of ConfUI. It can not be used as replacement for
// ytt UI.
type CarvelUI struct {
	stdout io.Writer
	stderr io.Writer
}

// Ensure CarvelUI implements ui.UI interface at compile time
var _ ui.UI = &CarvelUI{}

// NewCarvelUI creates a new CarvelUI with stdout/stderr writers.
func NewCarvelUI() *CarvelUI {
	return &CarvelUI{
		stdout: os.Stdout,
		stderr: os.Stderr,
	}
}

// NewCarvelUIWithWriters creates a CarvelUI with custom writers.
func NewCarvelUIWithWriters(stdout, stderr io.Writer) *CarvelUI {
	return &CarvelUI{
		stdout: stdout,
		stderr: stderr,
	}
}

// ErrorLinef prints formatted error line to stderr.
func (u *CarvelUI) ErrorLinef(pattern string, args ...interface{}) {
	fmt.Fprintf(u.stderr, pattern+"\n", args...)
}

// PrintLinef prints formatted line to stdout.
func (u *CarvelUI) PrintLinef(pattern string, args ...interface{}) {
	fmt.Fprintf(u.stdout, pattern+"\n", args...)
}

// BeginLinef starts a line (no newline).
func (u *CarvelUI) BeginLinef(pattern string, args ...interface{}) {
	fmt.Fprintf(u.stdout, pattern, args...)
}

// EndLinef ends a line with newline.
func (u *CarvelUI) EndLinef(pattern string, args ...interface{}) {
	fmt.Fprintf(u.stdout, pattern+"\n", args...)
}

// PrintBlock prints a block of bytes to stdout.
func (u *CarvelUI) PrintBlock(block []byte) {
	fmt.Fprint(u.stdout, string(block))
}

// PrintErrorBlock prints error block to stderr.
func (u *CarvelUI) PrintErrorBlock(block string) {
	fmt.Fprint(u.stderr, block)
}

// PrintTable prints a table using the table's own Print method.
func (u *CarvelUI) PrintTable(t table.Table) {
	t.Print(u.stdout)
}

// AskForText - non-interactive mode, returns default value.
func (u *CarvelUI) AskForText(opts ui.TextOpts) (string, error) {
	return opts.Default, nil
}

// AskForChoice - non-interactive mode, returns default choice.
func (u *CarvelUI) AskForChoice(opts ui.ChoiceOpts) (int, error) {
	return opts.Default, nil
}

// AskForPassword - non-interactive mode, returns empty string.
func (u *CarvelUI) AskForPassword(label string) (string, error) {
	return "", nil
}

// AskForConfirmation - non-interactive mode, returns nil (auto-confirmed).
func (u *CarvelUI) AskForConfirmation() error {
	return nil
}

// IsInteractive returns false as this is a non-interactive implementation.
func (u *CarvelUI) IsInteractive() bool {
	return false
}

// Flush is a no-op for this simple implementation.
func (u *CarvelUI) Flush() {}
