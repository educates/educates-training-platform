# shellcheck shell=bash
#
# Functions kit.sh runs a kit with: selecting the cluster, applying a kit's
# manifests with its labels, the record of each scenario's state, and
# checking the lines of an expect or requires file. See README.md beside
# this file.
#
# It runs under Bash 3.2, as macOS ships it, so it uses no associative
# arrays and never expands an array that may be empty.

# The labels the runner stamps on everything a kit applies.
KIT_LABEL=educates.dev/kit
KIT_SCENARIO_LABEL=educates.dev/kit-scenario

# The record of each scenario's state is a ConfigMap in this namespace,
# named educates-kit-<kit>.
KIT_RECORD_NAMESPACE=default

# The paths an expect line may name by alias. A kit's kit.conf adds its own
# as KIT_PATH_<alias>.
KIT_PATH_ready='{.status.conditions[?(@.type=="Ready")].status}'
KIT_PATH_reason='{.status.conditions[?(@.type=="Ready")].reason}'
KIT_PATH_message='{.status.conditions[?(@.type=="Ready")].message}'

die() {
    echo "kit.sh: $*" >&2
    exit 2
}

say() {
    echo "$*"
}

# trim prints $1 without leading and trailing white space.
trim() {
    local s=$1
    s=${s#"${s%%[![:space:]]*}"}
    s=${s%"${s##*[![:space:]]}"}
    printf '%s' "$s"
}

# kit_tmp prints a directory for this run's temporary files, removed when
# kit.sh exits.
kit_tmp() {
    if [ -z "${KIT_TMP:-}" ]; then
        die "internal error: KIT_TMP is not set"
    fi
    printf '%s' "$KIT_TMP"
}

# print_table prints tab-separated lines from stdin as aligned columns, the
# first line being the headings. The last column is not padded, so a long
# message does not widen the others.
print_table() {
    awk 'BEGIN { FS = "\t" }
        {
            rows = NR; n[NR] = NF
            for (i = 1; i <= NF; i++) {
                cell[NR, i] = $i
                if (i < NF && length($i) > width[i]) width[i] = length($i)
            }
        }
        END {
            for (r = 1; r <= rows; r++) {
                line = ""
                for (i = 1; i < n[r]; i++) line = line sprintf("%-" width[i] "s   ", cell[r, i])
                print line cell[r, n[r]]
            }
        }'
}

# ---------------------------------------------------------------------------
# Selecting the cluster

# default_cli prints the educates CLI to list local clusters with: educates
# on PATH, else the one this checkout's make local-build writes.
default_cli() {
    if command -v educates >/dev/null 2>&1; then
        command -v educates
        return
    fi
    local system machine
    system=$(uname -s | tr '[:upper:]' '[:lower:]')
    machine=$(uname -m)
    if [ "$machine" = x86_64 ]; then
        machine=amd64
    fi
    printf '%s' "$KITS_DIR/../../client-programs/bin/educates-$system-$machine"
}

# local_cluster_state prints the STATE of the row whose CONTEXT is $2 in the
# table 'educates local cluster list' prints, read with the CLI $1, or
# nothing when no row has that context. The table's columns are set apart by
# two spaces or more, and a value may hold a single space ("partly
# running"), so each value is taken from the column under its heading.
local_cluster_state() {
    local cli=$1 context=$2 listing
    listing=$("$cli" local cluster list) || die "cannot list the local clusters with $cli"
    awk -v context="$context" '
        function trim(s) { sub(/^[ \t]+/, "", s); sub(/[ \t]+$/, "", s); return s }
        NR == 1 {
            n = split(trim($0), heading, "  +")
            for (i = 1; i <= n; i++) column[heading[i]] = i
            next
        }
        {
            m = split(trim($0), cell, "  +")
            if (m != n || m < 5) next
            if (cell[column["CONTEXT"]] == context) { print cell[column["STATE"]]; exit }
        }' <<<"$listing"
}

# select_cluster sets KIT_CONTEXT, the arguments every kubectl call takes,
# KIT_KUBECTL_ARGS, and the option that selects the cluster again in the
# commands kit.sh suggests, KIT_CLUSTER_FLAG, from the options kit.sh read:
# --context names any kube context, read from $KUBECONFIG; otherwise
# --local-cluster, or $EDUCATES_LOCAL_CLUSTER, names a local cluster, the
# default one when empty, which must be running, as 'educates local cluster
# list' shows it.
select_cluster() {
    if [ -n "$OPT_CONTEXT" ]; then
        [ "$OPT_LOCAL_CLUSTER_GIVEN" = false ] || die "--context and --local-cluster can't be given together"
        kubectl config get-contexts "$OPT_CONTEXT" -o name >/dev/null 2>&1 ||
            die "no kube context '$OPT_CONTEXT' in ${KUBECONFIG:-~/.kube/config}"
        KIT_CONTEXT=$OPT_CONTEXT
        KIT_CLUSTER_FLAG="--context $KIT_CONTEXT"
        KIT_KUBECTL_ARGS=(--context "$KIT_CONTEXT" --request-timeout=30s)
        KIT_CLUSTER_DESCRIPTION="kube context $KIT_CONTEXT"
    else
        local name cli state
        if [ "$OPT_LOCAL_CLUSTER_GIVEN" = true ]; then
            name=$OPT_LOCAL_CLUSTER
        else
            name=${EDUCATES_LOCAL_CLUSTER:-}
        fi
        KIT_CLUSTER_FLAG=""
        if [ "$OPT_LOCAL_CLUSTER_GIVEN" = true ] || [ -n "$name" ]; then
            KIT_CLUSTER_FLAG="--local-cluster ${name:-''}"
        fi
        if [ -n "$name" ]; then
            KIT_CONTEXT=kind-educates-$name
            KIT_CLUSTER_DESCRIPTION="local cluster $name (kube context $KIT_CONTEXT)"
        else
            KIT_CONTEXT=kind-educates
            KIT_CLUSTER_DESCRIPTION="the default local cluster (kube context $KIT_CONTEXT)"
        fi
        cli=${OPT_CLI:-$(default_cli)}
        [ -x "$cli" ] || die "no educates CLI at $cli to list the local clusters with; pass --cli, or --context for a cluster of any kind"
        state=$(local_cluster_state "$cli" "$KIT_CONTEXT")
        [ -n "$state" ] || die "$KIT_CLUSTER_DESCRIPTION is not in 'educates local cluster list'"
        [ "$state" = running ] || die "$KIT_CLUSTER_DESCRIPTION is $state, not running"
        # The CLI writes every local cluster's context into this file,
        # whatever KUBECONFIG says.
        KIT_KUBECTL_ARGS=(--kubeconfig "$HOME/.kube/config" --context "$KIT_CONTEXT" --request-timeout=30s)
    fi
    kube get namespace "$KIT_RECORD_NAMESPACE" -o name >/dev/null ||
        die "cannot reach $KIT_CLUSTER_DESCRIPTION"
}

# kube runs kubectl against the selected cluster.
kube() {
    command kubectl "${KIT_KUBECTL_ARGS[@]}" "$@"
}

# ---------------------------------------------------------------------------
# Applying a kit's manifests

# stamped_apply applies the manifests $2..., with the kit's label and the
# scenario label $1 added to every resource, through kubectl's built-in
# kustomize, so the manifests themselves stay plain.
stamped_apply() {
    local scenario=$1
    shift
    local dir resources="" file i=0 status=0
    dir=$(mktemp -d "$(kit_tmp)/apply.XXXXXX")
    for file in "$@"; do
        i=$((i + 1))
        cp "$file" "$dir/manifest-$i.yaml"
        resources="$resources- manifest-$i.yaml
"
    done
    cat >"$dir/kustomization.yaml" <<EOF
apiVersion: kustomize.config.k8s.io/v1beta1
kind: Kustomization
resources:
$resources
labels:
- pairs:
    $KIT_LABEL: $KIT_NAME
    $KIT_SCENARIO_LABEL: $scenario
EOF
    kube apply -k "$dir" || status=$?
    rm -rf "$dir"
    return "$status"
}

# kit_apply applies the manifests named, or the manifest on stdin when none
# is, with the labels of the kit and of the scenario whose break.sh or
# fix.sh calls it, so that the kit's status shows what it creates and its
# delete removes it.
kit_apply() {
    if [ $# -eq 0 ]; then
        local file
        file=$(mktemp "$(kit_tmp)/stdin.XXXXXX")
        cat >"$file"
        stamped_apply "$KIT_SCENARIO" "$file"
    else
        stamped_apply "$KIT_SCENARIO" "$@"
    fi
}

# ---------------------------------------------------------------------------
# The record of each scenario's state

# record_load reads the kit's record into KIT_RECORD_DATA, as lines
# "<scenario>=<state>".
record_load() {
    KIT_RECORD_DATA=$(kube -n "$KIT_RECORD_NAMESPACE" get configmap "$KIT_RECORD" --ignore-not-found \
        -o go-template='{{range $k, $v := .data}}{{$k}}={{$v}}{{"\n"}}{{end}}') ||
        die "cannot read the kit's record, ConfigMap $KIT_RECORD_NAMESPACE/$KIT_RECORD"
}

# record_state prints the recorded state of the scenario $1, or nothing when
# it is not applied.
record_state() {
    awk -F= -v scenario="$1" '$1 == scenario { print $2; exit }' <<<"$KIT_RECORD_DATA"
}

# record_set records the state $1 for the scenarios $2...
record_set() {
    local state=$1
    shift
    local data="" scenario exists
    for scenario in "$@"; do
        data="$data${data:+,}\"$scenario\":\"$state\""
    done
    exists=$(kube -n "$KIT_RECORD_NAMESPACE" get configmap "$KIT_RECORD" --ignore-not-found -o name)
    if [ -n "$exists" ]; then
        kube -n "$KIT_RECORD_NAMESPACE" patch configmap "$KIT_RECORD" --type merge -p "{\"data\":{$data}}" >/dev/null
    else
        kube create -f - >/dev/null <<EOF
{"apiVersion": "v1", "kind": "ConfigMap",
 "metadata": {"name": "$KIT_RECORD", "namespace": "$KIT_RECORD_NAMESPACE", "labels": {"$KIT_LABEL": "$KIT_NAME"}},
 "data": {$data}}
EOF
    fi
    record_load
}

# ---------------------------------------------------------------------------
# Checking expectations

# expect_lines prints the lines of the section [$2] of the expect file $1,
# or, when $2 is empty, the lines before any section, as a requires file
# holds them, without comments and blank lines. Each line is prefixed with
# its line number and a tab.
expect_lines() {
    awk -v want="$2" '
        /^[ \t]*#/ || /^[ \t]*$/ { next }
        /^[ \t]*\[[a-z]+\][ \t]*$/ { section = $0; gsub(/[][ \t]/, "", section); next }
        section == want { line = $0; sub(/^[ \t]+/, "", line); sub(/[ \t]+$/, "", line); print NR "\t" line }
    ' "$1"
}

# expect_sections prints the names of the sections of the expect file $1.
expect_sections() {
    awk '/^[ \t]*\[[a-z]+\][ \t]*$/ { s = $0; gsub(/[][ \t]/, "", s); print s }' "$1"
}

# parse_resource sets RES_NAMESPACE, RES_KIND and RES_NAME from $1, written
# <kind>/<name> or <namespace>/<kind>/<name>, and RES_ARGS to the kubectl
# arguments that name it. It fails on anything else.
parse_resource() {
    local a b c d
    IFS=/ read -r a b c d <<<"$1"
    if [ -n "$a" ] && [ -n "$b" ] && [ -n "$c" ] && [ -z "$d" ]; then
        RES_NAMESPACE=$a RES_KIND=$b RES_NAME=$c
        RES_ARGS=(-n "$a" get "$b" "$c")
    elif [ -n "$a" ] && [ -n "$b" ] && [ -z "$c" ]; then
        RES_NAMESPACE="" RES_KIND=$a RES_NAME=$b
        RES_ARGS=(get "$a" "$b")
    else
        return 1
    fi
}

# output_for prints the kubectl output option that reads the path $1: a Go
# template in {{ }}, a JSONPath template in { }, or an alias of one. It
# fails on an unknown alias.
output_for() {
    local path=$1 alias
    case $path in
        '{{'*) printf 'go-template=%s' "$path" ;;
        '{'*) printf 'jsonpath=%s' "$path" ;;
        *)
            [[ "$path" =~ ^[a-z][a-z0-9_]*$ ]] || return 1
            alias=KIT_PATH_$path
            [ -n "${!alias:-}" ] || return 1
            output_for "${!alias}"
            ;;
    esac
}

