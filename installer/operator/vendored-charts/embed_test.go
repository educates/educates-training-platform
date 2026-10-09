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

package vendoredcharts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCertManager_Embedded asserts the //go:embed directive resolved to
// a usable chart at build time. Catches the "tarball renamed but the
// embed path wasn't updated" failure at test time rather than at
// operator startup.
func TestCertManager_Embedded(t *testing.T) {
	chrt, err := CertManager()
	if err != nil {
		t.Fatalf("CertManager: %v", err)
	}
	if got, want := chrt.Metadata.Name, "cert-manager"; got != want {
		t.Errorf("chart name = %q, want %q", got, want)
	}
	if got, want := chrt.Metadata.AppVersion, CertManagerVersion; got != want {
		t.Errorf("chart appVersion = %q, want CertManagerVersion %q", got, want)
	}
}

func TestContour_Embedded(t *testing.T) {
	chrt, err := Contour()
	if err != nil {
		t.Fatalf("Contour: %v", err)
	}
	if got, want := chrt.Metadata.Name, "contour"; got != want {
		t.Errorf("chart name = %q, want %q", got, want)
	}
	if got, want := chrt.Metadata.Version, ContourChartVersion; got != want {
		t.Errorf("chart version = %q, want ContourChartVersion %q", got, want)
	}
	if got, want := chrt.Metadata.AppVersion, ContourAppVersion; got != want {
		t.Errorf("chart appVersion = %q, want ContourAppVersion %q", got, want)
	}
}

func TestExternalDNS_Embedded(t *testing.T) {
	chrt, err := ExternalDNS()
	if err != nil {
		t.Fatalf("ExternalDNS: %v", err)
	}
	if got, want := chrt.Metadata.Name, "external-dns"; got != want {
		t.Errorf("chart name = %q, want %q", got, want)
	}
	if got, want := chrt.Metadata.Version, ExternalDNSChartVersion; got != want {
		t.Errorf("chart version = %q, want ExternalDNSChartVersion %q", got, want)
	}
	if got, want := chrt.Metadata.AppVersion, ExternalDNSAppVersion; got != want {
		t.Errorf("chart appVersion = %q, want ExternalDNSAppVersion %q", got, want)
	}
}

func TestKyverno_Embedded(t *testing.T) {
	chrt, err := Kyverno()
	if err != nil {
		t.Fatalf("Kyverno: %v", err)
	}
	if got, want := chrt.Metadata.Name, "kyverno"; got != want {
		t.Errorf("chart name = %q, want %q", got, want)
	}
	if got, want := chrt.Metadata.Version, KyvernoChartVersion; got != want {
		t.Errorf("chart version = %q, want KyvernoChartVersion %q", got, want)
	}
	if got, want := chrt.Metadata.AppVersion, KyvernoAppVersion; got != want {
		t.Errorf("chart appVersion = %q, want KyvernoAppVersion %q", got, want)
	}
}

// upstreamChartVersions maps each chart in the Makefile's
// VENDORED_CHARTS list to the version constant embed.go pins for it.
// Adding a new upstream chart means adding it in three places: the
// Makefile, embed.go, and this map.
var upstreamChartVersions = map[string]string{
	"cert-manager": CertManagerVersion,
	"contour":      ContourChartVersion,
	"external-dns": ExternalDNSChartVersion,
	"kyverno":      KyvernoChartVersion,
}

// runtimeSubchartFiles are the tarballs packaged from the in-repo
// Educates charts. They live in this directory but are gitignored build
// output of `make package-local-charts`, not `make vendor-charts`, so they
// are intentionally absent from SHA256SUMS and the Makefile list.
var runtimeSubchartFiles = map[string]bool{
	"secrets-manager-" + SecretsManagerChartVersion + ".tgz":  true,
	"lookup-service-" + LookupServiceChartVersion + ".tgz":    true,
	"session-manager-" + SessionManagerChartVersion + ".tgz":  true,
	"node-ca-injector-" + NodeCAInjectorChartVersion + ".tgz": true,
	"remote-access-" + RemoteAccessChartVersion + ".tgz":      true,
}

