#!/usr/bin/env bash
# Generate the digest-pinned platform image list for a release.
#
#   hack/generate-image-list.sh <version> <registry-host> <registry-namespace> [--no-digests]
#
# Writes one fully qualified image reference per line to stdout
# (`<repo>:<tag>@sha256:<digest>`), covering everything an install of the
# platform can need (see decisions.md "Image relocation is a published
# digest-pinned list"):
#
#   - the Educates platform images at the released version (operator,
#     runtime components, image-puller, docker-registry, ...);
#   - the full session-manager image inventory (the `imageVersions`
#     helper): the workshop base, the JDK and conda workshop
#     environments, and the optional runtime images for the vcluster
#     workshop application — vcluster itself plus its loft-sh Kubernetes
#     distro images, docker-in-docker, and the debian base. Extracted by
#     rendering the session-manager chart, so version bumps in the chart
#     flow through without editing this script;
#   - the images a session's vcluster picks itself, CoreDNS and the
#     init container that rewrites stateful set pod hosts, listed in
#     VCLUSTER_OWN_IMAGES below;
#   - the upstream cluster-service images (cert-manager, Contour,
#     external-dns, Kyverno), extracted by rendering the vendored chart
#     tarballs with default values. Defaults are a superset of what the
#     operator enables, so this over-collects slightly rather than ever
#     missing an image. Schema validation is skipped for these renders,
#     since some upstream schemas require values that have no default
#     (external-dns requires `policy`) and the operator supplies them.
#
# The list is intentionally COMPLETE — it includes the JDK/conda
# environments and the vcluster application images even though most
# installs use only a subset. When mirroring, delete the entries you do
# not use rather than guessing which ones might be missing. The
# JDK/conda environment images are multi-GB each.
#
# Digest resolution uses skopeo (preinstalled on GitHub runners) and
# requires the images to already be published — the release workflow
# runs this after the image-publish jobs. Use --no-digests for a local
# dry run without registry access.
set -euo pipefail

usage() {
    echo "usage: $0 <version> <registry-host> <registry-namespace> [--no-digests]" >&2
    exit 1
}

VERSION="${1:-}"
REGISTRY_HOST="${2:-}"
REGISTRY_NAMESPACE="${3:-}"
[ -n "$VERSION" ] && [ -n "$REGISTRY_HOST" ] && [ -n "$REGISTRY_NAMESPACE" ] || usage
RESOLVE_DIGESTS=true
if [ "${4:-}" = "--no-digests" ]; then
    RESOLVE_DIGESTS=false
elif [ -n "${4:-}" ]; then
    usage
fi

cd "$(dirname "$0")/.."

VENDORED_CHARTS_DIR=installer/operator/vendored-charts
SESSION_MANAGER_CHART=installer/charts/educates-training-platform/charts/session-manager
REGISTRY_PREFIX="$REGISTRY_HOST/$REGISTRY_NAMESPACE"

# Educates-built platform images that are NOT part of the session-manager
# imageVersions inventory (the operator, the other components, and the
# image pre-puller's helper). The inventory-resident Educates images,
# such as training-portal, docker-registry, base-environment, jdk* and
# conda, come from the chart render below and are deduplicated against
# this list.
PLATFORM_IMAGES="
image-puller
session-manager
secrets-manager
lookup-service
node-ca-injector
operator
"

# Images a session's vcluster picks itself, which the session-manager
# imageVersions inventory does not pin: CoreDNS, at the version vcluster
# maps each virtual Kubernetes version the vcluster application offers to
# (CoreDNSVersionMap in pkg/constants/coredns.go of loft-sh/vcluster, at
# the loftsh-vcluster version), and the init container that rewrites the
# hosts of stateful set pods (sync.toHost.pods.rewriteHosts in
# session-manager/packages/vcluster/vcluster-all-config.yaml). With an
# image registry prefix, the session manager makes it vcluster's default
# image registry, so these land under it at their upstream path.
VCLUSTER_OWN_IMAGES="
docker.io/coredns/coredns:1.12.0
docker.io/coredns/coredns:1.12.1
docker.io/coredns/coredns:1.14.1
docker.io/coredns/coredns:1.14.2
mirror.gcr.io/library/alpine:3.20
"