# shorten prints $1 cut to 160 characters.
shorten() {
    if [ ${#1} -gt 160 ]; then
        printf '%s...' "${1:0:157}"
    else
        printf '%s' "$1"
    fi
}

# get_resource runs kubectl with RES_ARGS and the output option $1, setting
# GOT to what it printed and GOT_ERROR to its error output. It returns 0 when
# the resource was read, 3 when it does not exist, 1 on any other error.
get_resource() {
    local errfile status=0
    errfile=$(mktemp "$(kit_tmp)/err.XXXXXX")
    GOT=$(kube "${RES_ARGS[@]}" -o "$1" 2>"$errfile") || status=$?
    GOT_ERROR=$(trim "$(cat "$errfile")")
    rm -f "$errfile"
    if [ "$status" -eq 0 ]; then
        return 0
    fi
    case $GOT_ERROR in
        *NotFound*) return 3 ;;
    esac
    return 1
}

# check_line checks one line of an expect or requires file, $2, whose
# manifests are named relative to the directory $1. It returns 0 when the
# line holds, 1 when it does not, and sets CHECK_DETAIL to what was found
# when it does not.
check_line() {
    local dir=$1 line=$2
    CHECK_DETAIL=""
    case $line in
        'event '*) check_event "$(trim "${line#event }")" ;;
        'reject '*) check_reject "$dir" "$(trim "${line#reject }")" ;;
        *) check_state "$line" ;;
    esac
}

