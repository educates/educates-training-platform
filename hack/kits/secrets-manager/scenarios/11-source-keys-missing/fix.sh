# shellcheck shell=bash
# Adds the key the source lacks.
kubectl -n kit-secrets-manager-source patch secret keys-missing --type=merge -p '{"stringData":{"password":"added"}}'
