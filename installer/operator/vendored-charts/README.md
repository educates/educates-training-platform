# Vendored upstream Helm charts

This directory holds upstream Helm charts the v4 operator drives via the
Helm SDK. The bytes are checked into the repository so that:

- chart-version bumps are deliberate, reviewable git events, not whatever
  the upstream registry happens to serve at install time;
- the operator can install in air-gapped environments without runtime
  registry access;
- image-relocation rewrites have a single point of control.

## Layout

```
installer/operator/vendored-charts/
├── README.md                    (this file)
├── SHA256SUMS                   (one line per upstream tarball: <hash>  <filename>)
├── embed.go                     (//go:embed directives + version constants)
├── embed_test.go                (load tests + directory-consistency test)
└── <name>-<version>.tgz         (one tarball per chart)
```

The directory lives inside the operator Go module so the operator can
`//go:embed` the tarballs directly into its binary; `embed.go` in this
directory is what the reconcilers import.

Two kinds of tarballs live here:

- **Upstream cluster-service charts** (cert-manager, contour,
  external-dns, kyverno). Listed in the `VENDORED_CHARTS` variable in
  `installer/operator/Makefile`, integrity-recorded in `SHA256SUMS`,
  downloaded by `make vendor-charts`.
- **Runtime subchart tarballs** (secrets-manager, lookup-service,
  session-manager, node-ca-injector, remote-access). Repackaged from the
  in-repo `educates-training-platform` chart sources; *not* covered by
  `SHA256SUMS` or `VENDORED_CHARTS`. At release time the publish
  workflow re-stamps them via `hack/stamp-release-version.sh`.

## Current contents (upstream charts)

| Chart | Chart version | App version | Upstream | Used by |
|---|---|---|---|---|
| cert-manager | v1.20.3 | v1.20.3 | https://charts.jetstack.io | EducatesClusterConfig (BundledCertManager) |
| contour | 0.7.0 | 1.33.6 | https://github.com/projectcontour/helm-charts | EducatesClusterConfig (BundledContour) |
| external-dns | 1.21.1 | 0.21.0 | https://github.com/kubernetes-sigs/external-dns | EducatesClusterConfig (BundledExternalDNS) |
| kyverno | 3.8.1 | v1.18.1 | https://kyverno.github.io/kyverno | EducatesClusterConfig (BundledKyverno) |

## Upgrading (or adding) an upstream chart

An upgrade touches several files which must move together. Worked
example for contour `0.6.0` → `0.7.0`; an addition is the same plus a
new accessor in `embed.go` and a new entry in `upstreamChartVersions`
in `embed_test.go`.

1. **Compute the new tarball's hash** out-of-band from the upstream
   release URL. This is the "I am deliberately trusting these bytes"
   step:

   ```
   curl -sSfL https://github.com/projectcontour/helm-charts/releases/download/contour-0.7.0/contour-0.7.0.tgz | shasum -a 256
   ```

2. **`installer/operator/Makefile`** — update the chart's
   `VENDORED_CHARTS` entry (version and URL).

3. **`SHA256SUMS`** — replace the chart's line with the new filename
   and the hash from step 1. (`make vendor-charts` refuses to download
   anything whose hash isn't already recorded.)

4. **`make vendor-charts`** (from `installer/operator/`) — downloads,
   verifies, and writes the new tarball into this directory.

5. **`embed.go`** — update the chart's `//go:embed` directive to the
   new filename, the `<X>ChartVersion` constant, and the
   `<X>AppVersion` constant (read `appVersion` from the new tarball's
   `Chart.yaml`). This is the load-bearing step: it determines what the
   operator actually installs and reports in
   `status.bundledChartVersions`.

6. **Remove the old tarball** — `git rm <name>-<old-version>.tgz`.
   Nothing prunes it automatically.

7. **Re-verify reconciler assumptions.** The reconcilers gate readiness
   on workload names and webhook behavior verified against a specific
   chart version (see e.g. the constants in
   `internal/controller/config/contour.go` and `kyverno.go`). Render
   the new chart (`helm template <release-name> ./<name>-<version>.tgz`)
   and confirm the expected Deployments/DaemonSets still come out with
   the same names; update the reconciler constants and their
   "verified against" comments if not.

8. **Regenerate the operator's fine-grained install RBAC.** The operator
   installs the vendored charts under its own ServiceAccount, so its
   `educates:installer:charts` ClusterRole must cover every resource kind
   the charts produce. Run `make generate-installer-rbac` (repo root): it
   renders the vendored charts and rebuilds
   `installer/charts/educates-installer/templates/rbac/charts-role.yaml`,
   failing if a chart now emits a Kind not in the curated map (add it to
   `hack/generate-installer-rbac.sh`). Then `make embed-installer-chart`
   to sync the CLI copy. `make ci-operator` enforces both.

9. **Update the version table above.**

10. **`make test`** — `TestVendoredCharts_DirectoryConsistent` in
    `embed_test.go` ties the Makefile, `SHA256SUMS`, `embed.go` and the
    tarballs on disk together, so a half-finished upgrade (or a stale
    leftover tarball) fails the test suite and CI. The per-chart load
    tests also assert the embedded chart's version matches the
    constants.

11. **Commit everything together**: new tarball, old tarball removal,
    `SHA256SUMS`, Makefile, `embed.go`, any reconciler updates, the
    regenerated `charts-role.yaml`, and this README.

Chart-specific follow-ups:

- **kyverno — finding the chart version.** Kyverno's Helm chart version
  and its binary (`appVersion`) are on **different numbering tracks**
  and the two are *not* aligned: the
  [kyverno/kyverno releases](https://github.com/kyverno/kyverno/releases)
  page shows only the binary version (e.g. `v1.18.1`), while the chart
  version (e.g. `3.8.1` — the `<VER>` in the
  `https://kyverno.github.io/kyverno/kyverno-<VER>.tgz` download URL)
  lives only in the Helm repo's `index.yaml`. Resolve the chart version
  that ships a given Kyverno binary from the repo index before
  downloading:

  ```
  # Which chart version ships Kyverno binary v1.18.1? (stable rows only)
  curl -sSfL https://kyverno.github.io/kyverno/index.yaml \
    | yq -r '.entries.kyverno[]
        | select(.appVersion | test("rc")|not)
        | .version + "  =>  " + .appVersion' \
    | grep ' v1.18.1$'        # → 3.8.1  =>  v1.18.1
  ```

  Drop the `grep` (and `| head`) to list the newest stable charts and the
  binary each bundles. The Helm CLI equivalent is `helm search repo
  kyverno/kyverno --versions` after `helm repo add kyverno
  https://kyverno.github.io/kyverno/`. The resolved chart version is the
  `<VER>` used everywhere in the upgrade steps above; the matching
  `appVersion` becomes `KyvernoAppVersion` (confirm it from the
  downloaded tarball's `Chart.yaml`).

- **kyverno — bundled policies.** The session-manager chart bundles
  policies vendored from
  [kyverno/policies](https://github.com/kyverno/policies) at the
  release branch matching `KyvernoAppVersion`. Re-vendor them as part
  of the bump (see
  `installer/charts/educates-training-platform/charts/session-manager/files/kyverno-policies/README.md`),
  then repackage the session-manager tarball into this directory with
  `make package-local-charts`.

The intent is "we ship the upstream chart unmodified" — never edit a
vendored tarball or unpack-and-repack it. If a change to the chart is
needed, it goes upstream first or is applied via Helm values at
install time, not by patching the bytes.
