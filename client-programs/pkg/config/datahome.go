package config

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/educates/educates-training-platform/client-programs/pkg/config/v1alpha1"
)

// DefaultLocalConfigModeline is the yaml-language-server modeline that
// points editors at the published EducatesLocalConfig schema for
// completion and validation.
const DefaultLocalConfigModeline = `# yaml-language-server: $schema=` + v1alpha1.SchemaBaseURL + v1alpha1.KindEducatesLocalConfig + `.json`

// DefaultLocalConfigYAML is the minimal EducatesLocalConfig — modeline,
// apiVersion and kind only — written by `local config init` and by
// `local cluster create` when no config exists yet. Every other field
// takes its default at deploy time.
const DefaultLocalConfigYAML = DefaultLocalConfigModeline + `
apiVersion: ` + v1alpha1.APIVersion + `
kind: ` + v1alpha1.KindEducatesLocalConfig + `
`

// LocalConfigPath is the path of the local config in the data home
// dataHome: <data-home>/config.yaml.
func LocalConfigPath(dataHome string) string {
	return filepath.Join(dataHome, "config.yaml")
}

// DnsmasqIncludeDir is the path of the local DNS resolver's include
// folder in the data home dataHome: <data-home>/dnsmasq.d. It holds the
// user's own dnsmasq configuration, in *.conf files, for names that
// resolve to anything other than a local cluster. The CLI creates it
// empty when it is missing, and never writes into it.
func DnsmasqIncludeDir(dataHome string) string {
	return filepath.Join(dataHome, "dnsmasq.d")
}

// LoadLocalConfigIfPresent loads the local config of the data home
// dataHome as it is. It returns nil when the file does not exist or holds
// another kind than EducatesLocalConfig, and neither migrates nor writes
// anything.
func LoadLocalConfigIfPresent(dataHome string) (*v1alpha1.EducatesLocalConfig, error) {
	path := LocalConfigPath(dataHome)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, nil
	}
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	local, _ := cfg.(*v1alpha1.EducatesLocalConfig)
	return local, nil
}

// EnsureLocalConfigFile is the single entry point for commands that
// read <data-home>/config.yaml. It composes the v3-to-v4 migration
// shim with the user-actionable missing-file diagnostic:
//
//   - config.yaml exists → return nil (proceed to Load).
//   - config.yaml missing → attempt v3 → v4 migration. If the v3
//     migration writes a fresh config.yaml, return nil. If migration
//     refuses (provider isn't laptop-kind), surface that error.
//   - config.yaml still missing after migration attempt → return
//     MissingLocalConfigError (first-time user / partial init).
func EnsureLocalConfigFile(dataHome string) error {
	configPath := LocalConfigPath(dataHome)
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}
	if err := MaybeMigrateV3(dataHome, os.Stderr); err != nil {
		return err
	}
	if _, err := os.Stat(configPath); err == nil {
		return nil
	}
	return MissingLocalConfigError(dataHome)
}

// EnsureOrInitLocalConfigFile is the cluster-create counterpart to
// EnsureLocalConfigFile: where the latter returns MissingLocalConfigError
// for a first-time user, this one writes the minimal default
// EducatesLocalConfig in its place so `local cluster create` runs
// end-to-end without a preceding `local config init`. It reports whether
// it created the file so the caller can tell the user.
//
// The v3 → v4 migration still runs first and still wins: a laptop-kind v3
// values.yaml is migrated (created=false), and a non-laptop v3 values.yaml
// still errors out of MaybeMigrateV3 — we never write a default over a
// real, if unmigratable, v3 install. The write below is reached only when
// there is genuinely no prior config to honour.
func EnsureOrInitLocalConfigFile(dataHome string) (created bool, err error) {
	configPath := LocalConfigPath(dataHome)
	if _, err := os.Stat(configPath); err == nil {
		return false, nil
	}
	if err := MaybeMigrateV3(dataHome, os.Stderr); err != nil {
		return false, err
	}
	if _, err := os.Stat(configPath); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(dataHome, 0o755); err != nil {
		return false, fmt.Errorf("create data home %q: %w", dataHome, err)
	}
	if err := os.WriteFile(configPath, []byte(DefaultLocalConfigYAML), 0o644); err != nil {
		return false, fmt.Errorf("write %s: %w", configPath, err)
	}
	return true, nil
}

// MissingLocalConfigError diagnoses why <data-home>/config.yaml is missing
// and returns a user-actionable error. Three cases:
//
//  1. v3 `values.yaml` exists alongside the missing config.yaml — the user
//     is on a pre-v4 data home and needs the migration shim.
//  2. The data home directory itself doesn't exist — first-time user.
//  3. The directory exists but config.yaml is missing — initialised data
//     home (e.g. secrets/ present from past CLI runs) but no v4 config yet.
//
// Returns nil if the data home looks healthy and config.yaml is present;
// callers should not invoke this in that case (the loader handles it).
func MissingLocalConfigError(dataHome string) error {
	configPath := LocalConfigPath(dataHome)
	if _, err := os.Stat(configPath); err == nil {
		// Caller misuse — config.yaml does exist. Surface a generic
		// message so the bug is visible.
		return fmt.Errorf("internal: MissingLocalConfigError called but %s exists", configPath)
	}

	v3Values := filepath.Join(dataHome, "values.yaml")
	if _, err := os.Stat(v3Values); err == nil {
		// EnsureLocalConfigFile would normally have triggered
		// MaybeMigrateV3 before reaching this branch; landing here
		// means migration refused (non-laptop provider) and the user
		// retried without reading that error. Re-state the path
		// forward briefly.
		return fmt.Errorf(`no v4 config found at %s, but a v3-style values.yaml is present at %s.

The migration shim only translates laptop-kind installs
(clusterInfrastructure.provider empty or "kind"). Other providers
need a fresh v4 config declared explicitly:

  educates admin platform render --config <your-v4-config.yaml>

The available v4 kinds live under cli.educates.dev/v1alpha1
(EducatesLocalConfig, EducatesConfig escape hatch, and the
scenario kinds GKE/EKS/Inline).`,
			configPath, v3Values)
	}

	if _, err := os.Stat(dataHome); os.IsNotExist(err) {
		return fmt.Errorf(`no Educates data home found at %s.

First-time setup: write a minimal config and re-run.

  educates local config init

Or, with --config <file>, point at any v4 config file:

  educates admin platform render --config <your-v4-config.yaml>`, dataHome)
	}

	return fmt.Errorf(`no v4 config found at %s.

The data home directory exists but config.yaml is missing. Write a
minimal config and re-run:

  educates local config init

Or point at an explicit v4 config:

  educates admin platform render --config <your-v4-config.yaml>`, configPath)
}
