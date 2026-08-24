# AGENTS.md

Briefing for AI agents working in this repository. Read it fully before
doing anything substantial. Sections are ordered by importance — the top
is what changes day-to-day, the bottom is mostly stable.

---

## The install path

The install pipeline is a **Helm chart + Go operator**. There is one
install path; the old v3 Carvel-based installer (kapp-controller) is gone.

- **CLI flow:** `educates admin platform deploy` (helm install the
  operator chart + `kubectl apply` the 4 platform CRs), or `educates
  local cluster create` for the laptop flow (kind + registry + deploy in
  one).
- **No in-place v3 migration.** Users on v3 delete their old install and
  follow the v4 path. The CLI silently migrates a v3 `values.yaml` → v4
  `config.yaml` on first run when the v3 provider was kind (or empty);
  other providers get a clear re-declare error.
- **CLI config is a kind ladder** (`cli.educates.dev/v1alpha1`):
  `EducatesLocalConfig` (laptop; lives at `<data-home>/config.yaml`),
  scenario kinds `EducatesGKEConfig` / `EducatesEKSConfig` /
  `EducatesInlineConfig`, and the `EducatesConfig` escape hatch (CR
  specs verbatim, no CLI defaults). JSON schemas are embedded at
  `client-programs/pkg/config/v1alpha1/schemas/`. Data home is
  `$XDG_DATA_HOME/educates/`, overridable via `EDUCATES_CLI_DATA_HOME`.

**Carvel libraries still live in the CLI** (`carvel.dev/imgpkg`,
`kapp`, `ytt`, `vendir`) — they power the **workshop tooling**
(`educates {cluster,docker} workshop ...` commands for publish /
deploy / serve). The install path no longer touches them.

**The Educates runtime is Python/kopf/Django.** Components in
`session-manager/`, `secrets-manager/`, `lookup-service/`,
`training-portal/`, `tunnel-manager/`, `workshop-images/`, and supporting
services keep those implementations. The installer is the operator + chart;
it installs the runtime but does not reimplement it.

---

## Repository scope: what's safe to change vs not

**Principle: stay inside the boundary of the feature you are working on.**
Every task is scoped to a feature or component, and that scope defines your
boundary. Inside the boundary you may create and modify freely: the files
the feature declares plus the files you were explicitly asked to change.
Outside the boundary, ask for explicit permission before changing anything.
Cross-boundary edits have wider implications (other owners, other release
cadences, extra coordination), so they are never made silently.

The boundary is whichever part of the project the feature lives in, not a
fixed "installer vs runtime" split. For example:

- **CLI work:** the boundary is `client-programs/`. Anything outside it (the
  installer, any runtime component, other tooling) needs explicit sign-off
  first.
- **Installer work:** the boundary is `installer/` plus `client-programs/`,
  because the CLI drives the install path and embeds the operator chart.
  Anything outside those two needs sign-off.
- **A runtime component** (for example `session-manager/`): the boundary is
  that component's directory plus the artifacts it directly owns and couples
  to. For `session-manager` that also includes `workshop-images/` (the base
  environment it spawns sessions from) and its own subchart under
  `installer/charts/educates-training-platform/charts/session-manager/`.
  Anything else needs sign-off. The same shape applies to `secrets-manager/`,
  `lookup-service/`, `training-portal/`, `tunnel-manager/`,
  `node-ca-injector/`, `assets-server/`, and `image-cache/`.

Documentation is governed by the release-notes and documentation-review
norms below, not by this boundary: update `project-docs/` (user-facing) and
`developer-docs/` for the feature you are working on as part of completing
it. You do not need separate permission to document your own change.

When a task genuinely needs a change outside its boundary (rare, for
example a runtime component that must consume a new installer config flag),
flag it explicitly and get agreement before touching the other area. A
clarifying question is cheaper than an unwanted cross-boundary edit.

---

## Working norms

How I expect to collaborate:

