#!/usr/bin/env bash
#
# Runs a validation kit: leaves a known set of states, healthy and failing,
# in an existing cluster for a person to inspect, checks that the cluster
# shows them as described, and breaks, fixes or removes them on request.
# See README.md beside this script, or run it with --help.

set -euo pipefail

KITS_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)
# shellcheck source=lib.sh
. "$KITS_DIR/lib.sh"

usage() {
    cat <<'EOF'
Usage: kit.sh <command> [<kit> [<scenario>...]] [options]

  list [<kit>]                  the kits, or a kit's scenarios and which can be
                                broken and fixed
  apply <kit> [<scenario>...]   check the kit's requires, then create its
                                scenarios (default: every scenario)
  status <kit>                  each scenario's state, and the kit's resources
  events <kit>                  the Events about the kit's resources, oldest
                                first
  verify <kit> [<scenario>...]  wait until each scenario looks as its expect
                                says for its state (default: every scenario
                                applied)
  break <kit> <scenario>        confirm the scenario is applied, run its
                                break.sh, and verify its broken state
  fix <kit> <scenario>          confirm the scenario's failing state, run its
                                fix.sh, and verify its fixed state
  delete <kit>                  remove everything the kit created

A scenario is named with or without its number: 02-source-missing or
source-missing.

Options:
  --local-cluster <name>  the local cluster to act on, as 'educates local
                          cluster list' shows it; an empty value selects the
                          default local cluster (default:
                          $EDUCATES_LOCAL_CLUSTER, else the default cluster)
  --context <context>     act on this kube context instead, of any cluster,
                          read from $KUBECONFIG
  --timeout <duration>    how long verify, break and fix wait, as 90, 90s or
                          2m (default: 120s)
  --cli <path>            the educates CLI to list local clusters with
                          (default: educates on PATH)
  -h, --help              print this help

Exit status: 0 when every check holds, 1 when one does not, 2 when the
command could not run.
EOF
}

# need_value fails unless the option $1 is followed by a value.
need_value() {
    [ $# -ge 2 ] || die "$1 needs a value"
}

# seconds prints the duration $1, written 90, 90s or 2m, in seconds.
seconds() {
    case $1 in
        *[!0-9sm]* | '' | s | m) return 1 ;;
        *m) [[ "${1%m}" =~ ^[0-9]+$ ]] && echo $((${1%m} * 60)) ;;
        *s) [[ "${1%s}" =~ ^[0-9]+$ ]] && echo "${1%s}" ;;
        *) [[ "$1" =~ ^[0-9]+$ ]] && echo "$1" ;;
    esac
}

OPT_CONTEXT=""
OPT_LOCAL_CLUSTER=""
OPT_LOCAL_CLUSTER_GIVEN=false
OPT_CLI=""
OPT_TIMEOUT=120
ARGS=()

COMMAND=${1:-}
[ $# -eq 0 ] || shift
while [ $# -gt 0 ]; do
    case $1 in
        --context) need_value "$@"; OPT_CONTEXT=$2; shift 2 ;;
        --context=*) OPT_CONTEXT=${1#*=}; shift ;;
        --local-cluster) need_value "$@"; OPT_LOCAL_CLUSTER=$2; OPT_LOCAL_CLUSTER_GIVEN=true; shift 2 ;;
        --local-cluster=*) OPT_LOCAL_CLUSTER=${1#*=}; OPT_LOCAL_CLUSTER_GIVEN=true; shift ;;
        --timeout) need_value "$@"; OPT_TIMEOUT=$(seconds "$2") || die "--timeout takes 90, 90s or 2m, not $2"; shift 2 ;;
        --timeout=*) OPT_TIMEOUT=$(seconds "${1#*=}") || die "--timeout takes 90, 90s or 2m, not ${1#*=}"; shift ;;
        --cli) need_value "$@"; OPT_CLI=$2; shift 2 ;;
        --cli=*) OPT_CLI=${1#*=}; shift ;;
        -h | --help) usage; exit 0 ;;
        -*) die "unknown option $1; see --help" ;;
        *) ARGS+=("$1"); shift ;;
    esac
done
set -- ${ARGS[@]+"${ARGS[@]}"}

KIT_TMP=$(mktemp -d "${TMPDIR:-/tmp}/kit.XXXXXX")
trap 'rm -rf "$KIT_TMP"' EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

# ---------------------------------------------------------------------------
# Kits and scenarios

