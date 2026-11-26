# shellcheck shell=bash
# Creates the importer named after the copy, with the rule's shared secret.
kit_apply <<'YAML'
apiVersion: secrets.educates.dev/v1beta1
kind: SecretImporter
metadata:
  name: gated
  namespace: kit-secrets-manager-target-a
spec:
  copyAuthorization:
    sharedSecret: kit-secrets-manager-shared
YAML
