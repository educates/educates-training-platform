/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package helm

import (
	"context"
	"testing"

	releasecommon "helm.sh/helm/v4/pkg/release/common"
)

// These exercise EnsureRelease against Helm's real (in-memory) release
// storage, so the install/upgrade/uninstall/rollback calls and the
// status-driven routing run end to end — not just the pure classify logic.

func TestEnsureRelease_FreshInstall(t *testing.T) {
	c, _ := NewMemoryClient("default")
	res, err := c.EnsureRelease(context.Background(), "demo", minimalChart(), map[string]any{"greeting": "hi"})
	if err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}
	if res.Action != ActionInstalled {
		t.Fatalf("Action = %q, want %q", res.Action, ActionInstalled)
	}
	if res.Release == nil || res.Release.Info.Status != releasecommon.StatusDeployed {
		t.Fatalf("release not deployed: %+v", res.Release)
	}
}

func TestEnsureRelease_UnchangedThenUpgradeOnDrift(t *testing.T) {
	c, _ := NewMemoryClient("default")
	ctx := context.Background()
	vals := map[string]any{"greeting": "hi"}

	if _, err := c.EnsureRelease(ctx, "demo", minimalChart(), vals); err != nil {
		t.Fatalf("install: %v", err)
	}

	// Same inputs → no-op.
	res, err := c.EnsureRelease(ctx, "demo", minimalChart(), vals)
	if err != nil {
		t.Fatalf("unchanged: %v", err)
	}
	if res.Action != ActionUnchanged {
		t.Fatalf("Action = %q, want %q", res.Action, ActionUnchanged)
	}

	// Changed values → upgrade.
	res, err = c.EnsureRelease(ctx, "demo", minimalChart(), map[string]any{"greeting": "bonjour"})
	if err != nil {
		t.Fatalf("upgrade: %v", err)
	}
	if res.Action != ActionUpgraded {
		t.Fatalf("Action = %q, want %q", res.Action, ActionUpgraded)
	}
}

// A component that renders empty values installs with map[string]any{}, which
// Helm stores as a nil Config. The second pass must read that back as
// Unchanged, not Upgraded — otherwise it upgrades on every reconcile, climbing
// revisions endlessly (the kyverno-churn regression).
func TestEnsureRelease_EmptyValuesStayUnchanged(t *testing.T) {
	c, _ := NewMemoryClient("default")
	ctx := context.Background()

	res, err := c.EnsureRelease(ctx, "demo", minimalChart(), map[string]any{})
	if err != nil {
		t.Fatalf("install: %v", err)
	}
	if res.Action != ActionInstalled {
		t.Fatalf("first pass Action = %q, want %q", res.Action, ActionInstalled)
	}

	res, err = c.EnsureRelease(ctx, "demo", minimalChart(), map[string]any{})
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if res.Action != ActionUnchanged {
		t.Fatalf("second pass Action = %q, want %q (empty values must not look drifted)", res.Action, ActionUnchanged)
	}
	if res.Release.Version != 1 {
		t.Errorf("release upgraded to revision %d; empty values should stay at revision 1", res.Release.Version)
	}
}

// Values that become empty must replace the release's values, not keep
// them: Helm's upgrade copies the previous values over empty new ones
// unless told otherwise. Kept values leave the release on its old
// settings, such as Kyverno on a removed image registry prefix, and look
// drifted on every pass, so it is upgraded on every reconcile.
func TestEnsureRelease_ValuesBecomingEmptyReplaceTheOldOnes(t *testing.T) {
	c, _ := NewMemoryClient("default")
	ctx := context.Background()

	if _, err := c.EnsureRelease(ctx, "demo", minimalChart(), map[string]any{"greeting": "hi"}); err != nil {
		t.Fatalf("install: %v", err)
	}

	res, err := c.EnsureRelease(ctx, "demo", minimalChart(), map[string]any{})
	if err != nil {
		t.Fatalf("upgrade to empty values: %v", err)
	}
	if res.Action != ActionUpgraded {
		t.Fatalf("Action = %q, want %q", res.Action, ActionUpgraded)
	}
	if len(res.Release.Config) != 0 {
		t.Errorf("release values = %v, want none", res.Release.Config)
	}

	res, err = c.EnsureRelease(ctx, "demo", minimalChart(), map[string]any{})
	if err != nil {
		t.Fatalf("second pass: %v", err)
	}
	if res.Action != ActionUnchanged {
		t.Errorf("second pass Action = %q, want %q", res.Action, ActionUnchanged)
	}
}

