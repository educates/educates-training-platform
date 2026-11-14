#!/usr/bin/env bash
#
# Creates and deletes a named local cluster from a local build, for the
# harnesses in this directory. See README.md beside this script.
#
#   local-cluster.sh create <name> --listen-address <ip> --tag <tag> [--domain <domain>] [--cli <path>]
#   local-cluster.sh delete <name> [--cli <path>]
#   local-cluster.sh show <name>
#
# create writes the named cluster's config with the educates CLI, records
# that this script created it in a marker file in the cluster's folder of the
# CLI data home, and creates the cluster with the CLI. delete and show act
# only on a cluster whose folder holds that marker, so they never touch a
# local cluster this script did not create.

set -euo pipefail

SCRIPT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
REPO_ROOT=$(cd "$SCRIPT_DIR/../.." && pwd)

MARKER_FILE=harness-cluster.env

# The images make local-build pushes under its PACKAGE_VERSION, which the
# CLI built with it deploys.
BUILD_IMAGES="operator session-manager training-portal base-environment
docker-registry image-puller secrets-manager tunnel-manager image-cache
assets-server lookup-service node-ca-injector"

REGISTRY=localhost:5001

usage() {
    cat <<'EOF'
Usage:
  local-cluster.sh create <name> --listen-address <ip> --tag <tag> [--domain <domain>] [--cli <path>]
  local-cluster.sh delete <name> [--cli <path>]
  local-cluster.sh show <name>

create   Creates the named local cluster <name> from the build
         'make local-build PACKAGE_VERSION=<tag>' produced, on the host
         address --listen-address, which no other local cluster may use,
         under --domain (default: <name>.educates.test).
delete   Deletes a named local cluster this script created, and its config.
show     Prints what this script recorded when it created the cluster.

--cli    The educates CLI the build produced (default: this checkout's
         client-programs/bin/educates-<os>-<arch>). delete defaults to the
         CLI recorded at create when it still exists.
EOF
}

die() {
    echo "local-cluster.sh: $*" >&2
    exit 1
}

# default_cli prints the path of the CLI this checkout's make local-build
# writes, named as the Makefile names it.
default_cli() {
    local system machine
    system=$(uname -s | tr '[:upper:]' '[:lower:]')
    machine=$(uname -m)
    if [ "$machine" = x86_64 ]; then
        machine=amd64
    fi
    echo "$REPO_ROOT/client-programs/bin/educates-$system-$machine"
}

# data_home prints the CLI data home, resolved as the CLI resolves it.
data_home() {
    if [ -n "${EDUCATES_CLI_DATA_HOME:-}" ]; then
        echo "$EDUCATES_CLI_DATA_HOME"
    elif [ -n "${XDG_DATA_HOME:-}" ]; then
        echo "$XDG_DATA_HOME/educates"
    elif [ "$(uname -s)" = Darwin ]; then
        echo "$HOME/Library/Application Support/educates"
    else
        echo "$HOME/.local/share/educates"
    fi
}

# need_value fails unless the option $1 is followed by a value.
need_value() {
    [ $# -ge 2 ] || die "$1 needs a value"
}

check_name() {
    local name=$1
    if ! [[ "$name" =~ ^[a-z0-9]([a-z0-9-]{0,29}[a-z0-9])?$ ]]; then
        die "cluster name '$name' is not a lowercase DNS label of at most 31 characters"
    fi
}

check_cli() {
    [ -x "$1" ] || die "no educates CLI at $1; build it with 'make local-build PACKAGE_VERSION=<tag>' or pass --cli"
}

# cluster_rows prints the rows of 'educates local cluster list' as
# "<listen address> <domain> <context>", the last three columns, which hold
# no spaces, unlike the state column.
cluster_rows() {
    "$1" local cluster list 2>/dev/null | awk 'NR > 1 && NF >= 5 { print $(NF-2), $(NF-1), $NF }'
}

# kind_cluster_exists reports whether the local cluster <name> has node
# containers, as 'educates local cluster list' sees it.
kind_cluster_exists() {
    local cli=$1 name=$2
    cluster_rows "$cli" | awk -v ctx="kind-educates-$name" '$3 == ctx { found = 1 } END { exit !found }'
}

marker_value() {
    local marker=$1 key=$2
    sed -n "s/^$key=//p" "$marker" | head -n 1
}

cmd_create() {
    local name="" listen="" tag="" domain="" cli=""
    while [ $# -gt 0 ]; do
        case "$1" in
            --listen-address) need_value "$@"; listen=$2; shift 2 ;;
            --tag) need_value "$@"; tag=$2; shift 2 ;;
            --domain) need_value "$@"; domain=$2; shift 2 ;;
            --cli) need_value "$@"; cli=$2; shift 2 ;;
            -h|--help) usage; exit 0 ;;
            -*) die "unknown option $1" ;;
            *) [ -z "$name" ] || die "unexpected argument $1"; name=$1; shift ;;
        esac
    done
    [ -n "$name" ] || die "create needs a cluster name"
    [ -n "$listen" ] || die "create needs --listen-address, a host address no other local cluster uses"
    [ -n "$tag" ] || die "create needs --tag, the PACKAGE_VERSION of the local build"
    check_name "$name"
    domain=${domain:-$name.educates.test}
    cli=${cli:-$(default_cli)}
    check_cli "$cli"
    cli=$(cd "$(dirname "$cli")" && pwd)/$(basename "$cli")

    # The CLI deploys the images of its own compiled-in version, so it has to
    # be the CLI of the build named. It prints its version on stderr.
    local version
    version=$("$cli" version 2>&1)
    [ "$version" = "$tag" ] || die "$cli is the CLI of build '$version', not '$tag'"

    local missing="" image
    for image in $BUILD_IMAGES; do
        if ! curl -fsS -o /dev/null -I \
            -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.list.v2+json, application/vnd.docker.distribution.manifest.v2+json' \
            "http://$REGISTRY/v2/educates-$image/manifests/$tag" 2>/dev/null; then
            missing="$missing educates-$image"
        fi
    done
    [ -z "$missing" ] || die "the local registry $REGISTRY has no images tagged $tag for:$missing; run 'make local-build PACKAGE_VERSION=$tag'"

    local folder
    folder="$(data_home)/clusters/$name"
    if [ -e "$folder" ]; then
        if [ -f "$folder/$MARKER_FILE" ]; then
            die "local cluster '$name' already exists, created by this script; delete it first with '$0 delete $name'"
        fi
        die "local cluster '$name' already has a config at $folder that this script did not create; leaving it alone"
    fi

    local row
    row=$(cluster_rows "$cli" | awk -v ip="$listen" '$1 == ip { print $3 }' | head -n 1)
    [ -z "$row" ] || die "listen address $listen is used by the local cluster of kube context $row"

    echo "==> Writing the config of local cluster '$name' ($listen, $domain)"
    "$cli" local config init --local-cluster "$name" --listen-address "$listen" --domain "$domain"

    cat > "$folder/$MARKER_FILE" <<EOF
