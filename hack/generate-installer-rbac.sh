#!/usr/bin/env bash
# Generate the fine-grained ClusterRole the operator's ServiceAccount needs
# to Helm-install and uninstall the vendored charts under its own identity.
# This is the replacement for the cluster-admin shortcut.
#
#   hack/generate-installer-rbac.sh
#
# Writes installer/charts/educates-installer/templates/rbac/charts-role.yaml
# (ClusterRole `educates:installer:charts`). Re-run after `make vendor-charts`
# and commit the result; CI (`make ci-operator`) regenerates and fails on any
# diff.
#
# Two layers:
#
#   1. The CURATED map below is the authoritative grant set — every Kind the
#      operator applies via Helm, mapped to its (apiGroup, resource). It is a
#      deliberate superset: it covers what the charts render today AND kinds
#      they only produce behind value toggles / optional subcharts (rendering
#      with default values under-collects for some charts, so the grant set is
#      curated, not scraped — that way an under-render can never cause an
#      under-grant).
#
#   2. The script renders every vendored chart and fails if any chart produces
#      a Kind absent from the map. So a chart bump cannot silently introduce a
#      resource the role doesn't cover: an unknown Kind is a hard error that
#      forces a maintainer to classify it here, never a silent under-grant.
#
# The emitted role also carries the two things a render can't express: `bind`
# and `escalate` on roles/clusterroles (Kubernetes privilege-escalation
# prevention otherwise blocks the operator from creating the charts' own
# ClusterRoles unless it already holds every permission they grant), and full
# lifecycle verbs including `delete` on customresourcedefinitions (teardown
# with cert-manager `crds.keep=false` cascade-deletes CRDs).
set -euo pipefail

cd "$(dirname "$0")/.."

VENDORED=installer/operator/vendored-charts
OUT=installer/charts/educates-installer/templates/rbac/charts-role.yaml
ROLE_NAME=educates:installer:charts

command -v helm >/dev/null 2>&1 || { echo "helm not found on PATH" >&2; exit 1; }

# Curated grant set: one "<group>|<resource>|<Kind>" per line. Empty group is
# core (v1). <Kind> is what the render check matches against. Keep sorted.
CURATED=$(cat <<'EOF'
|configmaps|ConfigMap
|persistentvolumeclaims|PersistentVolumeClaim
|pods|Pod
|secrets|Secret
|serviceaccounts|ServiceAccount
|services|Service
admissionregistration.k8s.io|mutatingwebhookconfigurations|MutatingWebhookConfiguration
admissionregistration.k8s.io|validatingwebhookconfigurations|ValidatingWebhookConfiguration
apiextensions.k8s.io|customresourcedefinitions|CustomResourceDefinition
apiregistration.k8s.io|apiservices|APIService
apps|daemonsets|DaemonSet
apps|deployments|Deployment
apps|statefulsets|StatefulSet
autoscaling|horizontalpodautoscalers|HorizontalPodAutoscaler
batch|cronjobs|CronJob
batch|jobs|Job
cert-manager.io|certificates|Certificate
cert-manager.io|clusterissuers|ClusterIssuer
cert-manager.io|issuers|Issuer
flowcontrol.apiserver.k8s.io|flowschemas|FlowSchema
flowcontrol.apiserver.k8s.io|prioritylevelconfigurations|PriorityLevelConfiguration
monitoring.coreos.com|podmonitors|PodMonitor
monitoring.coreos.com|prometheusrules|PrometheusRule
monitoring.coreos.com|servicemonitors|ServiceMonitor
networking.k8s.io|ingressclasses|IngressClass
networking.k8s.io|ingresses|Ingress
networking.k8s.io|networkpolicies|NetworkPolicy
policies.kyverno.io|validatingpolicies|ValidatingPolicy
policy|poddisruptionbudgets|PodDisruptionBudget
rbac.authorization.k8s.io|clusterrolebindings|ClusterRoleBinding
rbac.authorization.k8s.io|clusterroles|ClusterRole
rbac.authorization.k8s.io|rolebindings|RoleBinding
rbac.authorization.k8s.io|roles|Role
secrets.educates.dev|secretcopiers|SecretCopier
secrets.educates.dev|secretinjectors|SecretInjector
security.openshift.io|securitycontextconstraints|SecurityContextConstraints
EOF
)