// A failed release whose desired inputs are unchanged must be held, not
// retried — and left untouched. This is the regression that caused a failed
// chart install to be reported as healthy.
func TestEnsureRelease_HeldFailedDoesNotChurn(t *testing.T) {
	c, _ := NewMemoryClient("default")
	vals := map[string]any{"greeting": "hi"}
	if err := c.SeedRelease("demo", 1, releasecommon.StatusFailed, minimalChart(), vals, "boom"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := c.EnsureRelease(context.Background(), "demo", minimalChart(), vals)
	if err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}
	if res.Action != ActionHeldFailed {
		t.Fatalf("Action = %q, want %q", res.Action, ActionHeldFailed)
	}
	if res.Release.Info.Description != "boom" {
		t.Errorf("Description = %q, want %q (release should be untouched)", res.Release.Info.Description, "boom")
	}

	// Still the same single failed revision — nothing was reinstalled.
	live, err := c.Status("demo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if live.Version != 1 || live.Info.Status != releasecommon.StatusFailed {
		t.Errorf("release mutated: version=%d status=%s", live.Version, live.Info.Status)
	}
}

// A failed first install (no deployed revision) with drifted inputs — the
// exact contour scenario — recovers via uninstall+reinstall.
func TestEnsureRelease_RepairReinstallOnFailedFirstInstall(t *testing.T) {
	c, _ := NewMemoryClient("default")
	if err := c.SeedRelease("demo", 1, releasecommon.StatusFailed, minimalChart(), map[string]any{"greeting": "old"}, "boom"); err != nil {
		t.Fatalf("seed: %v", err)
	}

	res, err := c.EnsureRelease(context.Background(), "demo", minimalChart(), map[string]any{"greeting": "new"})
	if err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}
	if res.Action != ActionRepairedReinstall {
		t.Fatalf("Action = %q, want %q", res.Action, ActionRepairedReinstall)
	}
	live, err := c.Status("demo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if live.Info.Status != releasecommon.StatusDeployed {
		t.Errorf("release not deployed after repair: %s", live.Info.Status)
	}
}

// A failed upgrade with a prior deployed revision recovers in place (upgrade),
// without the destructive uninstall.
func TestEnsureRelease_RepairUpgradeWhenDeployedRevisionExists(t *testing.T) {
	c, _ := NewMemoryClient("default")
	// rev1 deployed (the last good), rev2 failed (the broken upgrade).
	if err := c.SeedRelease("demo", 1, releasecommon.StatusSuperseded, minimalChart(), map[string]any{"greeting": "v1"}, "ok"); err != nil {
		t.Fatalf("seed rev1: %v", err)
	}
	if err := c.SeedRelease("demo", 2, releasecommon.StatusFailed, minimalChart(), map[string]any{"greeting": "v2"}, "boom"); err != nil {
		t.Fatalf("seed rev2: %v", err)
	}

	res, err := c.EnsureRelease(context.Background(), "demo", minimalChart(), map[string]any{"greeting": "v3"})
	if err != nil {
		t.Fatalf("EnsureRelease: %v", err)
	}
	if res.Action != ActionRepairedUpgrade {
		t.Fatalf("Action = %q, want %q", res.Action, ActionRepairedUpgrade)
	}
	live, err := c.Status("demo")
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if live.Info.Status != releasecommon.StatusDeployed {
		t.Errorf("release not deployed after repair-upgrade: %s", live.Info.Status)
	}
}
