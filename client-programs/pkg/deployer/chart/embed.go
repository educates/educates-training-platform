// Package chart embeds the educates-installer Helm chart into the CLI
// binary. The chart files are copied from installer/charts/educates-installer/
// by the `make embed-installer-chart` target (and refreshed from the same
// source whenever the chart changes).
//
// The duplication is intentional: go:embed paths cannot escape the
// containing package, and committing the copy makes builds reproducible
// without a pre-build hook. The Makefile target and `make verify-installer-chart`
// catch drift.
package chart

import (
	"embed"
	"fmt"
	"io/fs"
	"strings"

	"helm.sh/helm/v4/pkg/chart/loader/archive"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	helmloader "helm.sh/helm/v4/pkg/chart/v2/loader"
)

//go:embed all:files
var chartFS embed.FS

// Name is the helm chart name. Matches files/Chart.yaml.
const Name = "educates-installer"

// Load reads the embedded operator chart and returns a parsed *chart.Chart
// ready to pass to helm install/upgrade actions.
//
// helm.sh/helm/v4 doesn't expose a fs.FS-aware loader, so we walk the
// embedded files and hand them to LoadFiles as BufferedFile entries with
// chart-root-relative names (Chart.yaml, templates/foo.yaml, ...).
func Load() (*chart.Chart, error) {
	sub, err := fs.Sub(chartFS, "files")
	if err != nil {
		return nil, fmt.Errorf("embedded chart: open files: %w", err)
	}

	var files []*archive.BufferedFile
	walkErr := fs.WalkDir(sub, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, readErr := fs.ReadFile(sub, path)
		if readErr != nil {
			return readErr
		}
		// strings.TrimPrefix is a no-op when path is already chart-root
		// relative (which fs.Sub guarantees); kept for defence in depth.
		files = append(files, &archive.BufferedFile{
			Name: strings.TrimPrefix(path, "./"),
			Data: data,
		})
		return nil
	})
	if walkErr != nil {
		return nil, fmt.Errorf("embedded chart: walk: %w", walkErr)
	}

	c, err := helmloader.LoadFiles(files)
	if err != nil {
		return nil, fmt.Errorf("embedded chart: load: %w", err)
	}
	return c, nil
}
