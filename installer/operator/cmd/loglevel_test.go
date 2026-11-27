/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package main

import (
	"testing"

	"go.uber.org/zap/zapcore"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

func TestApplyLogLevel_SetsTheLevelOfEachName(t *testing.T) {
	cases := []struct {
		level       string
		lowestShown zapcore.Level
	}{
		{"debug", zapcore.DebugLevel},
		{"info", zapcore.InfoLevel},
		// logr has no warning level: at warn only errors are logged.
		{"warn", zapcore.WarnLevel},
		{"error", zapcore.ErrorLevel},
	}
	for _, c := range cases {
		t.Run(c.level, func(t *testing.T) {
			opts := zap.Options{Development: true}
			if err := applyLogLevel(&opts, c.level, false); err != nil {
				t.Fatalf("applyLogLevel(%q): %v", c.level, err)
			}
			if opts.Level == nil {
				t.Fatalf("applyLogLevel(%q) left the level unset", c.level)
			}
			if !opts.Level.Enabled(c.lowestShown) {
				t.Errorf("level %q does not show %s", c.level, c.lowestShown)
			}
			if c.lowestShown > zapcore.DebugLevel && opts.Level.Enabled(c.lowestShown-1) {
				t.Errorf("level %q shows %s", c.level, c.lowestShown-1)
			}
		})
	}
}

func TestApplyLogLevel_KeepsAnExplicitZapLogLevel(t *testing.T) {
	explicit := zapcore.Level(-2) // --zap-log-level=2, a numeric verbosity
	opts := zap.Options{Development: true, Level: explicit}

	if err := applyLogLevel(&opts, "info", true); err != nil {
		t.Fatalf("applyLogLevel: %v", err)
	}
	if !opts.Level.Enabled(explicit) {
		t.Errorf("an explicit --zap-log-level was overridden by --log-level")
	}
}

func TestApplyLogLevel_RefusesAnUnknownName(t *testing.T) {
	opts := zap.Options{}
	if err := applyLogLevel(&opts, "verbose", false); err == nil {
		t.Fatal("applyLogLevel accepted an unknown level")
	}
}
