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

func (r *logRecorder) record(name, msg string, err error, values []any) {
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
	s.recorder.record(s.name, msg, nil, append(slices.Clone(s.values), keysAndValues...))
	if s.next.Enabled(level) {
		s.next.Info(level, msg, keysAndValues...)
	}
}

func (s *recordingSink) Error(err error, msg string, keysAndValues ...any) {
	s.recorder.record(s.name, msg, err, append(slices.Clone(s.values), keysAndValues...))
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
