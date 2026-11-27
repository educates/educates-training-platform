/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"flag"
	"fmt"
	"log/slog"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// zapLogLevelFlag is the controller-runtime flag that sets the logger's
// level directly, numeric verbosity included.
const zapLogLevelFlag = "zap-log-level"

// applyLogLevel sets the level of opts from level, the value of
// --log-level: debug, info, warn or error, the log levels the platform
// custom resources and the educates-installer chart use. logr has no
// warning level, so at warn only errors are logged. When zapLevelPassed
// reports that --zap-log-level was given explicitly, its level stays.
func applyLogLevel(opts *zap.Options, level string, zapLevelPassed bool) error {
	var lvl zapcore.Level
	switch level {
	case "debug":
		lvl = zapcore.DebugLevel
	case "info":
		lvl = zapcore.InfoLevel
	case "warn":
		lvl = zapcore.WarnLevel
	case "error":
		lvl = zapcore.ErrorLevel
	default:
		return fmt.Errorf("unknown log level %q: use debug, info, warn or error", level)
	}
	if !zapLevelPassed {
		opts.Level = lvl
	}
	return nil
}

// flagPassed reports whether the flag name was set on the command line.
func flagPassed(fs *flag.FlagSet, name string) bool {
	passed := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			passed = true
		}
	})
	return passed
}

// helmLogLevel returns the slog level the Helm SDK logs at for the
// operator's logger options opts, so that its output follows the
// operator's own level, whichever of --log-level and an explicit
// --zap-log-level set it: debug and any numeric verbosity beyond it give
// the SDK's debug output, and info, warn and error the SDK's own level of
// that name.
func helmLogLevel(opts zap.Options) slog.Level {
	if opts.Level == nil {
		// zap's development mode logs at debug when no level is set.
		return slog.LevelDebug
	}
	switch {
	case opts.Level.Enabled(zapcore.DebugLevel):
		return slog.LevelDebug
	case opts.Level.Enabled(zapcore.InfoLevel):
		return slog.LevelInfo
	case opts.Level.Enabled(zapcore.WarnLevel):
		return slog.LevelWarn
	default:
		return slog.LevelError
	}
}
