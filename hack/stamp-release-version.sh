#!/usr/bin/env bash
# Stamp a release version across the v4 install charts and the copies
# embedded in the operator image and the CLI.
#
#   hack/stamp-release-version.sh <version> <registry-host> <registry-namespace> [--charts-only]
#
# The committed tree carries a development version (and upstream-default
# registry annotations); real versions are stamped at publish time from
# the git tag by the release workflow — nothing is committed back. See
# decisions.md "Release versions are stamped in CI at publish time".
#
# What gets stamped:
#   - educates-installer + educates-training-platform (umbrella) +
#     the five runtime subcharts: Chart.yaml version, appVersion, and
#     the educates.dev/image-registry-{host,namespace} annotations
#     (only where a chart already carries them).
#   - The umbrella's dependencies[].version pins.
#   - The CLI's embedded operator chart (regenerated from the stamped
#     educates-installer chart, same as `make embed-installer-chart`).
#
# Full mode (default) additionally rebuilds what the operator image
# embeds — requires `helm` on PATH:
#   - Repackages the five runtime subcharts into
#     installer/operator/vendored-charts/ at the stamped version.
#   - Rewrites the //go:embed filenames and <X>ChartVersion constants
#     in vendored-charts/embed.go (local subcharts only; upstream
#     charts stay pinned).
# The repackaged tarballs are gitignored build output, so only the
# embed.go rewrite is committed by release-prep. They get no SHA256SUMS
# entries: that file pins the vendored upstream charts only, and helm
# package does not produce the same bytes twice, so a recorded checksum
# could never be verified.
#
# --charts-only skips the operator-image pieces; it needs only perl, so
# the macOS CLI build runners can use it.
set -euo pipefail

usage() {
    echo "usage: $0 <version> <registry-host> <registry-namespace> [--charts-only]" >&2
    exit 1
}

VERSION="${1:-}"
REGISTRY_HOST="${2:-}"
REGISTRY_NAMESPACE="${3:-}"
[ -n "$VERSION" ] && [ -n "$REGISTRY_HOST" ] && [ -n "$REGISTRY_NAMESPACE" ] || usage
CHARTS_ONLY=false
if [ "${4:-}" = "--charts-only" ]; then
    CHARTS_ONLY=true
elif [ -n "${4:-}" ]; then
    usage
fi

cd "$(dirname "$0")/.."

INSTALLER_CHART=installer/charts/educates-installer
UMBRELLA_CHART=installer/charts/educates-training-platform
VENDORED_CHARTS_DIR=installer/operator/vendored-charts
EMBED_GO=$VENDORED_CHARTS_DIR/embed.go
EMBEDDED_CLI_CHART=client-programs/pkg/deployer/chart/files
LOCAL_SUBCHARTS="secrets-manager lookup-service session-manager node-ca-injector remote-access"

# Maps a subchart name to its version constant in embed.go.
chart_version_const() {
    case "$1" in
        secrets-manager)  echo SecretsManagerChartVersion ;;
        lookup-service)   echo LookupServiceChartVersion ;;
        session-manager)  echo SessionManagerChartVersion ;;
        node-ca-injector) echo NodeCAInjectorChartVersion ;;
        remote-access)    echo RemoteAccessChartVersion ;;
        *) echo "unknown subchart $1" >&2; exit 1 ;;
    esac
}

# Stamps version, appVersion and (where present) the image-registry
# annotations of one Chart.yaml. Line-level perl keeps this portable
# (no yq on the macOS runners) and preserves comments.
stamp_chart_yaml() {
    local file="$1"
    perl -pi -e "s|^version: .*|version: ${VERSION}|" "$file"
    perl -pi -e "s|^appVersion: .*|appVersion: \"${VERSION}\"|" "$file"
    perl -pi -e "s|^(\s+)educates\.dev/image-registry-host: .*|\${1}educates.dev/image-registry-host: \"${REGISTRY_HOST}\"|" "$file"
    perl -pi -e "s|^(\s+)educates\.dev/image-registry-namespace: .*|\${1}educates.dev/image-registry-namespace: \"${REGISTRY_NAMESPACE}\"|" "$file"
}

echo ">> stamping charts to version ${VERSION} (registry ${REGISTRY_HOST}/${REGISTRY_NAMESPACE})"

stamp_chart_yaml "$INSTALLER_CHART/Chart.yaml"
stamp_chart_yaml "$UMBRELLA_CHART/Chart.yaml"
# The umbrella's only indented `version:` lines are its dependency pins.
perl -pi -e "s|^(\s+)version: .*|\${1}version: ${VERSION}|" "$UMBRELLA_CHART/Chart.yaml"
for name in $LOCAL_SUBCHARTS; do
    stamp_chart_yaml "$UMBRELLA_CHART/charts/$name/Chart.yaml"
done

echo ">> refreshing embedded CLI chart from $INSTALLER_CHART"
rm -rf "$EMBEDDED_CLI_CHART"
mkdir -p "$EMBEDDED_CLI_CHART"
cp -r "$INSTALLER_CHART/." "$EMBEDDED_CLI_CHART/"

if $CHARTS_ONLY; then
    echo ">> --charts-only: skipping operator vendored-charts rebuild"
    exit 0
fi

echo ">> repackaging runtime subcharts into $VENDORED_CHARTS_DIR"
for name in $LOCAL_SUBCHARTS; do
    rm -f "$VENDORED_CHARTS_DIR/$name-"*.tgz
    helm package "$UMBRELLA_CHART/charts/$name" --destination "$VENDORED_CHARTS_DIR" >/dev/null
    [ -f "$VENDORED_CHARTS_DIR/$name-$VERSION.tgz" ] || {
        echo "helm package did not produce $name-$VERSION.tgz" >&2
        exit 1
    }
done

echo ">> rewriting embed.go filenames and version constants"
for name in $LOCAL_SUBCHARTS; do
    const=$(chart_version_const "$name")
    perl -pi -e "s|^//go:embed ${name}-.*\.tgz$|//go:embed ${name}-${VERSION}.tgz|" "$EMBED_GO"
    perl -pi -e "s|^const ${const} = \".*\"|const ${const} = \"${VERSION}\"|" "$EMBED_GO"
done
# Every //go:embed target must exist on disk or the operator image
# build fails later with a far less obvious error.
while read -r tarball; do
    [ -f "$VENDORED_CHARTS_DIR/$tarball" ] || {
        echo "embed.go references $tarball but it is missing from $VENDORED_CHARTS_DIR" >&2
        exit 1
    }
done < <(perl -ne 'print "$1\n" if m|^//go:embed (.*\.tgz)$|' "$EMBED_GO")

echo ">> done"