# Upstream cluster-service charts the operator installs in Managed
# mode. Globs resolve the single vendored tarball per chart so version
# bumps don't touch this script.
UPSTREAM_CHART_GLOBS="
cert-manager-*.tgz
contour-*.tgz
external-dns-*.tgz
kyverno-*.tgz
"

emit() {
    local ref="$1"
    if $RESOLVE_DIGESTS; then
        local digest
        # --override-os keeps darwin hosts working; .Digest is the
        # multi-arch index digest (verified equal to the raw manifest
        # sha256), which is what a relocation copy should pin.
        digest=$(skopeo inspect --override-os linux --format '{{.Digest}}' "docker://$ref") || {
            echo "failed to resolve digest for $ref" >&2
            exit 1
        }
        echo "$ref@$digest"
    else
        echo "$ref"
    fi
}

# Render a chart, leaving helm's stderr visible and exiting on failure so
# a chart that no longer renders cannot yield a short or empty list.
render_chart() {
    local chart="$1"
    shift
    helm template image-list-probe "$chart" "$@" || {
        echo "failed to render chart $chart" >&2
        exit 1
    }
}

# Image references appear in rendered manifests as `image:` fields and,
# for cert-manager's acmesolver, as a `--*-image=` controller argument.
extract_chart_images() {
    local tarball="$1"
    render_chart "$tarball" --skip-schema-validation |
        grep -ohE '(image: *"?[^"[:space:]]+"?|--[a-z0-9-]*image=[^"[:space:]]+)' |
        sed -E 's/^image: *"?//; s/"$//; s/^--[a-z0-9-]*image=//'
}

# Render the session-manager chart and pull every image reference out of
# the imageVersions inventory (and the chart's own pod images). The
# registry prefix is forced to the release's host/namespace so the
# Educates-built entries resolve to the published location; clusterIngress
# .domain is a required value that does not affect image refs.
render_session_manager_images() {
    render_chart "$SESSION_MANAGER_CHART" \
        --set clusterIngress.domain=image-list-probe.invalid \
        --set development.imageRegistry.host="$REGISTRY_HOST" \
        --set development.imageRegistry.namespace="$REGISTRY_NAMESPACE" |
        grep -ohE 'image: *"?[^"[:space:]]+"?' |
        sed -E 's/^image: *"?//; s/"$//'
}

all_refs=""

for name in $PLATFORM_IMAGES; do
    all_refs+="$REGISTRY_PREFIX/educates-$name:$VERSION"$'\n'
done

# Captured before the loop rather than read through process substitution,
# whose exit status set -e never sees, so a failed render stops the script.
session_manager_refs=$(render_session_manager_images)

# Educates-built inventory entries render at the chart's appVersion; pin
# them to the release VERSION instead. External entries (vcluster,
# loft-sh Kubernetes, the vcluster Contour/Envoy, docker-in-docker,
# debian) pass through verbatim.
while read -r ref; do
    [ -n "$ref" ] || continue
    case "$ref" in
        "$REGISTRY_PREFIX"/*) all_refs+="${ref%:*}:$VERSION"$'\n' ;;
        *) all_refs+="$ref"$'\n' ;;
    esac
done <<< "$session_manager_refs"

for ref in $VCLUSTER_OWN_IMAGES; do
    all_refs+="$ref"$'\n'
done

for glob in $UPSTREAM_CHART_GLOBS; do
    # shellcheck disable=SC2086 -- glob expansion is the point
    set -- $VENDORED_CHARTS_DIR/$glob
    [ -f "$1" ] || {
        echo "no vendored chart matching $glob in $VENDORED_CHARTS_DIR" >&2
        exit 1
    }
    all_refs+="$(extract_chart_images "$1")"$'\n'
done

while read -r ref; do
    [ -n "$ref" ] || continue
    emit "$ref"
done < <(echo "$all_refs" | sort -u)
