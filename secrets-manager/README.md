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

Running `go test ./...` directly skips the two suites that need an API
server, with a message saying so, unless `KUBEBUILDER_ASSETS` names the
envtest binaries or `make setup-envtest` has downloaded them into `bin/`.

The scenario suite in `test/scenarios` also runs against a live cluster:
set `SECRETS_MANAGER_LIVE_KUBECONFIG` to the cluster's kubeconfig, and
optionally `SECRETS_MANAGER_LIVE_CONTEXT` to its context and
`SECRETS_MANAGER_LIVE_NAMESPACE` to the namespace the secrets-manager runs
in, `educates` by default. The suite's package comment describes its labels.
Against a live cluster the suite waits much longer for what the
secrets-manager must not do, so a full run takes well over an hour and
needs a timeout to match:

```bash
SECRETS_MANAGER_LIVE_KUBECONFIG=$HOME/.kube/config \
SECRETS_MANAGER_LIVE_CONTEXT=kind-educates \
  go test ./test/scenarios -timeout 3h -ginkgo.label-filter '!live-only'
```

Leave the label filter out to run the `live-only` scenarios too. Two of
them change the cluster itself, so run them only against a cluster of its
own, such as a kind cluster made for the run: one scales the cluster's
secrets-manager Deployment to no replica, and the other scales it to two
replicas and cordons every node. Each puts back what it changed when it
ends, whether it passed or failed. A run that is killed first, by an
interrupt or by the `go test` timeout, can leave the nodes cordoned and the
Deployment scaled; the next run against the cluster puts both back as it
starts, from the annotations the scenarios leave on them, as do
`kubectl uncordon` and `kubectl scale` by hand.
