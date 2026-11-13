package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/educates/educates-training-platform/client-programs/pkg/localclusters"
	"github.com/educates/educates-training-platform/client-programs/pkg/utils"
)

// renderWithDefault renders the resolver configuration of the current
// data home, with no kind cluster running, serving the default local
// cluster from def.
func renderWithDefault(t *testing.T, def localclusters.ResolverInput) string {
	t.Helper()
	inv := localclusters.NewInventory(localclusters.Observed{}, utils.GetEducatesHomeDir())
	content, err := inv.ResolverConfig("192.168.1.10", def)
	if err != nil {
		t.Fatalf("ResolverConfig: %v", err)
	}
	return string(content)
}

func wantLines(t *testing.T, content string, lines ...string) {
	t.Helper()
	for _, line := range lines {
		if !strings.Contains(content, "\n"+line+"\n") {
			t.Errorf("resolver config has no line %q:\n%s", line, content)
		}
	}
}

func TestDefaultResolverConfig_EscapeConfig_ServesItsIngressDomain(t *testing.T) {
	t.Setenv("EDUCATES_CLI_DATA_HOME", t.TempDir())
	path := writeFixture(t, `apiVersion: cli.educates.dev/v1alpha1
kind: EducatesConfig
target:
  provider: kind
  resolver:
    targetAddress: 192.168.50.50
    extraDomains:
    - extra.example.test
educatesClusterConfig:
  mode: Managed
  ingress:
    domain: workshops.example.test
    ingressClassName: contour
    controller:
      provider: BundledContour
    certificates:
      provider: None
`)

	def, err := defaultResolverConfig(path, "")
	if err != nil {
		t.Fatalf("defaultResolverConfig: %v", err)
	}

	wantLines(t, renderWithDefault(t, def),
		"address=/workshops.example.test/192.168.50.50",
		"address=/extra.example.test/192.168.50.50")
}

func TestDefaultResolverConfig_Domain_OverridesTheDataHomeConfigDomain(t *testing.T) {
	dataHome := t.TempDir()
	t.Setenv("EDUCATES_CLI_DATA_HOME", dataHome)
	if err := os.WriteFile(filepath.Join(dataHome, "config.yaml"), []byte(
		"apiVersion: cli.educates.dev/v1alpha1\nkind: EducatesLocalConfig\ningress:\n  domain: educates.test\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	def, err := defaultResolverConfig("", "mydomain.test")
	if err != nil {
		t.Fatalf("defaultResolverConfig: %v", err)
	}

	content := renderWithDefault(t, def)
	wantLines(t, content, "address=/mydomain.test/192.168.1.10")
	if strings.Contains(content, "educates.test") {
		t.Errorf("resolver config still serves the overridden domain:\n%s", content)
	}
}

func TestDefaultResolverConfig_NoLocalConfig_SaysHowToWriteOne(t *testing.T) {
	t.Setenv("EDUCATES_CLI_DATA_HOME", filepath.Join(t.TempDir(), "missing"))

	_, err := defaultResolverConfig("", "")

	if err == nil || !strings.Contains(err.Error(), "educates local config init") {
		t.Fatalf("defaultResolverConfig error = %v, want the missing local config error", err)
	}
}
