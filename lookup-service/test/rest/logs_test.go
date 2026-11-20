package rest

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/go-logr/logr"
)

// logRecorder records the lines a service logs, so that a spec can check
// that the service says why it did something.
type logRecorder struct {
	mu    sync.Mutex
	lines []string
	// levels holds the level of each of lines: error, info or debug.
	levels []string
}

// logger returns a logger that records every line it logs, at any
// verbosity, and passes it on to next.
func (r *logRecorder) logger(next logr.Logger) logr.Logger {
	return logr.New(&recordingSink{next: next.GetSink(), recorder: r})
}

// Lines returns the lines logged so far, each the logger's name, the
// message, any error and the key and value pairs.
func (r *logRecorder) Lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.lines)
}

// Levels returns the level, error, info or debug, of each of the lines
// logged so far that contain every one of the substrings, in the order
// they were logged.
func (r *logRecorder) Levels(substrings ...string) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var levels []string
	for i, line := range r.lines {
		if !slices.ContainsFunc(substrings, func(s string) bool { return !strings.Contains(line, s) }) {
			levels = append(levels, r.levels[i])
		}
	}
	return levels
}

// infoOrDebug names the level of a line logged at a logr verbosity.
func infoOrDebug(verbosity int) string {
	if verbosity > 0 {
		return "debug"
	}
	return "info"
}

func (r *logRecorder) record(level, name, msg string, err error, values []any) {
	var line strings.Builder
	if name != "" {
		line.WriteString(name + ": ")
	}
	line.WriteString(msg)
	if err != nil {
		line.WriteString(": " + err.Error())
	}
	for i := 0; i+1 < len(values); i += 2 {
		fmt.Fprintf(&line, " %v=%v", values[i], values[i+1])
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lines = append(r.lines, line.String())
	r.levels = append(r.levels, level)
}

// recordingSink is a logr sink that records every line and passes it on.
type recordingSink struct {
	next     logr.LogSink
	recorder *logRecorder
	name     string
	values   []any
}

func (s *recordingSink) Init(info logr.RuntimeInfo) { s.next.Init(info) }

// Enabled records every verbosity; the sink passed on to decides for itself
// what it writes.
func (s *recordingSink) Enabled(int) bool { return true }

func (s *recordingSink) Info(level int, msg string, keysAndValues ...any) {
	s.recorder.record(infoOrDebug(level), s.name, msg, nil, append(slices.Clone(s.values), keysAndValues...))
	if s.next.Enabled(level) {
		s.next.Info(level, msg, keysAndValues...)
	}
}

func (s *recordingSink) Error(err error, msg string, keysAndValues ...any) {
	s.recorder.record("error", s.name, msg, err, append(slices.Clone(s.values), keysAndValues...))
	s.next.Error(err, msg, keysAndValues...)
}

func (s *recordingSink) WithValues(keysAndValues ...any) logr.LogSink {
	return &recordingSink{
		next:     s.next.WithValues(keysAndValues...),
		recorder: s.recorder,
		name:     s.name,
		values:   append(slices.Clone(s.values), keysAndValues...),
	}
}

func (s *recordingSink) WithName(name string) logr.LogSink {
	full := name
	if s.name != "" {
		full = s.name + "." + name
	}
	return &recordingSink{next: s.next.WithName(name), recorder: s.recorder, name: full, values: s.values}
}
