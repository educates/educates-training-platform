# shellcheck shell=bash
# Gives the second target namespace's importer the exporter's shared secret.
kubectl -n kit-secrets-manager-target-b patch secretimporter export --type=merge \
    -p '{"spec":{"copyAuthorization":{"sharedSecret":"kit-secrets-manager-export-shared"}}}'