- **Ask before destructive operations.** `rm`, `git push`, `git reset --hard`,
  deleting files outside `/tmp` — confirm first.
- **Don't push to remote.** Commits are fine when asked; pushes are mine.
- **Small, focused commits with clear messages.** Conventional Commits style
  preferred (`feat(operator): ...`, `fix(crd): ...`).
- **No `Co-Authored-By: Claude` trailers in commit messages.** Commits are
  authored by me; collaboration with you is a working detail, not a
  history artefact. Plain commit message body, no attribution footer.
- **Don't run tests unless asked.** I'll often want to inspect intermediate
  state without test runs interfering.
- **Verify Helm chart values before suggesting them.** `helm show values
  <chart>` is the source of truth, not your training data.
- **Verify Kubernetes API semantics before suggesting them.** Especially
  finalizers, namespace deletion, controller-runtime cache behavior. When in
  doubt, ask me to verify, or read the source.
- **Prefer asking over assuming.** A clarifying question costs me 10 seconds.
  Wrong code costs me 30 minutes of debugging plus the time to undo it.
- **When uncertain about a design decision, stop and ask.** Don't pick a
  direction and run with it.
- **Always draft a plan or check-list when working with complex tasks.** If you
  envision the task is going to be long, either plan properly if needed, or at
  least create a task-list to track progress.
- **Every user-facing change needs a release-notes entry to be complete.** Before
  considering a change done, add or update the matching entry in
  `project-docs/release-notes/version-4.0.0.md` (New Features / Features Changed /
  Bugs Fixed). Purely internal changes — refactors, CI, tests, developer docs —
  are exempt. Match the file's prose style: full sentences, double-backtick
  literals, wrapped lines.
- **Every user-facing change needs a documentation review for completeness.** Before
  considering a change done, review the user facing documentation
  `project-docs`. Purely internal changes — refactors, CI, tests —
  are exempt. Match the documentations's prose style: full sentences, double-backtick
  literals, wrapped lines, **NEVER USE** emdashes.
---

## Build and run commands

### Install path

CLI-driven (laptop or single command from CI):

```bash
educates local secrets add ca <domain>-ca --domain <domain>   # one-time: generate self-signed CA
educates local config init                                    # one-time: write a minimal config
educates local cluster create                                 # kind + registry + deploy in one
# Or after the cluster's already up:
educates admin platform deploy --local-config
educates admin platform render --local-config                 # dry-run / GitOps preview
educates admin platform delete                                # uninstall
```

Raw helm + kubectl path (no CLI):

```bash
helm install educates-installer ./installer/charts/educates-installer \
  --namespace educates-installer --create-namespace
kubectl apply -f educates-cluster-config.yaml
kubectl apply -f educates-secrets-manager.yaml
kubectl apply -f educates-lookup-service.yaml       # optional
kubectl apply -f educates-session-manager.yaml
```

### Operator project

The Go operator lives at `installer/operator/` (kubebuilder project,
with the standard `config/` kustomize tree stripped — `controller-gen`
writes CRDs and RBAC directly into the `educates-installer` chart).
Module path: `github.com/educates/educates-training-platform/installer/operator`,
in `go.work`. API packages: `api/config/v1alpha1` (EducatesClusterConfig)
and `api/platform/v1alpha1` (SecretsManager, LookupService,
SessionManager).

Make targets, all run from `installer/operator/`:

```bash
make manifests                 # Regenerate CRDs + RBAC into the chart
make generate                  # Regenerate deepcopy
make test                      # Run envtest (downloads binaries on first run)
make envtest                   # Just download envtest binaries
make docker-build              # Build local operator image (dev)
make smoke-test                # kind + helm install + apply CR + assert log line
make rbac-verify               # kind install+teardown under the fine-grained role (no cluster-admin)
make lint                      # golangci-lint
make vendor-charts             # Download upstream charts into vendored-charts/, verify SHA256
make verify-vendored-charts    # Re-verify SHA256 of tarballs already on disk
```

