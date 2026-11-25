# shellcheck shell=bash
# Deletes the frozen copy, so the copier creates it again with v2.
kubectl -n kit-secrets-manager-target-b delete secret immutable
