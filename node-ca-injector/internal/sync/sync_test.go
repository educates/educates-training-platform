package sync

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func setupTestDir(t *testing.T) (Config, string) {
	t.Helper()
	tmpDir := t.TempDir()

	certsD := filepath.Join(tmpDir, "certs.d")
	if err := os.MkdirAll(certsD, 0755); err != nil {
		t.Fatal(err)
	}

	hostsFile := filepath.Join(tmpDir, "hosts.json")
	caFile := filepath.Join(tmpDir, "ca.crt")

	if err := os.WriteFile(caFile, []byte("-----BEGIN CERTIFICATE-----\ntest-ca-content\n-----END CERTIFICATE-----\n"), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		HostsFile: hostsFile,
		CAFile:    caFile,
		CertsD:    certsD,
	}

	return cfg, tmpDir
}

func writeHostsJSON(t *testing.T, path string, hosts []string) {
	t.Helper()
	data, err := json.Marshal(hosts)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSyncOnce_CreatesHostDirectories(t *testing.T) {
	cfg, _ := setupTestDir(t)
	writeHostsJSON(t, cfg.HostsFile, []string{"registry-session1.example.com", "registry-session2.example.com"})

	if err := SyncOnce(cfg); err != nil {
		t.Fatalf("SyncOnce failed: %v", err)
	}

	// Verify CA was written
	caPath := filepath.Join(cfg.CertsD, caDestDir, "ca.crt")
	if _, err := os.Stat(caPath); os.IsNotExist(err) {
		t.Error("CA file was not created")
	}

	// Verify host directories were created
	for _, host := range []string{"registry-session1.example.com", "registry-session2.example.com"} {
		hostsToml := filepath.Join(cfg.CertsD, host, "hosts.toml")
		if _, err := os.Stat(hostsToml); os.IsNotExist(err) {
			t.Errorf("hosts.toml not created for %s", host)
		}

		marker := filepath.Join(cfg.CertsD, host, managedMarker)
		if _, err := os.Stat(marker); os.IsNotExist(err) {
			t.Errorf("managed marker not created for %s", host)
		}

		content, err := os.ReadFile(hostsToml)
		if err != nil {
			t.Fatal(err)
		}
		expected := hostsTomlContent()
		if string(content) != string(expected) {
			t.Errorf("hosts.toml content mismatch for %s:\ngot:  %q\nwant: %q", host, content, expected)
		}
	}
}

func TestSyncOnce_RemovesStaleDirectories(t *testing.T) {
	cfg, _ := setupTestDir(t)

	// First sync with two hosts
	writeHostsJSON(t, cfg.HostsFile, []string{"host-a.example.com", "host-b.example.com"})
	if err := SyncOnce(cfg); err != nil {
		t.Fatal(err)
	}

	// Second sync with only one host
	writeHostsJSON(t, cfg.HostsFile, []string{"host-a.example.com"})
	if err := SyncOnce(cfg); err != nil {
		t.Fatal(err)
	}

	// host-a should still exist
	if _, err := os.Stat(filepath.Join(cfg.CertsD, "host-a.example.com", "hosts.toml")); os.IsNotExist(err) {
		t.Error("host-a.example.com was incorrectly removed")
	}

	// host-b should be removed
	if _, err := os.Stat(filepath.Join(cfg.CertsD, "host-b.example.com")); !os.IsNotExist(err) {
		t.Error("host-b.example.com was not removed")
	}
}

func TestSyncOnce_PreservesUnmanagedDirectories(t *testing.T) {
	cfg, _ := setupTestDir(t)

	// Create an unmanaged directory (no marker)
	unmanagedDir := filepath.Join(cfg.CertsD, "user-registry.example.com")
	os.MkdirAll(unmanagedDir, 0755)
	os.WriteFile(filepath.Join(unmanagedDir, "hosts.toml"), []byte("server = \"https://user-registry.example.com\""), 0644)

	// Sync with empty host list
	writeHostsJSON(t, cfg.HostsFile, []string{})
	if err := SyncOnce(cfg); err != nil {
		t.Fatal(err)
	}

	// Unmanaged directory should be preserved
	if _, err := os.Stat(filepath.Join(unmanagedDir, "hosts.toml")); os.IsNotExist(err) {
		t.Error("unmanaged directory was incorrectly removed")
	}
}

func TestSyncOnce_UpdatesCAContent(t *testing.T) {
	cfg, _ := setupTestDir(t)
	writeHostsJSON(t, cfg.HostsFile, []string{})

	if err := SyncOnce(cfg); err != nil {
		t.Fatal(err)
	}

	caPath := filepath.Join(cfg.CertsD, caDestDir, "ca.crt")
	content, err := os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}

	// Update CA file
	newCA := []byte("-----BEGIN CERTIFICATE-----\nnew-ca-content\n-----END CERTIFICATE-----\n")
	os.WriteFile(cfg.CAFile, newCA, 0644)

	if err := SyncOnce(cfg); err != nil {
		t.Fatal(err)
	}

	content, err = os.ReadFile(caPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(newCA) {
		t.Errorf("CA was not updated:\ngot:  %q\nwant: %q", content, newCA)
	}
}

func TestSyncOnce_EmptyHostsJSON(t *testing.T) {
	cfg, _ := setupTestDir(t)
	writeHostsJSON(t, cfg.HostsFile, []string{})

	if err := SyncOnce(cfg); err != nil {
		t.Fatalf("SyncOnce failed with empty hosts: %v", err)
	}

	// CA should still be written
	caPath := filepath.Join(cfg.CertsD, caDestDir, "ca.crt")
	if _, err := os.Stat(caPath); os.IsNotExist(err) {
		t.Error("CA file was not created for empty host list")
	}
}

func TestSyncOnce_Idempotent(t *testing.T) {
	cfg, _ := setupTestDir(t)
	writeHostsJSON(t, cfg.HostsFile, []string{"host.example.com"})

	// Run twice
	if err := SyncOnce(cfg); err != nil {
		t.Fatal(err)
	}
	if err := SyncOnce(cfg); err != nil {
		t.Fatal(err)
	}

	// Should still have exactly one host directory (plus CA dir)
	entries, _ := os.ReadDir(cfg.CertsD)
	count := 0
	for _, e := range entries {
		if e.IsDir() {
			count++
		}
	}
	// _educates-ca + host.example.com = 2
	if count != 2 {
		t.Errorf("expected 2 directories, got %d", count)
	}
}
