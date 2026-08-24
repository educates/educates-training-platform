#!/usr/bin/env bash
#
# RBAC acceptance test: install and tear down the full platform on kind with
# the operator running under ONLY its fine-grained roles (cluster-admin off),
# and assert no RBAC-forbidden errors surface across the whole lifecycle.
#
# This is the real gate for the cluster-admin -> fine-grained-role change.
# envtest runs privileged and cannot catch an under-grant; only a real API
# server enforcing RBAC can. Run manually:
#
#   make -C installer/operator rbac-verify
#
# Exercised on kind: cert-manager (CustomCA), Contour, Kyverno, and the three
# platform components (+ node-ca-injector / remote-access extras). external-dns
# needs a cloud DNS provider so it is not installed here — its resource kinds
# (Deployment/Service/ClusterRole/CRD/ServiceAccount) are a subset of what the
# other charts already exercise.
#
# Image dependency: only the operator image is built locally and sideloaded
# into the node (`kind load`; the `dev` tag resolves to imagePullPolicy
# IfNotPresent so the sideloaded build is used). Every other image is pulled
# from its public registry over the internet — the upstream cluster services
# AND the Educates runtime components, which the chart resolves to the
# published `ghcr.io/educates/educates-*` refs. So this test needs internet
# and those runtime images to already be published; on a fork or offline the
# platform components will ImagePullBackOff and the readiness waits will time
# out (a false failure unrelated to RBAC). The permission surface is
# image-independent, so that dependency does not weaken what the test proves.
#
# The kind cluster is created if absent and torn down on exit
# (KEEP_CLUSTER=true keeps it). Set rbac.clusterAdmin=true install to compare.
#
set -euo pipefail

CLUSTER_NAME="${CLUSTER_NAME:-educates-rbac-verify}"
NAMESPACE="${NAMESPACE:-educates-installer}"
IMG="${IMG:-ghcr.io/educates/educates-operator:dev}"
RELEASE="${RELEASE:-educates-installer}"
CHART_DIR="${CHART_DIR:-../charts/educates-installer}"
SAMPLES_DIR="${SAMPLES_DIR:-../samples}"
READY_TIMEOUT="${READY_TIMEOUT:-300}"   # per-CR wait, seconds
WORKDIR="$(mktemp -d)"

CREATED_CLUSTER=false
cleanup() {
    if [[ "$CREATED_CLUSTER" == "true" && "${KEEP_CLUSTER:-false}" != "true" ]]; then
        echo "Tearing down kind cluster $CLUSTER_NAME..."
        kind delete cluster --name "$CLUSTER_NAME" >/dev/null 2>&1 || true
    fi
    rm -rf "$WORKDIR"
}
trap cleanup EXIT

fail() { echo "FAIL: $*" >&2; exit 1; }

for tool in kind kubectl helm docker openssl; do
    command -v "$tool" >/dev/null 2>&1 || fail "$tool not found on PATH"
done

# --- scan operator logs for RBAC-forbidden errors --------------------------
# An under-grant surfaces as apiserver "forbidden" errors in the operator log
# (helm apply/delete failing) — the signal this test exists to catch.
assert_no_forbidden() {
    local phase="$1" hits
    hits=$(kubectl logs -n "$NAMESPACE" -l app.kubernetes.io/name=educates-installer \
        --tail=-1 2>/dev/null | grep -iE 'forbidden|cannot .* at the cluster scope' || true)
    if [ -n "$hits" ]; then
        echo "--- RBAC-forbidden errors during $phase ---" >&2
        echo "$hits" | tail -30 >&2
        fail "operator hit RBAC-forbidden errors during $phase — the fine-grained role is under-granting"
    fi
    echo "  [$phase] no RBAC-forbidden errors in operator logs"
}

wait_ready() {
    local kind="$1" name="${2:-cluster}"
    echo "Waiting up to ${READY_TIMEOUT}s for $kind/$name to be Ready..."
    if ! kubectl wait --for=condition=Ready --timeout="${READY_TIMEOUT}s" "$kind/$name" 2>/dev/null; then
        echo "--- $kind/$name status ---" >&2
        kubectl get "$kind" "$name" -o yaml 2>/dev/null | sed -n '/status:/,$p' | head -60 >&2
        assert_no_forbidden "install ($kind not Ready)"
        fail "$kind/$name did not reach Ready within ${READY_TIMEOUT}s (no forbidden errors seen — inspect status above)"
    fi
}

