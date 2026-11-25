# secrets-manager kit

Puts the Go secrets-manager's four kinds into each of their states, so that
you can see their status, the columns `kubectl get` prints and their
Events, then recovers each failing one. See the [kits README](../README.md)
for running it.

```
hack/kits/kit.sh apply secrets-manager --local-cluster mycheck
hack/kits/kit.sh verify secrets-manager --local-cluster mycheck
hack/kits/kit.sh status secrets-manager --local-cluster mycheck
hack/kits/kit.sh events secrets-manager --local-cluster mycheck
kubectl --context kind-educates-mycheck describe secretcopier kit-secrets-manager-source-missing
hack/kits/kit.sh break secrets-manager immutable --local-cluster mycheck
hack/kits/kit.sh fix secrets-manager immutable --local-cluster mycheck
hack/kits/kit.sh delete secrets-manager --local-cluster mycheck
```

## What it needs

A cluster running the Go secrets-manager: the Deployment
`educates/secrets-manager` available, and the secrets-manager's CRDs with
the `status.summary` the Python secrets-manager's lack. `apply` checks both,
as `requires` says, and applies nothing on a cluster without them.

## What it creates

Three namespaces: `kit-secrets-manager-source` holds the sources, and
copies land in `kit-secrets-manager-target-a` and
`kit-secrets-manager-target-b`. They carry the label `kit-secrets-manager:
source` or `target`, which a scenario's label selector picks them by. The
copiers and injectors, which have no namespace, are named
`kit-secrets-manager-<scenario>`. Nothing else in the cluster is touched,
and `delete` removes it all.

## Scenarios

| Scenario | Resources | After `apply` | `break` | After `fix` |
|---|---|---|---|---|
| 01-healthy | copier `kit-secrets-manager-healthy` | `True InSync 1/1`; the copy carries its source's labels and the copier's label and annotation | | |
| 02-source-missing | copier `kit-secrets-manager-source-missing` | `False SourceMissing 0/1`; Warning `NotReady` after 60 s | | the source is created: `InSync 1/1`; Normal `Recovered` |
| 03-invalid-selector | copier `kit-secrets-manager-invalid-selector` | `False InvalidSelector 0/0`: an `Exists` expression with values; the message names `rules[0]` | | the values are dropped: `InSync 2/2`, both target namespaces but not the source's |
| 04-conflict | copier `kit-secrets-manager-conflict` | `False Conflict 0/1`; the hand-made Secret in target-a keeps its data | | the hand-made Secret is deleted: `InSync 1/1`, the copy holds the source's data |
| 05-awaiting-authorization | copier `kit-secrets-manager-awaiting-authorization` | `False AwaitingAuthorization 0/1`; no copy | | an importer named after the copy is created: `InSync 1/1`; importer `Imported`, bound to the copier, with a Normal `Imported` |
| 06-target-errors | copier `kit-secrets-manager-target-errors` | `False TargetErrors 0/1`; the message names target-a and quotes the API server's `Required value` | | the TLS keys are listed too: `InSync 1/1`, a `kubernetes.io/tls` copy |
| 07-immutable | copier `kit-secrets-manager-immutable` | `True InSync 1/1`, the copy holds `v1` | the copy is made immutable and the source moves to `v2`: `False TargetErrors`, the copy is kept at `v1`, one Warning `CopyNotUpdated` | the frozen copy is deleted: `InSync 1/1`, recreated with `v2` |
| 08-export-import | exporter `export`, importers `export` in both targets and `nobody-exports` in target-b | exporter `False AwaitingAuthorization 1/2`; importers `Imported`, `AwaitingAuthorization` (`SharedSecretMismatch`) and `NoMatchingExporter` | | the target-b importer gets the right shared secret: exporter `InSync 2/2`, that importer `Imported`; `nobody-exports` stays as it is, on purpose |
| 09-injector | injectors `kit-secrets-manager-injector` and `kit-secrets-manager-injector-empty` | `True InSync 1/1`, the pull secret in target-a's `default` service account; `False InvalidSelector` for the empty source selector | | the empty injector names the pull secret: `InSync 1/1` |
| 10-rejected-at-admission | two copiers, never created | admission refuses an empty `targetSecret.keys`, and an annotation key under `secrets.educates.dev/` | | |

Every resource that is not Ready gets a Warning `NotReady` once it has not
been Ready for 60 seconds, while its status shows the problem at once, so a
`verify` right after `apply` takes a minute. A Normal `Recovered` follows
only a `NotReady`, which is why `fix` waits for the failing state first.
Every `expect` file holds the exact checks.

Applying 06-target-errors prints the API server's warning `tls: failed to
find any PEM data in certificate input`: the TLS source holds placeholder
text, not a certificate, which is all the scenario needs.