# check_state checks "<resource> absent" or "<resource> <path> <op> <value>".
check_state() {
    local line=$1 resource rest path op value output status
    resource=${line%%[[:space:]]*}
    rest=$(trim "${line#"$resource"}")
    parse_resource "$resource" || { CHECK_DETAIL="cannot read the resource '$resource'"; return 1; }

    if [ "$rest" = absent ]; then
        get_resource name && status=0 || status=$?
        case $status in
            3) return 0 ;;
            0) CHECK_DETAIL="it exists"; return 1 ;;
            *) CHECK_DETAIL=$(shorten "$GOT_ERROR"); return 1 ;;
        esac
    fi

    # A Go template may hold spaces, so it runs to its first "}}"; any other
    # path is one word.
    local close='}}'
    case $rest in
        '{{'*'}}'*) path=${rest%%"$close"*}$close ;;
        *) path=${rest%%[[:space:]]*} ;;
    esac
    rest=$(trim "${rest#"$path"}")
    op=${rest%%[[:space:]]*}
    value=$(trim "${rest#"$op"}")
    case $op in
        '=' | '!=' | '~') ;;
        *) CHECK_DETAIL="cannot read the line: expected = != or ~ after the path"; return 1 ;;
    esac
    output=$(output_for "$path") || { CHECK_DETAIL="unknown path alias '$path'"; return 1; }

    get_resource "$output" && status=0 || status=$?
    case $status in
        0) ;;
        3) CHECK_DETAIL="it does not exist"; return 1 ;;
        *) CHECK_DETAIL=$(shorten "$GOT_ERROR"); return 1 ;;
    esac
    case $op in
        '=') [ "$GOT" = "$value" ] && return 0 ;;
        '!=') [ "$GOT" != "$value" ] && return 0 ;;
        '~') case $GOT in *"$value"*) return 0 ;; esac ;;
    esac
    CHECK_DETAIL="found \"$(shorten "$GOT")\""
    return 1
}

