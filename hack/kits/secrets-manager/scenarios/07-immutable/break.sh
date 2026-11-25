# shellcheck shell=bash
# Freezes the copy at v1, then moves its source to v2.
kubectl -n kit-secrets-manager-target-b patch secret immutable --type=merge -p '{"immutable":true}'
kubectl -n kit-secrets-manager-source patch secret immutable --type=merge -p '{"stringData":{"value":"v2"}}'