# ---------------------------------------------------------------------------
# Render check: every Kind a vendored chart produces must be in the map.
# ---------------------------------------------------------------------------

# Probe values that force each chart to render its full resource set (cert-
# manager gates its CRDs; the runtime subcharts require an ingress domain / CA
# ref to template). Default values are otherwise a superset of what the
# operator enables.
render_args() {
    case "$1" in
        cert-manager-*)     echo "--set crds.enabled=true" ;;
        lookup-service-*)   echo "--set ingress.host=probe.invalid" ;;
        node-ca-injector-*) echo "--set clusterIngress.caCertificateRef.name=probe" ;;
        session-manager-*)  echo "--set clusterIngress.domain=probe.invalid" ;;
        *)                  echo "" ;;
    esac
}

known_kind() {
    grep -q "|$1\$" <<<"$CURATED"
}

rendered_kinds() {
    local tarball base
    for tarball in "$VENDORED"/*.tgz; do
        base=$(basename "$tarball")
        # shellcheck disable=SC2046 -- render_args is a space-separated flag list
        helm template rbac-probe "$tarball" --include-crds $(render_args "$base") 2>/dev/null \
            | awk '/^kind:/ { gsub(/["\r]/,"",$2); print $2 }'
    done | sort -u
}

unmapped=""
while read -r kind; do
    [ -n "$kind" ] || continue
    known_kind "$kind" || unmapped+="  $kind"$'\n'
done < <(rendered_kinds)

if [ -n "$unmapped" ]; then
    {
        echo "ERROR: vendored charts render Kinds not in the curated RBAC map:"
        echo "$unmapped"
        echo "Add each to CURATED in hack/generate-installer-rbac.sh (with its"
        echo "apiGroup and plural resource), then re-run. This guard is what keeps"
        echo "a chart bump from silently leaving the operator under-permissioned."
    } >&2
    exit 1
fi

# ---------------------------------------------------------------------------
# Emit the ClusterRole from the curated map. Deterministic: one rule per
# apiGroup with the full lifecycle verb set, except roles/clusterroles which
# additionally carry bind + escalate.
# ---------------------------------------------------------------------------

LIFECYCLE="create delete get list patch update watch"

emit_rule() {
    local group="$1" resources="$2" verbs="$3" r v
    echo "- apiGroups:"
    if [ -z "$group" ]; then
        echo '  - ""'
    else
        echo "  - $group"
    fi
    echo "  resources:"
    for r in $resources; do echo "  - $r"; done
    echo "  verbs:"
    for v in $(printf '%s\n' $verbs | sort -u); do echo "  - $v"; done
}

{
    echo "# Code generated by hack/generate-installer-rbac.sh; DO NOT EDIT."
    echo "# Permissions the operator ServiceAccount needs to Helm-install and"
    echo "# uninstall the vendored charts under its own identity. Regenerate with"
    echo "# 'make generate-installer-rbac' after changing the vendored charts."
    echo "---"
    echo "apiVersion: rbac.authorization.k8s.io/v1"
    echo "kind: ClusterRole"
    echo "metadata:"
    echo "  name: $ROLE_NAME"
    echo "rules:"

    # Distinct groups in sorted order (empty/core sorts first via the leading
    # delimiter in the map).
    groups=$(awk -F'|' '{print $1}' <<<"$CURATED" | sort -u)
    while read -r group; do
        resources=$(awk -F'|' -v g="$group" '$1==g {print $2}' <<<"$CURATED" | sort -u)
        if [ "$group" = "rbac.authorization.k8s.io" ]; then
            # roles/clusterroles need bind+escalate so the operator can create
            # the charts' own (broader) roles without holding every permission
            # they grant; the *binding objects only need lifecycle verbs.
            escalatable=$(grep -E '^(clusterroles|roles)$' <<<"$resources" || true)
            plain=$(grep -Ev '^(clusterroles|roles)$' <<<"$resources" || true)
            [ -n "$plain" ] && emit_rule "$group" "$plain" "$LIFECYCLE"
            [ -n "$escalatable" ] && emit_rule "$group" "$escalatable" "bind escalate $LIFECYCLE"
        else
            emit_rule "$group" "$resources" "$LIFECYCLE"
        fi
    done <<<"$groups"
} >"$OUT"

echo "wrote $OUT"
