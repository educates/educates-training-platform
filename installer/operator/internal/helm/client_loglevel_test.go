/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package helm

import (
	"context"
	"log/slog"
	"testing"

	"k8s.io/client-go/rest"
)

func TestNewClient_GivesTheHelmSDKTheLogLevel(t *testing.T) {
	cfg := &rest.Config{Host: "https://127.0.0.1:1"}
	for _, c := range []struct {
		level     slog.Level
		debugShow bool
	}{
		{slog.LevelDebug, true},
		{slog.LevelInfo, false},
	} {
		client, err := NewClient(cfg, "default", c.level)
		if err != nil {
			t.Fatalf("NewClient: %v", err)
		}
		logger := client.cfg.Logger()
		if got := logger.Enabled(context.Background(), slog.LevelDebug); got != c.debugShow {
			t.Errorf("at %s the Helm SDK logs debug = %t, want %t", c.level, got, c.debugShow)
		}
		if !logger.Enabled(context.Background(), c.level) {
			t.Errorf("at %s the Helm SDK does not log %s", c.level, c.level)
		}
	}
}
