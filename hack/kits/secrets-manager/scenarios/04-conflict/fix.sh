# shellcheck shell=bash
# Deletes the hand-made Secret, so the copier can create its copy.
kubectl -n kit-secrets-manager-target-a delete secret conflict
