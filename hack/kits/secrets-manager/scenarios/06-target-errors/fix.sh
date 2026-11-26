# shellcheck shell=bash
# Lists the keys a TLS Secret needs too.
kubectl patch secretcopier kit-secrets-manager-target-errors --type=json \
    -p '[{"op":"replace","path":"/spec/rules/0/sourceSecret/keys","value":["tls.crt","tls.key","ca.crt"]}]'