// parseVendoredChartsMakefile extracts the name=version pairs from the
// VENDORED_CHARTS variable in the operator Makefile.
func parseVendoredChartsMakefile(t *testing.T) map[string]string {
	t.Helper()

	data, err := os.ReadFile("../Makefile")
	if err != nil {
		t.Fatalf("read ../Makefile: %v", err)
	}

	entries := map[string]string{}
	inBlock := false
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.HasPrefix(line, "VENDORED_CHARTS :=") {
			inBlock = true
			continue
		}
		if !inBlock {
			continue
		}
		entry := strings.TrimSuffix(strings.TrimSpace(line), "\\")
		entry = strings.TrimSpace(entry)
		if entry == "" {
			break
		}
		parts := strings.SplitN(entry, "=", 3)
		if len(parts) != 3 {
			t.Fatalf("malformed VENDORED_CHARTS entry %q", entry)
		}
		entries[parts[0]] = parts[1]
		if !strings.HasSuffix(strings.TrimSpace(line), "\\") {
			break
		}
	}
	if len(entries) == 0 {
		t.Fatal("found no VENDORED_CHARTS entries in ../Makefile")
	}
	return entries
}

// parseSHA256SUMS returns the set of filenames recorded in SHA256SUMS.
func parseSHA256SUMS(t *testing.T) map[string]bool {
	t.Helper()

	data, err := os.ReadFile("SHA256SUMS")
	if err != nil {
		t.Fatalf("read SHA256SUMS: %v", err)
	}

	files := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			files[fields[1]] = true
		}
	}
	return files
}

// TestVendoredCharts_DirectoryConsistent ties the three places an
// upstream chart version lives — the Makefile VENDORED_CHARTS list,
// SHA256SUMS, and embed.go — to each other and to the tarballs on
// disk. It is what makes a half-finished chart bump (Makefile and
// SHA256SUMS updated, embed.go still on the old version, stale
// tarball left behind) fail CI instead of silently shipping the old
// chart.
func TestVendoredCharts_DirectoryConsistent(t *testing.T) {
	makefile := parseVendoredChartsMakefile(t)
	sums := parseSHA256SUMS(t)

	for name, version := range makefile {
		embedded, known := upstreamChartVersions[name]
		if !known {
			t.Errorf("chart %q is in the Makefile but has no entry in "+
				"upstreamChartVersions (add it to embed.go and this test)", name)
			continue
		}
		if embedded != version {
			t.Errorf("chart %q: Makefile pins %s but embed.go pins %s — finish "+
				"the upgrade (update embed.go's constant and //go:embed directive)",
				name, version, embedded)
		}
		file := name + "-" + version + ".tgz"
		if !sums[file] {
			t.Errorf("chart %q: %s is not recorded in SHA256SUMS", name, file)
		}
		if _, err := os.Stat(file); err != nil {
			t.Errorf("chart %q: %s missing on disk — run `make vendor-charts`", name, file)
		}
	}

	for name := range upstreamChartVersions {
		if _, ok := makefile[name]; !ok {
			t.Errorf("chart %q is embedded but missing from the Makefile VENDORED_CHARTS list", name)
		}
	}

	for file := range sums {
		name := strings.TrimSuffix(file, ".tgz")
		matched := false
		for chartName, version := range makefile {
			if name == chartName+"-"+version {
				matched = true
				break
			}
		}
		if !matched {
			t.Errorf("SHA256SUMS records %s which no Makefile VENDORED_CHARTS entry produces — remove the stale line", file)
		}
	}

	tarballs, err := filepath.Glob("*.tgz")
	if err != nil {
		t.Fatalf("glob tarballs: %v", err)
	}
	for _, file := range tarballs {
		if sums[file] || runtimeSubchartFiles[file] {
			continue
		}
		t.Errorf("stale vendored tarball %s — neither SHA256SUMS nor the "+
			"embedded runtime subcharts reference it; `git rm` it if it is an "+
			"upstream chart, or re-run `make package-local-charts` if it is a "+
			"leftover build of a runtime subchart", file)
	}
}