# check_event checks "<resource> <type> <reason> [= <count>]": the resource
# has Events of that type and reason, as many as count says when it is
# given. Events are matched by the resource's UID, so those of an earlier
# resource of the same name never count. An Event that stands for a series
# of occurrences counts as that many.
check_event() {
    local line=$1 resource type reason eq count extra uid status listing occurrences
    read -r resource type reason eq count extra <<<"$line"
    if [ -z "$reason" ] || [ -n "$extra" ] || { [ -n "$eq" ] && { [ "$eq" != = ] || ! [[ "$count" =~ ^[0-9]+$ ]]; }; }; then
        CHECK_DETAIL="cannot read the line: expected event <resource> <type> <reason> [= <count>]"
        return 1
    fi
    parse_resource "$resource" || { CHECK_DETAIL="cannot read the resource '$resource'"; return 1; }
    get_resource 'jsonpath={.metadata.uid}' && status=0 || status=$?
    case $status in
        0) uid=$GOT ;;
        3) CHECK_DETAIL="the resource does not exist"; return 1 ;;
        *) CHECK_DETAIL=$(shorten "$GOT_ERROR"); return 1 ;;
    esac
    listing=$(kube get events -A --field-selector "involvedObject.uid=$uid,type=$type,reason=$reason" \
        -o jsonpath='{range .items[*]}{.count} {.series.count}{"\n"}{end}' 2>&1) || {
        CHECK_DETAIL=$(shorten "$(trim "$listing")")
        return 1
    }
    occurrences=$(awk 'NF || length($0) { n = 1; for (i = 1; i <= NF; i++) if ($i + 0 > n) n = $i + 0; total += n }
        END { print total + 0 }' <<<"$listing")
    if [ -z "$eq" ]; then
        [ "$occurrences" -ge 1 ] && return 0
        CHECK_DETAIL="no such Event"
    else
        [ "$occurrences" -eq "$count" ] && return 0
        CHECK_DETAIL="$occurrences such Events"
    fi
    return 1
}

