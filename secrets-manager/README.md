Secrets Manager
===============

This directory holds the source code for the Educates secrets-manager, a Go
operator built on controller-runtime. It copies Secrets between namespaces
and injects Secrets into service accounts, as the `SecretCopier`,
`SecretExporter`, `SecretImporter` and `SecretInjector` resources of
`secrets.educates.dev/v1beta1` ask, and reports in their status whether
their copies and injections are in sync.

The Go types of the four kinds are in `api/v1beta1`. Their custom resource
definitions live in the secrets-manager subchart, under
`installer/charts/educates-training-platform/charts/secrets-manager/crds`,
and are the schema of record: only the DeepCopy methods of the types are
generated, by `make generate`.

`make build` builds the secrets-manager, `make test` runs its tests, the
scenario suite against envtest among them, and `make help` lists the other
targets. `make run` runs it from your host against the current kubeconfig
context, without leader election. The root Makefile's `ci-secrets-manager`
target runs the checks CI gates on.

The scenario suite in `test/scenarios` also runs against a live cluster:
set `SECRETS_MANAGER_LIVE_KUBECONFIG` to the cluster's kubeconfig, and
optionally `SECRETS_MANAGER_LIVE_CONTEXT` to its context and
`SECRETS_MANAGER_LIVE_NAMESPACE` to the namespace the secrets-manager runs
in, `educates` by default. The suite's package comment describes its labels.
