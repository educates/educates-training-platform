# shellcheck shell=bash
# Names the pull secret in the empty injector's source selector.
kubectl patch secretinjector kit-secrets-manager-injector-empty --type=json \
    -p '[{"op":"replace","path":"/spec/rules/0/sourceSecrets/nameSelector/matchNames","value":["pull"]}]'