# check_reject checks "<manifest> ~ <text>": a server-side dry run of the
# manifest, named relative to $1, is refused with text in its error. Nothing
# is created either way.
check_reject() {
    local dir=$1 line=$2 manifest rest text output
    manifest=${line%%[[:space:]]*}
    rest=$(trim "${line#"$manifest"}")
    case $rest in
        '~ '*) text=$(trim "${rest#\~}") ;;
        *) CHECK_DETAIL="cannot read the line: expected reject <manifest> ~ <text>"; return 1 ;;
    esac
    [ -f "$dir/$manifest" ] || { CHECK_DETAIL="no manifest $manifest"; return 1; }
    if output=$(kube apply --dry-run=server -f "$dir/$manifest" 2>&1); then
        CHECK_DETAIL="accepted: $(shorten "$(trim "$output")")"
        return 1
    fi
    case $output in
        *"$text"*) return 0 ;;
    esac
    CHECK_DETAIL="refused with: $(shorten "$(trim "$output")")"
    return 1
}

# The checks run_checks waits on, one entry each: the scenario and state
# they belong to, the directory their manifests are named relative to, the
# line, its line number, and whether it held when last checked.
CHECK_SCENARIO=()
CHECK_STATE=()
CHECK_DIR=()
CHECK_LINE=()
CHECK_NUMBER=()
CHECK_OK=()
CHECK_WHY=()
CHECK_COUNT=0