### Common

```bash
make build-client-programs                    # Build educates CLI binary
cd client-programs && go mod tidy             # Tidy CLI deps
cd node-ca-injector && go test ./...          # Run Go tests
make build-project-docs                       # Build Sphinx docs
make prune-all                                # Clean caches and build artifacts
```

### CI parity (run the gating workflows locally)

```bash
make ci                                       # All CI checks (CLI + operator)
make ci-cli                                   # client-programs-ci.yaml: vet/build/test + chart/schema drift
make ci-operator                              # installer-operator-ci.yaml: vet/build + manifest/deepcopy drift + envtest + lint
make stage-renderer-files                     # Stage the gitignored CLI theme embed dir (also done by build-cli/ci-cli)
```

`ci-operator` needs the Go version pinned in `installer/operator/go.mod`
on `PATH` (or `GOTOOLCHAIN=go1.x.y`) to avoid mixed-toolchain compile
errors. See `developer-docs/build-instructions.md`.

---

## Architecture

### Runtime components

- **session-manager/** — Python/kopf operator managing workshop sessions,
  environments, allocations, training portals, vcluster integration. Main
  control plane.
- **secrets-manager/** — Python/kopf operator handling secret copying,
  exporting, importing, injection across namespaces.
- **training-portal/** — Django web app, user-facing portal. Exposes REST API.
- **lookup-service/** — Python service for service discovery.
- **tunnel-manager/** — Hybrid Python/Go operator for network tunneling.
- **node-ca-injector/** — Go program injecting CA certificates into
  Kubernetes nodes.
- **workshop-images/** — Dockerfiles for workshop session containers.

### Installer

The installer has three layers:

1. **`educates-installer` Helm chart** (`installer/charts/educates-installer/`)
   — installs the operator, four CRDs, and RBAC. This is what users
   `helm install` (imperative) or what ArgoCD/Flux points at
   (declarative). Same artifact, both paths. Installed by `educates admin
   platform deploy` via an embedded copy at
   `client-programs/pkg/deployer/chart/files/`, or by raw `helm install`.
2. **Go operator** (`installer/operator/`) — reconciles the four CRDs.
   Uses the `helm.sh/helm/v4` SDK to install upstream cluster-service
   charts (cert-manager, contour, kyverno, external-dns) from vendored
   tarballs, and the runtime component charts. Has finalizers for clean
   uninstall (drain in reverse install order).
3. **Runtime component charts** — secrets-manager, lookup-service,
   session-manager. Installed by the operator into the shared `educates`
   namespace.

- **client-programs/** — Go CLI (`educates`). Holds the install pipeline
  (load → translate → helm install + apply CRs + wait Ready) and the
  workshop tooling (publish, deploy, render — these still use carvel libs
  for OCI bundle and kapp deploy).

### Go workspace

`go.work` covers `client-programs/` and `node-ca-injector/`. Other Go
services (`assets-server/`, `tunnel-manager/`) have their own `go.mod` and
are built only via Docker.

### The four CRDs

All cluster-scoped, all singletons named `cluster`. Components consume
`EducatesClusterConfig.status` as their input contract — they never read
its `.spec` directly. This is what lets Inline mode work without
components knowing the difference.

- **`EducatesClusterConfig`** (`config.educates.dev/v1alpha1`) —
  cluster-wide infrastructure and services. Two modes: `Managed`
  (operator installs cluster services) and `Inline` (user declares
  pre-existing resources). Publishes `status.ingress`,
  `status.bundledChartVersions`, and per-service Ready conditions
  (`CertificatesReady`, `IngressReady`, `DNSReady`,
  `PolicyEnforcementReady`).
- **`SecretsManager`** (`platform.educates.dev/v1alpha1`) —
  secrets-manager component. Gates on `EducatesClusterConfig.status`
  being Ready.
- **`LookupService`** (`platform.educates.dev/v1alpha1`) —
  lookup-service component.
- **`SessionManager`** (`platform.educates.dev/v1alpha1`) —
  session-manager component. Gates on `EducatesClusterConfig.status` and
  `SecretsManager.Ready`.

---

## Key conventions and gotchas

**CRDs and Go types:**
- The Go API types under `installer/operator/api/` are the source of
  truth for CRD shape. CRDs and RBAC are generated from them via `make
  manifests` directly into the chart.
- Field names follow Kubernetes conventions: `camelCase` in YAML/JSON,
  `PascalCase` in Go structs, lowercase tags.
- Use `+kubebuilder:` markers for validation, defaults, printer columns.
- EducatesClusterConfig has three structural CEL rules on spec — singleton
  name, mode immutability (`oldSelf`), mode-field exclusivity. The three
  platform CRDs have singleton-name only.

**Status conventions:**
- Status is the public interface for component CRs and humans. Treat it as
  versioned API surface.
- Keep status minimal — only the inter-CR contract plus conditions.
- Use standard Kubernetes condition types (`Ready`, plus PascalCase
  domain-specific ones like `CertificatesReady`).

**RBAC:**
- The operator runs with two ClusterRoles bound to its ServiceAccount:
  `educates:installer:manager` (generated by `make manifests` from
  `+kubebuilder:rbac` markers — the operator's own API calls) and
  `educates:installer:charts` (generated by `make generate-installer-rbac`,
  a root Make target — the resources the operator applies via Helm on the
  vendored charts' behalf, plus `escalate`/`bind` so it can create the
  charts' own broader roles). The old unconditional cluster-admin binding is
  now gated behind `rbac.clusterAdmin` (default `false`) — an escape hatch,
  not the default path.
- The charts role is generated by rendering every vendored chart and mapping
  the resulting kinds through a curated resource map; an unmapped kind is a
  hard error, so a chart bump can't silently under-grant. `ci-operator`
  drift-checks it in `templates/rbac` alongside the marker-generated role;
  `make rbac-verify` proves sufficiency with a real kind install+teardown
  (envtest runs privileged and can't catch an under-grant).
- The EducatesClusterConfig reconciler has read-only `get/list/watch` on
  its referenced kinds (Secrets, ClusterIssuers, IngressClasses) plus full
  access on its own kind. Platform reconcilers carry full access on their
  own kinds plus what their charts install need.

**Watches:**
- Reconcilers watch referenced resources (Secrets, ClusterIssuers,
  IngressClasses), not just CR generation changes. External changes — like
  a deleted TLS secret — must propagate to status within seconds.
- Secret + IngressClass + Deployment are registered in `SetupWithManager`
  (operator-namespace-scoped Secret cache; cluster-scoped IngressClass;
  Deployment cluster-wide).
- cert-manager.io ClusterIssuer + Certificate watches are **deferred** —
  registered at runtime by `CRDWatcher`
  (`internal/controller/config/crd_watcher.go`) once discovery confirms
  the CRDs exist (activation lag up to `PollInterval`, 15s in production).
  cert-manager CRDs are **not** a prerequisite for operator startup.
- Each watched kind has its own mapping function (`mapSecretToSingleton`,
  `mapIngressClassToSingleton`, etc., in `watches.go`) that drops events
  the reconciler can't act on — referenced-from-spec or operator-owned
  only — to avoid a cluster-wide reconcile flood. Deferred-watch kinds use
  the same machinery: `CRDWatcher.Targets` carries `(GVK, MapFunc)` pairs.

**cert-manager.io access split:**
- Watches go through CRDWatcher's deferred-registration path (no
  GVK-at-startup requirement). Get / Create / Update / SSA-patch calls use
  typed `cmv1.*`, and only run after `ensureCertManagerReady` confirms the
  CRDs are present, so typed GVK resolution always succeeds.
- On CRD removal post-activation (rare; mostly the end of Managed-mode
  teardown), the reconciler classifies the resulting `NoMatchError` /
  404-with-UnexpectedServerResponse via `isCertManagerCRDMissingErr` and
  surfaces `CertificatesReady=False reason=CertManagerCRDsMissing`,
  `phase=Degraded`.

**Helm SDK:**
- Target `helm.sh/helm/v4`, wrapped by `installer/operator/internal/helm`
  so reconcilers don't repeat `action.Configuration` boilerplate. Use
  `helm.NewClient(restCfg, ns)` in production and `helm.NewMemoryClient(ns)`
  in tests.
- Don't shell out to the `helm` binary; use the SDK in-process.
- Always verify chart values against the upstream chart's current
  `values.yaml`. Don't rely on memory.

**Vendored upstream charts:**
- Live at `installer/operator/vendored-charts/<name>-<version>.tgz`,
  integrity-recorded in `SHA256SUMS`. The operator loads them via
  `helm.LoadArchive`. Refresh with `make vendor-charts` after updating the
  version + hash entries. The operator is the sole installer of cluster
  services — there is no `educates-cluster-services` umbrella chart.

**Readiness checks for cluster services:**
- `Deployment.status.availableReplicas == replicas` is necessary but not
  sufficient.
- For cert-manager: also verify API discovery responds (e.g.,
  `GET /apis/cert-manager.io/v1` returns 200).
- For Kyverno: similar webhook readiness check.
- For Contour: IngressClass exists and the controller is healthy.

**Operator image and namespace:**
- `make docker-build` builds a local image for dev; running `helm install`
  against the chart from a clone requires `make docker-build` + `kind
  load` first. Released images are published by the release workflow.
- The operator namespace is supplied via the `OPERATOR_NAMESPACE` env var
  (downward API in the chart Deployment). User-supplied Secrets referenced
  from `EducatesClusterConfig.spec.inline` must live there.

**Image relocation:**
- For air-gapped, prefer `helm dt wrap`/`unwrap` or equivalent at build
  time, not runtime.
- For online but mirrored registries, use the
  `EducatesClusterConfig.spec.imageRegistry.prefix` field.
- Don't replicate `kbld`-style runtime digest resolution in the operator;
  pin in published values files instead.

**Local dev environment:**
- Local kind cluster expects port 5001 for the local Docker registry.
- macOS users may have an `educates resolver` providing `*.educates.test`
  resolution to a local IP (typically `10.10.10.1`).

---

## Glossary

Educates uses domain-specific terms that have precise meanings:

- **Workshop** — a Kubernetes resource (Workshop CR) defining a workshop's
  content, environment requirements, session lifetime, etc. The recipe.
- **Workshop Environment** — a deployed instance of a workshop, ready to
  spawn sessions. Created by the training portal from a Workshop.
- **Workshop Session** — an actual user's session, with its own namespace,
  resources, and (optionally) vcluster.
- **Training Portal** — a deployed instance of the user-facing UI. Multiple
  training portals can exist on one Educates install, each serving
  different workshops.
- **Cluster service** — an operational dependency of Educates that lives at
  cluster scope (cert-manager, contour, kyverno, external-dns). Distinct
  from Educates components.
- **Educates component** — secrets-manager, lookup-service, session-manager.
  These three are individually deployable; session-manager depends on
  secrets-manager.
- **BYO** — Bring Your Own. Used when the user provides a cluster service
  themselves (their own cert-manager, their own ingress controller) and
  Educates uses it via External-mode discriminators or full Inline mode.
- **Managed mode** vs **Inline mode** — `EducatesClusterConfig.spec.mode`
  values. Managed = operator installs cluster services. Inline = user
  asserts what already exists.
- **Operator namespace** — the namespace where the operator runs and where
  it expects to find user-provided Secrets (TLS, CA, image-pull secrets)
  referenced by name in CRs.

---

## When in doubt

- If a question is about how to implement something concrete, ask in this
  session.
- If a question is architectural and there's no record of the decision,
  stop and ask rather than picking a direction.
- Default to asking. Asking is cheap.
