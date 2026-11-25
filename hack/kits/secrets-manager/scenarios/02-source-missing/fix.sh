# shellcheck shell=bash
# Creates the missing source.
kit_apply <<'YAML'
apiVersion: v1
kind: Secret
metadata:
  name: not-there-yet
  namespace: kit-secrets-manager-source
stringData:
  value: arrived
YAML