# load_kit loads the kit $1: its kit.conf and its scenarios, every directory
# under scenarios/ that holds an expect file, in order.
load_kit() {
    local name=$1 dir
    [[ "$name" =~ ^[a-z0-9][a-z0-9-]*$ ]] || die "'$name' is not a kit name"
    KIT_DIR=$KITS_DIR/$name
    [ -f "$KIT_DIR/kit.conf" ] || die "no kit '$name'; 'kit.sh list' shows the kits"
    KIT_NAME=$name
    KIT_RECORD=educates-kit-$name
    KIT_DESCRIPTION=""
    KIT_KINDS=""
    # shellcheck source=/dev/null
    . "$KIT_DIR/kit.conf"
    [ -n "$KIT_KINDS" ] || die "kit $name: kit.conf names no KIT_KINDS"
    # shellcheck disable=SC2086 # Word splitting joins the kinds, however written.
    KIT_KINDS_LIST=$(echo $KIT_KINDS | tr ' ' ',')
    KIT_SCENARIOS=()
    for dir in "$KIT_DIR"/scenarios/*/; do
        [ -f "$dir/expect" ] || continue
        dir=${dir%/}
        KIT_SCENARIOS+=("${dir##*/}")
    done
    [ ${#KIT_SCENARIOS[@]} -gt 0 ] || die "kit $name has no scenarios"
}

# scenario_dir prints the directory of the scenario $1.
scenario_dir() {
    printf '%s' "$KIT_DIR/scenarios/$1"
}

# resolve_scenario prints the scenario named $1, with or without its number.
resolve_scenario() {
    local wanted=$1 scenario found=""
    for scenario in "${KIT_SCENARIOS[@]}"; do
        if [ "$scenario" = "$wanted" ] || [ "${scenario#[0-9]*-}" = "$wanted" ]; then
            [ -z "$found" ] || die "kit $KIT_NAME: '$wanted' names more than one scenario"
            found=$scenario
        fi
    done
    [ -n "$found" ] || die "kit $KIT_NAME has no scenario '$wanted'; 'kit.sh list $KIT_NAME' shows its scenarios"
    printf '%s' "$found"
}

# select_scenarios sets SELECTED to the scenarios named in $@, or to every
# scenario when none is.
select_scenarios() {
    local wanted
    SELECTED=()
    if [ $# -eq 0 ]; then
        SELECTED=("${KIT_SCENARIOS[@]}")
        return
    fi
    for wanted in "$@"; do
        SELECTED+=("$(resolve_scenario "$wanted")")
    done
}

# has prints "yes" when the scenario $1 has the script $2.
has() {
    if [ -f "$(scenario_dir "$1")/$2" ]; then echo yes; else echo -; fi
}

# summary prints the first comment of the scenario $1's expect file.
summary() {
    awk '/^[ \t]*#/ { sub(/^[ \t]*#[ \t]*/, ""); print; exit }' "$(scenario_dir "$1")/expect"
}

say_header() {
    say "Kit $KIT_NAME on $KIT_CLUSTER_DESCRIPTION"
}

# hint prints the kit.sh command line that runs the command $1 for the same
# kit, with the arguments $2..., on the same cluster.
hint() {
    local command=$1
    shift
    printf '%s %s %s' "$0" "$command" "$KIT_NAME"
    [ $# -eq 0 ] || printf ' %s' "$@"
    [ -z "${KIT_CLUSTER_FLAG:-}" ] || printf ' %s' "$KIT_CLUSTER_FLAG"
}

# confirm_state waits until the scenario $1 shows its state $2 as its expect
# says, and exits 1 when it does not, saying it will not $3.
confirm_state() {
    local scenario=$1 state=$2 action=$3
    say "==> Confirming $scenario is $state"
    checks_reset
    checks_add "$scenario" "$(scenario_dir "$scenario")/expect" "$state" "$(scenario_dir "$scenario")" ||
        die "$scenario: its expect has no [$state] section"
    if ! run_checks "$OPT_TIMEOUT"; then
        say "$scenario is not $state as its expect says; not running its $action."
        exit 1
    fi
}

# run_scenario_script runs the script $2 of the scenario $1 in its
# directory, with kubectl pointed at the selected cluster and kit_apply at
# hand. A command that fails in it stops kit.sh, so the scenario's state is
# not recorded as changed.
run_scenario_script() {
    local scenario=$1 script=$2
    say "==> Running $scenario/$script"
    (
        KIT_SCENARIO=$scenario
        KIT_SCENARIO_DIR=$(scenario_dir "$scenario")
        kubectl() { kube "$@"; }
        cd "$KIT_SCENARIO_DIR"
        # shellcheck source=/dev/null
        . "./$script"
    )
}

# verify_state checks the scenario $1 against its state $2 and exits 1 when
# it does not show it.
verify_state() {
    local scenario=$1 state=$2
    say "==> Verifying $scenario is $state"
    checks_reset
    checks_add "$scenario" "$(scenario_dir "$scenario")/expect" "$state" "$(scenario_dir "$scenario")" ||
        die "$scenario: its expect has no [$state] section"
    run_checks "$OPT_TIMEOUT" || exit 1
}

# ---------------------------------------------------------------------------
# Commands

cmd_list() {
    local dir scenario
    if [ $# -eq 0 ]; then
        {
            printf 'KIT\tDESCRIPTION\n'
            for dir in "$KITS_DIR"/*/; do
                [ -f "$dir/kit.conf" ] || continue
                dir=${dir%/}
                (
                    KIT_DESCRIPTION=""
                    # shellcheck source=/dev/null
                    . "$dir/kit.conf"
                    printf '%s\t%s\n' "${dir##*/}" "$KIT_DESCRIPTION"
                )
            done
        } | print_table
        return
    fi
    [ $# -eq 1 ] || die "list takes at most one kit"
    load_kit "$1"
    {
        printf 'SCENARIO\tBREAK\tFIX\tSUMMARY\n'
        for scenario in "${KIT_SCENARIOS[@]}"; do
            printf '%s\t%s\t%s\t%s\n' "$scenario" "$(has "$scenario" break.sh)" "$(has "$scenario" fix.sh)" "$(summary "$scenario")"
        done
    } | print_table
}

cmd_apply() {
    [ $# -ge 1 ] || die "apply needs a kit"
    load_kit "$1"
    shift
    select_scenarios "$@"
    select_cluster
    say_header

    record_load
    local scenario state refused=""
    for scenario in "${SELECTED[@]}"; do
        state=$(record_state "$scenario")
        case $state in
            broken | fixed) refused="$refused $scenario ($state)" ;;
        esac
    done
    [ -z "$refused" ] || die "can't apply again what was broken or fixed:$refused; start over with '$(hint delete)' and '$(hint apply)'"

    if [ -f "$KIT_DIR/requires" ]; then
        say "==> Checking the cluster meets the kit's requires"
        checks_reset
        if checks_add requires "$KIT_DIR/requires" "" "$KIT_DIR"; then
            run_checks 0 || die "the cluster does not meet kit $KIT_NAME's requires; nothing was applied"
        fi
    fi

    if [ -f "$KIT_DIR/base.yaml" ]; then
        say "==> Applying base.yaml"
        stamped_apply base "$KIT_DIR/base.yaml"
    fi
    for scenario in "${SELECTED[@]}"; do
        if [ -f "$(scenario_dir "$scenario")/apply.yaml" ]; then
            say "==> Applying $scenario"
            stamped_apply "$scenario" "$(scenario_dir "$scenario")/apply.yaml"
        fi
    done
    record_set applied "${SELECTED[@]}"
    say "Applied ${#SELECTED[@]} scenarios. Verify them with: $(hint verify)"
}

cmd_status() {
    [ $# -eq 1 ] || die "status takes a kit"
    load_kit "$1"
    select_cluster
    say_header
    record_load
    local scenario state
    say
    {
        printf 'SCENARIO\tSTATE\tBREAK\tFIX\n'
        for scenario in "${KIT_SCENARIOS[@]}"; do
            state=$(record_state "$scenario")
            printf '%s\t%s\t%s\t%s\n' "$scenario" "${state:-not applied}" "$(has "$scenario" break.sh)" "$(has "$scenario" fix.sh)"
        done
    } | print_table
    say
    kube get "$KIT_KINDS_LIST" -A -l "$KIT_LABEL=$KIT_NAME"
    say
    say "Secrets: kubectl --context $KIT_CONTEXT get secrets -A -l $KIT_LABEL=$KIT_NAME"
}

cmd_events() {
    [ $# -eq 1 ] || die "events takes a kit"
    load_kit "$1"
    select_cluster
    say_header
    local uids events
    uids=$(kube get "$KIT_KINDS_LIST" -A -l "$KIT_LABEL=$KIT_NAME" \
        -o jsonpath='{range .items[*]}{.metadata.uid}{"\n"}{end}')
    if [ -z "$uids" ]; then
        say "The kit has no resources on this cluster."
        return
    fi
    events=$(kube get events -A --sort-by=.metadata.creationTimestamp -o jsonpath='{range .items[*]}{.involvedObject.uid}{"\t"}{.metadata.creationTimestamp}{"\t"}{.type}{"\t"}{.reason}{"\t"}{.involvedObject.namespace}{"\t"}{.involvedObject.kind}/{.involvedObject.name}{"\t"}{.count} {.series.count}{"\t"}{.message}{"\n"}{end}')
    printf '%s\n' "$uids" >"$KIT_TMP/uids"
    say
    awk -F '\t' -v OFS='\t' '
        FILENAME == ARGV[1] { kit[$1] = 1; next }
        FNR == 1 { print "TIME (UTC)", "TYPE", "REASON", "OBJECT", "COUNT", "MESSAGE" }
        ($1 in kit) {
            n = split($7, c, " "); count = 1
            for (i = 1; i <= n; i++) if (c[i] + 0 > count) count = c[i] + 0
            object = ($5 == "" ? $6 : $5 "/" $6)
            print substr($2, 12, 8), $3, $4, object, count, $8
        }' "$KIT_TMP/uids" - <<<"$events" | print_table
}

cmd_verify() {
    [ $# -ge 1 ] || die "verify needs a kit"
    load_kit "$1"
    shift
    local named=false
    [ $# -eq 0 ] || named=true
    select_scenarios "$@"
    select_cluster
    say_header
    record_load
    local scenario state skipped="" missing=""
    checks_reset
    for scenario in "${SELECTED[@]}"; do
        state=$(record_state "$scenario")
        if [ -z "$state" ]; then
            [ "$named" = false ] || die "$scenario is not applied; apply it with '$(hint apply "$scenario")'"
            skipped="$skipped $scenario"
            continue
        fi
        checks_add "$scenario" "$(scenario_dir "$scenario")/expect" "$state" "$(scenario_dir "$scenario")" ||
            missing="$missing $scenario ($state)"
    done
    [ "$CHECK_COUNT" -gt 0 ] || [ -n "$missing" ] || die "the kit is not applied on this cluster; apply it with '$(hint apply)'"
    [ -z "$skipped" ] || say "Not applied, so not verified:$skipped"
    say "==> Verifying, waiting up to ${OPT_TIMEOUT}s"
    local status=0
    run_checks "$OPT_TIMEOUT" || status=1
    if [ -n "$missing" ]; then
        say "No expectations for the state of:$missing"
        status=1
    fi
    return "$status"
}

cmd_break() {
    [ $# -eq 2 ] || die "break takes a kit and a scenario"
    load_kit "$1"
    local scenario state
    scenario=$(resolve_scenario "$2")
    [ -f "$(scenario_dir "$scenario")/break.sh" ] || die "$scenario has no break.sh"
    select_cluster
    say_header
    record_load
    state=$(record_state "$scenario")
    case $state in
        applied) ;;
        '') die "$scenario is not applied; apply it with '$(hint apply "$scenario")'" ;;
        *) die "$scenario is $state; it breaks once after apply: start over with '$(hint delete)' and '$(hint apply)'" ;;
    esac
    confirm_state "$scenario" applied break.sh
    run_scenario_script "$scenario" break.sh
    record_set broken "$scenario"
    verify_state "$scenario" broken
    say "Fix it with: $(hint fix "${scenario#[0-9]*-}")"
}

cmd_fix() {
    [ $# -eq 2 ] || die "fix takes a kit and a scenario"
    load_kit "$1"
    local scenario state from=applied
    scenario=$(resolve_scenario "$2")
    [ -f "$(scenario_dir "$scenario")/fix.sh" ] || die "$scenario has no fix.sh"
    [ ! -f "$(scenario_dir "$scenario")/break.sh" ] || from=broken
    select_cluster
    say_header
    record_load
    state=$(record_state "$scenario")
    if [ "$state" != "$from" ]; then
        case $state in
            '') die "$scenario is not applied; apply it with '$(hint apply "$scenario")'" ;;
            applied) die "$scenario is applied, not broken; break it first with '$(hint break "${scenario#[0-9]*-}")'" ;;
            *) die "$scenario is $state; it is fixed once: start over with '$(hint delete)' and '$(hint apply)'" ;;
        esac
    fi
    confirm_state "$scenario" "$from" fix.sh
    run_scenario_script "$scenario" fix.sh
    record_set fixed "$scenario"
    verify_state "$scenario" fixed
}

cmd_delete() {
    [ $# -eq 1 ] || die "delete takes a kit"
    load_kit "$1"
    select_cluster
    say_header
    say "==> Deleting the kit's ${KIT_KINDS_LIST//,/, }"
    kube delete "$KIT_KINDS_LIST" -A -l "$KIT_LABEL=$KIT_NAME" --ignore-not-found --wait=true --timeout=120s
    say "==> Deleting the kit's namespaces"
    kube delete namespaces -l "$KIT_LABEL=$KIT_NAME" --ignore-not-found --wait=true --timeout=300s
    kube -n "$KIT_RECORD_NAMESPACE" delete configmap "$KIT_RECORD" --ignore-not-found >/dev/null
    say "Deleted kit $KIT_NAME and its record."
}

case $COMMAND in
    list) cmd_list "$@" ;;
    apply) cmd_apply "$@" ;;
    status) cmd_status "$@" ;;
    events) cmd_events "$@" ;;
    verify) cmd_verify "$@" ;;
    break) cmd_break "$@" ;;
    fix) cmd_fix "$@" ;;
    delete) cmd_delete "$@" ;;
    -h | --help | help) usage ;;
    '') usage >&2; exit 2 ;;
    *) die "unknown command '$COMMAND'; see --help" ;;
esac