# Created by hack/harnesses/local-cluster.sh, whose delete command removes
# this cluster and this folder.
HARNESS_CLUSTER_NAME=$name
HARNESS_CLUSTER_TAG=$tag
HARNESS_CLUSTER_CLI=$cli
HARNESS_CLUSTER_LISTEN_ADDRESS=$listen
HARNESS_CLUSTER_DOMAIN=$domain
HARNESS_CLUSTER_CONTEXT=kind-educates-$name
HARNESS_CLUSTER_CREATED=$(date -u +%Y-%m-%dT%H:%M:%SZ)
EOF

    # kind makes the cluster it creates the current context; the previous
    # one, or none, is restored afterwards so other terminals keep their
    # cluster.
    local kubeconfig="$HOME/.kube/config" previous=""
    previous=$(kubectl --kubeconfig "$kubeconfig" config current-context 2>/dev/null || true)

    echo "==> Creating local cluster '$name' with $cli"
    if ! "$cli" local cluster create --local-cluster "$name"; then
        if kind_cluster_exists "$cli" "$name"; then
            echo "local-cluster.sh: create failed; the cluster is left for inspection: delete it with '$0 delete $name'" >&2
        else
            "$cli" local config delete --local-cluster "$name" >/dev/null 2>&1 || true
            echo "local-cluster.sh: create failed before the kind cluster existed; its config was removed" >&2
        fi
        exit 1
    fi

    if [ -z "$previous" ]; then
        kubectl --kubeconfig "$kubeconfig" config unset current-context >/dev/null 2>&1 || true
    elif [ "$previous" != "kind-educates-$name" ]; then
        kubectl --kubeconfig "$kubeconfig" config use-context "$previous" >/dev/null 2>&1 || true
    fi

    cat <<EOF

Local cluster '$name' is ready:
  kube context:   kind-educates-$name
  listen address: $listen
  domain:         $domain
  CLI:            $cli
Select it with --local-cluster $name, or export EDUCATES_LOCAL_CLUSTER=$name.
Delete it with: $0 delete $name
EOF
}

cmd_delete() {
    local name="" cli=""
    while [ $# -gt 0 ]; do
        case "$1" in
            --cli) need_value "$@"; cli=$2; shift 2 ;;
            -h|--help) usage; exit 0 ;;
            -*) die "unknown option $1" ;;
            *) [ -z "$name" ] || die "unexpected argument $1"; name=$1; shift ;;
        esac
    done
    [ -n "$name" ] || die "delete needs a cluster name"
    check_name "$name"

    local folder marker
    folder="$(data_home)/clusters/$name"
    marker="$folder/$MARKER_FILE"
    [ -d "$folder" ] || die "no local cluster '$name' to delete"
    [ -f "$marker" ] || die "local cluster '$name' was not created by this script; leaving it alone"

    if [ -z "$cli" ]; then
        cli=$(marker_value "$marker" HARNESS_CLUSTER_CLI)
        [ -x "$cli" ] || cli=$(default_cli)
    fi
    check_cli "$cli"

    if kind_cluster_exists "$cli" "$name"; then
        echo "==> Deleting local cluster '$name'"
        "$cli" local cluster delete --local-cluster "$name"
    else
        echo "==> Local cluster '$name' has no kind cluster; deleting its config only"
    fi
    "$cli" local config delete --local-cluster "$name"
    echo "Deleted local cluster '$name' and its config."
}

cmd_show() {
    local name=${1:-}
    [ -n "$name" ] || die "show needs a cluster name"
    check_name "$name"
    local marker
    marker="$(data_home)/clusters/$name/$MARKER_FILE"
    [ -f "$marker" ] || die "no local cluster '$name' created by this script"
    grep '^HARNESS_CLUSTER_' "$marker"
}

command=${1:-}
[ $# -eq 0 ] || shift
case "$command" in
    create) cmd_create "$@" ;;
    delete) cmd_delete "$@" ;;
    show) cmd_show "$@" ;;
    -h|--help|help) usage ;;
    *) usage >&2; exit 2 ;;
esac