checks_reset() {
    CHECK_SCENARIO=() CHECK_STATE=() CHECK_DIR=() CHECK_LINE=() CHECK_NUMBER=() CHECK_OK=() CHECK_WHY=()
    CHECK_COUNT=0
}

# checks_add adds the checks of section $3 of the file $2, for the scenario
# $1, whose manifests are named relative to $4. It returns 1 when the
# section has no lines.
checks_add() {
    local scenario=$1 file=$2 state=$3 dir=$4 number line added=0
    while IFS=$'\t' read -r number line; do
        [ -n "$number" ] || continue
        CHECK_SCENARIO[CHECK_COUNT]=$scenario
        CHECK_STATE[CHECK_COUNT]=$state
        CHECK_DIR[CHECK_COUNT]=$dir
        CHECK_LINE[CHECK_COUNT]=$line
        CHECK_NUMBER[CHECK_COUNT]=$number
        CHECK_OK[CHECK_COUNT]=unknown
        CHECK_WHY[CHECK_COUNT]=""
        CHECK_COUNT=$((CHECK_COUNT + 1))
        added=$((added + 1))
    done <<<"$(expect_lines "$file" "$state")"
    [ "$added" -gt 0 ]
}

# run_checks checks every check added until each holds or the timeout of $1
# seconds passes, and prints how each ended. A check that held is checked
# again only once the others hold, in a last pass over all of them, so what
# it reports held at one time. It returns 1 when a check does not hold.
run_checks() {
    local timeout=$1 start now deadline last_note=0 i pending full=true failed=0
    start=$(date +%s)
    deadline=$((start + timeout))
    while :; do
        pending=0
        i=0
        while [ "$i" -lt "$CHECK_COUNT" ]; do
            if [ "$full" = true ] || [ "${CHECK_OK[i]}" != true ]; then
                if check_line "${CHECK_DIR[i]}" "${CHECK_LINE[i]}"; then
                    CHECK_OK[i]=true
                else
                    CHECK_OK[i]=false
                    CHECK_WHY[i]=$CHECK_DETAIL
                    pending=$((pending + 1))
                fi
            fi
            i=$((i + 1))
        done
        if [ "$pending" -eq 0 ]; then
            [ "$full" = true ] && break
            full=true
            continue
        fi
        full=false
        now=$(date +%s)
        [ "$now" -lt "$deadline" ] || break
        if [ $((now - last_note)) -ge 15 ]; then
            say "  waiting for $pending of $CHECK_COUNT checks ($((now - start))s of ${timeout}s)"
            last_note=$now
        fi
        sleep 2
    done

    local scenario="" mark
    i=0
    while [ "$i" -lt "$CHECK_COUNT" ]; do
        if [ "${CHECK_SCENARIO[i]}/${CHECK_STATE[i]}" != "$scenario" ]; then
            scenario="${CHECK_SCENARIO[i]}/${CHECK_STATE[i]}"
            say "${CHECK_SCENARIO[i]}${CHECK_STATE[i]:+ (${CHECK_STATE[i]})}"
        fi
        if [ "${CHECK_OK[i]}" = true ]; then
            mark="ok  "
        else
            mark="FAIL"
            failed=$((failed + 1))
        fi
        say "  $mark  ${CHECK_LINE[i]}"
        if [ "${CHECK_OK[i]}" != true ]; then
            say "        ${CHECK_WHY[i]} (line ${CHECK_NUMBER[i]})"
        fi
        i=$((i + 1))
    done
    if [ "$failed" -gt 0 ]; then
        say "$failed of $CHECK_COUNT checks failed after $(($(date +%s) - start))s."
        return 1
    fi
    say "All $CHECK_COUNT checks hold."
}