wait_gone() {
    local kind="$1" name="${2:-cluster}"
    echo "Waiting up to ${READY_TIMEOUT}s for $kind/$name to be deleted (finalizer drain)..."
    if ! kubectl wait --for=delete --timeout="${READY_TIMEOUT}s" "$kind/$name" 2>/dev/null; then
        assert_no_forbidden "teardown ($kind not deleted)"
        fail "$kind/$name did not finish deleting within ${READY_TIMEOUT}s"
    fi
}

# --- cluster ---------------------------------------------------------------
if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
    echo "Creating kind cluster $CLUSTER_NAME..."
    kind create cluster --name "$CLUSTER_NAME"
    CREATED_CLUSTER=true
fi
kubectl config use-context "kind-$CLUSTER_NAME" >/dev/null

echo "Building + loading operator image $IMG..."
make docker-build IMG="$IMG" >/dev/null
kind load docker-image "$IMG" --name "$CLUSTER_NAME"
repo="${IMG%:*}"; tag="${IMG##*:}"

# --- install chart (cluster-admin OFF by default) --------------------------
echo "Installing $RELEASE (rbac.clusterAdmin defaults to false)..."
helm upgrade --install "$RELEASE" "$CHART_DIR" \
    --namespace "$NAMESPACE" --create-namespace \
    --set "image.repository=$repo" --set "image.tag=$tag" \
    --wait --timeout 2m

# Guard: confirm the operator is NOT bound to cluster-admin for this run.
if kubectl get clusterrolebinding educates:installer:cluster-admin >/dev/null 2>&1; then
    fail "educates:installer:cluster-admin binding exists — cluster-admin is not off; this run would not prove the fine-grained role"
fi
echo "Confirmed: operator running without the cluster-admin binding."

# --- CustomCA secret the samples/01 config references ----------------------
echo "Creating self-signed CA secret educates-custom-ca in $NAMESPACE..."
openssl req -x509 -newkey rsa:2048 -nodes \
    -keyout "$WORKDIR/ca.key" -out "$WORKDIR/ca.crt" -days 3650 \
    -subj "/CN=Educates RBAC Verify CA" \
    -addext "basicConstraints=critical,CA:TRUE" >/dev/null 2>&1
kubectl -n "$NAMESPACE" create secret tls educates-custom-ca \
    --cert="$WORKDIR/ca.crt" --key="$WORKDIR/ca.key"

# --- install: cluster config, then platform components ---------------------
echo "Applying EducatesClusterConfig (Managed: cert-manager + Contour + Kyverno)..."
kubectl apply -f "$SAMPLES_DIR/01-local-kind-customca.yaml"
wait_ready educatesclusterconfig
assert_no_forbidden "cluster-service install"

echo "Applying platform components..."
kubectl apply -f "$SAMPLES_DIR/secretsmanager.yaml"
wait_ready secretsmanager
kubectl apply -f "$SAMPLES_DIR/lookupservice.yaml"
kubectl apply -f "$SAMPLES_DIR/sessionmanager.yaml"
wait_ready lookupservice
wait_ready sessionmanager
assert_no_forbidden "platform-component install"

# --- teardown in reverse order ---------------------------------------------
echo "Deleting in reverse order..."
kubectl delete sessionmanager cluster --wait=false
kubectl delete lookupservice cluster --wait=false
kubectl delete secretsmanager cluster --wait=false
wait_gone sessionmanager
wait_gone lookupservice
wait_gone secretsmanager
assert_no_forbidden "platform-component teardown"

kubectl delete educatesclusterconfig cluster --wait=false
wait_gone educatesclusterconfig
assert_no_forbidden "cluster-service teardown"

echo
echo "PASS: full install + teardown completed with the fine-grained role only,"
echo "      no RBAC-forbidden errors in the operator log."
