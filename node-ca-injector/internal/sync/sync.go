package sync

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

const (
	defaultHostsFile    = "/config/hosts/hosts.json"
	defaultCertsD       = "/host/etc/containerd/certs.d"
	caDestDir           = "_educates-ca"
	managedMarker       = ".educates-managed"
	defaultSyncInterval = 10 * time.Second
)

// defaultCAFiles is the ordered list of candidate CA certificate paths.
// The first file that exists will be used.
var defaultCAFiles = []string{
	"/config/ca/ca.crt",
	"/config/ca/tls.crt",
}

// hostsTomlContent generates the hosts.toml content for a registry host.
// Paths reference the node's filesystem, not the container's mount path.
func hostsTomlContent() []byte {
	return []byte(fmt.Sprintf("# Managed by educates node-ca-injector\nca = \"/etc/containerd/certs.d/%s/ca.crt\"\n", caDestDir))
}

// Config holds the sync configuration, injectable for testing.
type Config struct {
	HostsFile    string
	CAFiles      []string
	CertsD       string
	SyncInterval time.Duration
}

// DefaultConfig returns the default configuration for production use.
func DefaultConfig() Config {
	return Config{
		HostsFile:    defaultHostsFile,
		CAFiles:      defaultCAFiles,
		CertsD:       defaultCertsD,
		SyncInterval: defaultSyncInterval,
	}
}

// SyncOnce performs a single sync iteration.
func SyncOnce(cfg Config) error {
	// Ensure CA directory and file on node
	caDir := filepath.Join(cfg.CertsD, caDestDir)
	if err := os.MkdirAll(caDir, 0755); err != nil {
		return fmt.Errorf("creating CA directory: %w", err)
	}

	caContent, err := readFirstExisting(cfg.CAFiles)
	if err != nil {
		return fmt.Errorf("reading CA file: %w", err)
	}

	caDest := filepath.Join(caDir, "ca.crt")
	if err := writeIfChanged(caDest, caContent); err != nil {
		return fmt.Errorf("writing CA to node: %w", err)
	}

	// Read desired hosts (file may not exist if ConfigMap hasn't been created yet)
	var desiredHosts []string
	hostsData, err := os.ReadFile(cfg.HostsFile)
	if err != nil {
		if !os.IsNotExist(err) {
			return fmt.Errorf("reading hosts file: %w", err)
		}
		// ConfigMap not yet created, treat as empty host list
	} else {
		if err := json.Unmarshal(hostsData, &desiredHosts); err != nil {
			return fmt.Errorf("parsing hosts JSON: %w", err)
		}
	}

	desiredSet := make(map[string]bool, len(desiredHosts))
	for _, h := range desiredHosts {
		desiredSet[h] = true
	}

	// Discover current managed directories
	currentManaged := make(map[string]bool)
	entries, err := os.ReadDir(cfg.CertsD)
	if err != nil {
		return fmt.Errorf("reading certs.d directory: %w", err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		markerPath := filepath.Join(cfg.CertsD, e.Name(), managedMarker)
		if _, err := os.Stat(markerPath); err == nil {
			currentManaged[e.Name()] = true
		}
	}

	tomlContent := hostsTomlContent()

	// Create missing host directories
	for host := range desiredSet {
		if currentManaged[host] {
			// Already exists, ensure hosts.toml is current
			tomlPath := filepath.Join(cfg.CertsD, host, "hosts.toml")
			if err := writeIfChanged(tomlPath, tomlContent); err != nil {
				return fmt.Errorf("writing hosts.toml for %s: %w", host, err)
			}
			continue
		}
		hostDir := filepath.Join(cfg.CertsD, host)
		if err := os.MkdirAll(hostDir, 0755); err != nil {
			return fmt.Errorf("creating host directory %s: %w", host, err)
		}
		if err := os.WriteFile(filepath.Join(hostDir, "hosts.toml"), tomlContent, 0644); err != nil {
			return fmt.Errorf("writing hosts.toml for %s: %w", host, err)
		}
		if err := os.WriteFile(filepath.Join(hostDir, managedMarker), nil, 0644); err != nil {
			return fmt.Errorf("writing marker for %s: %w", host, err)
		}
		fmt.Printf("created hosts.d entry for %s\n", host)
	}

	// Remove stale managed directories
	for host := range currentManaged {
		if !desiredSet[host] {
			hostDir := filepath.Join(cfg.CertsD, host)
			if err := os.RemoveAll(hostDir); err != nil {
				return fmt.Errorf("removing stale host directory %s: %w", host, err)
			}
			fmt.Printf("removed hosts.d entry for %s\n", host)
		}
	}

	return nil
}

// readFirstExisting reads the first file from paths that exists. Returns an error if none exist.
func readFirstExisting(paths []string) ([]byte, error) {
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err == nil {
			return data, nil
		}
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("reading %s: %w", p, err)
		}
	}
	return nil, fmt.Errorf("no CA file found, tried: %v", paths)
}

// writeIfChanged writes content to path only if the file doesn't exist or has different content.
func writeIfChanged(path string, content []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}
	return os.WriteFile(path, content, 0644)
}

// Run starts the sync loop until interrupted.
func Run() error {
	cfg := DefaultConfig()

	// Allow override via environment variables
	if v := os.Getenv("HOSTS_FILE"); v != "" {
		cfg.HostsFile = v
	}
	if v := os.Getenv("CA_FILE"); v != "" {
		cfg.CAFiles = []string{v}
	}
	if v := os.Getenv("CERTS_D"); v != "" {
		cfg.CertsD = v
	}

	fmt.Printf("starting sync loop: hosts=%s ca=%v certs.d=%s interval=%s\n",
		cfg.HostsFile, cfg.CAFiles, cfg.CertsD, cfg.SyncInterval)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)

	// Run immediately on startup
	if err := SyncOnce(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "sync error: %v\n", err)
	}

	ticker := time.NewTicker(cfg.SyncInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if err := SyncOnce(cfg); err != nil {
				fmt.Fprintf(os.Stderr, "sync error: %v\n", err)
			}
		case <-stop:
			fmt.Println("shutting down sync loop")
			return nil
		}
	}
}
