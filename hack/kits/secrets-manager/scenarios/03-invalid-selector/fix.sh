# shellcheck shell=bash
# Drops the values an Exists expression must not have.
kubectl patch secretcopier kit-secrets-manager-invalid-selector --type=json \
    -p '[{"op":"remove","path":"/spec/rules/0/targetNamespaces/labelSelector/matchExpressions/0/values"}]'
