package logger

import (
	"fmt"
	"io"
	"os"
)

// StdoutUI is a local implementation that replaces the ui.UI interface from
// carvel.dev/ytt/pkg/cmd/ui. This allows us to avoid importing that package
// while still being compatible with ytt's RunWithFiles method.
//
// Go's structural typing for interfaces means this type satisfies the ui.UI
// interface as long as it implements the same method signatures:
//   - Printf(string, ...interface{})
//   - Debugf(string, ...interface{})
//   - Warnf(str string, args ...interface{})
//   - DebugWriter() io.Writer
type StdoutUI struct {
	stdout io.Writer
	stderr io.Writer
}

// NewStdoutUI creates a new StdoutUI with default stdout and stderr writers.
func NewStdoutUI() StdoutUI {
	return StdoutUI{os.Stdout, os.Stderr}
}

// Printf writes formatted output to stdout.
func (y StdoutUI) Printf(str string, args ...interface{}) {
	fmt.Fprintf(y.stdout, str, args...)
}

// Warnf writes formatted warning output to stderr.
func (y StdoutUI) Warnf(str string, args ...interface{}) {
	fmt.Fprintf(y.stderr, str, args...)
}

// Debugf writes formatted debug output to stderr if debug mode is enabled.
func (y StdoutUI) Debugf(str string, args ...interface{}) {
	// Do nothing
}

// DebugWriter returns an io.Writer for debug output.
// Returns stderr if debug mode is enabled, otherwise returns a no-op writer.
func (y StdoutUI) DebugWriter() io.Writer {
	// Do nothing
	return noopWriter{}
}

// noopWriter is a writer that discards all data.
type noopWriter struct{}

var _ io.Writer = noopWriter{}

func (w noopWriter) Write(data []byte) (int, error) {
	return len(data), nil
}
