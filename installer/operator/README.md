# educates-installer operator

Go operator that reconciles the four v4 Educates CRDs
(`EducatesClusterConfig`, `SecretsManager`, `LookupService`,
`SessionManager`) and is shipped as the `educates-installer` Helm chart at
[`installer/charts/educates-installer/`](../charts/educates-installer/).

## Architecture

See the repository-root [`AGENTS.md`](../../AGENTS.md) for the installer
architecture and CRD overview.

## Layout

- `api/config/v1alpha1/` — `EducatesClusterConfig` types.
- `api/platform/v1alpha1/` — `SecretsManager`, `LookupService`,
  `SessionManager` types + shared common types.
- `internal/controller/{config,platform}/` — reconcilers for the config
  and platform CRDs.
- `internal/runtimeadmission/`: the envtest suite for the admission
  policies the session-manager chart ships. It applies the chart's CRDs,
  policies and roles to a real API server and checks what each policy
  admits and refuses, impersonating the service accounts involved.
- `cmd/main.go` — manager wiring all four controllers.

The kubebuilder-default `config/` kustomize tree is intentionally absent;
`controller-gen` writes CRDs and RBAC directly into the chart.

## Make targets

| Target | What |
|---|---|
| `make manifests` | Regenerate CRDs + ClusterRole into the `educates-installer` chart |
| `make generate` | Regenerate DeepCopy methods |
| `make test` | Run envtest (downloads `setup-envtest` binaries on first run) |
| `make envtest` | Download envtest binaries only |
| `make docker-build` | Build the operator image (`IMG=…` to override) |
| `make smoke-test` | Local kind-based smoke test |
| `make rbac-verify` | Local kind install+teardown under the fine-grained role (cluster-admin off), asserting no RBAC-forbidden errors |
| `make lint` | golangci-lint |
| `make build` | Build the manager binary |
| `make run` | Run the manager against `~/.kube/config` |

To reproduce the full installer-operator CI job locally (chart-version
lint, `go vet`/`build`, CRD/RBAC + DeepCopy drift checks, envtest and
`golangci-lint`) in one command, run `make ci-operator` from the
repository root. See [Running CI checks locally](../../developer-docs/build-instructions.md#running-ci-checks-locally).
