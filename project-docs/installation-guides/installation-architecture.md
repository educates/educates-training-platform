(installation-architecture)=
Installation Architecture
=========================

Installing Educates involves three different kinds of objects, each living at a different level of the stack:

1. **CLI configuration kinds** — local YAML files describing your scenario, consumed only by the `educates` CLI.
2. **Operator custom resources** — Kubernetes API objects driving the operator; the declarative contract of an installation.
3. **Helm charts** — the artifacts that actually put workloads on the cluster, installed either by you or by the operator.

The how-to guides ([CLI based](cli-based-installation), [Helm based](helm-based-installation)) tell you which commands to run; this document explains how the three layers relate and how each one's output becomes the next one's input, so you can predict, inspect, and debug what an installation does at every step.

The three layers
----------------

```
 you author                 you (or the CLI) apply           the operator installs
┌─────────────────────┐    ┌──────────────────────────┐    ┌─────────────────────────────┐
│ CLI config kind     │    │ educates-installer chart │    │ vendored cluster-service     │
│ cli.educates.dev    │───▶│ (operator + CRDs + RBAC) │    │ charts: cert-manager,        │
│ EducatesGKEConfig,  │    ├──────────────────────────┤    │ contour, kyverno,            │
│ EducatesEKSConfig,  │───▶│ 4 custom resources       │───▶│ external-dns   (Managed mode)│
│ EducatesLocalConfig,│    │ EducatesClusterConfig    │    ├─────────────────────────────┤
│ EducatesInlineConfig│    │ SecretsManager           │    │ vendored runtime subcharts:  │
│ EducatesConfig      │    │ LookupService            │    │ secrets-manager,             │
└─────────────────────┘    │ SessionManager           │    │ lookup-service,              │
   local file only         └──────────────────────────┘    │ session-manager (+ extras)   │
                              cluster API objects          └─────────────────────────────┘
                                                              Helm releases on the cluster
```

### CLI configuration kinds (`cli.educates.dev/v1alpha1`)

A single local YAML file, validated against a published JSON schema, never applied to the cluster. Each kind encodes one scenario and locks that scenario's invariants — `EducatesGKEConfig` *is* "Contour + ACME via CloudDNS + external-dns + Kyverno with Workload Identity", so the file only carries the handful of values that genuinely vary (project, domain, ACME email). `EducatesConfig` is the escape hatch that carries the custom resource specs verbatim when no scenario kind fits. See [configuration settings](configuration-settings) for every kind and field.

This layer is optional: it exists for convenience and only the CLI ever reads it.

### Operator custom resources (`config.educates.dev` / `platform.educates.dev`)

Four cluster-scoped singletons, all named `cluster`. This is the real declarative interface of an installation — whether the YAML was written by hand, rendered by the CLI, or synced by ArgoCD, the operator behaves identically.

* `EducatesClusterConfig` — cluster-wide concerns: ingress, certificates, DNS, policy enforcement. In **Managed** mode the operator installs the cluster services; in **Inline** mode you assert what already exists.
* `SecretsManager`, `LookupService`, `SessionManager` — one per Educates runtime component.

Two contracts shape this layer:

* **Status is the interface between resources.** The component resources never read `EducatesClusterConfig.spec`; they consume its `status` (the validated ingress contract, resolved policy engines, image registry). That is what makes Inline mode invisible to components — by the time status is published, Managed and Inline installs look the same.
* **Components gate on their dependencies.** `SessionManager` refuses to install until `EducatesClusterConfig` and `SecretsManager` report `Ready=True`, so apply order is a convenience, not a correctness requirement.

### Helm charts

Three groups, with different installers:

* **`educates-installer`** — the operator chart: operator Deployment, the four CRDs, RBAC. The only chart *you* (or your GitOps tool) install. The CLI embeds a copy and installs the same chart.
* **Vendored upstream charts** — cert-manager, Contour, Kyverno, external-dns, embedded in the operator image as integrity-checked tarballs. Installed by the `EducatesClusterConfig` reconciler in Managed mode; never installed in Inline mode.
* **`educates-training-platform`** — the runtime umbrella chart with `secrets-manager`, `lookup-service`, and `session-manager` subcharts (plus the optional `node-ca-injector` and `remote-access` extras). The operator installs the components from vendored copies of these subcharts, one Helm release per component resource. The umbrella is also installable standalone, without the operator, for users who want to manage runtime values directly with Helm.

How values flow through the pipeline
------------------------------------

Each stage takes the previous stage's object and derives the next one's values. Every intermediate can be inspected.

### Stage 1 — config kind → chart values + custom resources

The CLI loads your file, validates it against the kind's JSON schema (unknown fields are rejected), applies defaults (static ones like `lookupService: true`, derived ones like `cert-manager@<project>.iam.gserviceaccount.com`, and CLI-binary ones like the operator image tag), then the **translator** combines the kind's locked invariants with your fields to produce two things:

* values for the `educates-installer` chart (operator image, log level, image pull secrets), and
* the four custom resource manifests.

Inspect it — this prints exactly what stage 2 will apply, without touching the cluster:

```shell
educates admin platform render --config config.yaml
```

The output is deterministic for a given config file and CLI version. Committing it to Git is the handoff point from the CLI path to the GitOps path: from here on, nothing remembers that a CLI kind ever existed.

### Stage 2 — `educates-installer` chart → running operator

`helm install` (run by `educates admin platform deploy`, your pipeline, or GitOps) creates the CRDs, RBAC, and the operator Deployment. The operator starts idle: everything from here on is driven by the custom resources.

```shell
helm -n educates-installer get values educates-installer
```

### Stage 3 — `EducatesClusterConfig` → cluster-service chart values (Managed mode)

The `EducatesClusterConfig` reconciler renders a values map for each vendored upstream chart from the spec, then installs them with the in-process Helm SDK:

| Spec fields | Chart (release/namespace) | Derived values (examples) |
|---|---|---|
| `ingress.domain`, `ingress.ingressClassName`, `controller.bundledContour.*` | `contour` / `contour` | `contour.replicaCount`, `contour.ingressClass.{name,create,default}`, `envoy.service.type`, the `external-dns.kubernetes.io/hostname: *.<domain>.` Service annotation, also set as `external-dns.alpha.kubernetes.io/hostname` for external-dns releases before 0.22 |
| `ingress.certificates.bundledCertManager.*` | `cert-manager` / `cert-manager` | `crds.enabled`; the ClusterIssuer and wildcard Certificate are created directly by the operator, not through chart values |
| `dns.bundledExternalDNS.*` | `external-dns` / `external-dns` | `provider`, `sources`, `domainFilters: [<domain>]`, `txtOwnerId`, Workload Identity / IRSA ServiceAccount annotations |
| `policyEnforcement.*` | `kyverno` / `kyverno` | chart defaults plus `global.image.registry` when an image-registry prefix is set |

In Inline mode this stage installs nothing — the reconciler only validates that the referenced Secrets, IngressClass, and engines exist.

Either way, the stage ends by publishing **status**: the validated ingress contract (`status.ingress.domain`, wildcard certificate and CA Secret references, optional ClusterIssuer), resolved policy engines, image registry, the resolved extension package delivery (`status.packageDelivery.imageMount`, settled against what the cluster supports), and installed chart versions.

```shell
helm -n contour get values contour                  # what the operator computed for a cluster service
kubectl get educatesclusterconfig cluster -o yaml   # the published status contract
```

### Stage 4 — component resources + `EducatesClusterConfig.status` → runtime chart values

Each component reconciler waits for its gates, then merges its own spec with the cluster config's *status* into values for its vendored runtime subchart, and installs it into the shared `educates` namespace:

* `SecretsManager` → release `secrets-manager`: `logLevel`, image override, resources, pull secrets from `status.imageRegistry`.
* `LookupService` → release `lookup-service`: `ingress.host` composed as `<spec.ingress.prefix>.<status.ingress.domain>`, TLS reference from status (or the per-resource override), publishing the result as `status.url`.
* `SessionManager` → release `session-manager`: `clusterIngress.{domain,tlsCertificateRef,caCertificateRef}` from status, `clusterSecurity.policyEngine` from status, `packageDelivery.imageMount` from status (which decides whether extension packages declared as an image are mounted into a workshop session or have their contents fetched), plus everything from its own spec (themes, analytics, storage, network, ingress overrides). Its `nodeCATrust` and `remoteAccess` modes decide two extra releases, `node-ca-injector` and `remote-access`.

```shell
helm -n educates list                              # one release per component
helm -n educates get values session-manager       # the merged values the operator computed
```

### Stage 5 — session-manager chart → runtime configuration

The session-manager chart serialises its values into the `educates-config` Secret (key `educates-operator-config.yaml`, plus the per-workshop policy feed in `kyverno-policies.yaml`). This blob is what the Educates runtime actually reads when creating training portals and workshop sessions — the last stop of the pipeline.

```shell
kubectl -n educates get secret educates-config \
  -o jsonpath='{.data.educates-operator-config\.yaml}' | base64 -d
```

Worked example: one field, end to end
-------------------------------------

Following `domain: workshops.example.com` from an `EducatesGKEConfig` file through every stage:

1. **Config kind** — `domain: workshops.example.com`, validated by the `EducatesGKEConfig` schema.
2. **Custom resource** — the translator emits it as `EducatesClusterConfig.spec.ingress.domain` (and the kind's invariants fill in Contour, ACME/CloudDNS, external-dns around it).
3. **Cluster-service values** — the reconciler fans it out: the Envoy Service is annotated `external-dns.kubernetes.io/hostname: *.workshops.example.com.` (and with the same value under the older `external-dns.alpha.kubernetes.io/hostname` key), the wildcard Certificate requests `*.workshops.example.com`, external-dns gets `domainFilters: [workshops.example.com]`. Once the certificate is issued, `status.ingress.domain: workshops.example.com` is published together with the wildcard Secret reference.
4. **Runtime values** — the `SessionManager` reconciler reads it *from status* and sets the session-manager chart's `clusterIngress.domain`; the `LookupService` reconciler composes `lookup.workshops.example.com`.
5. **Runtime configuration** — the chart writes `clusterIngress.domain` into the `educates-config` Secret, and from there every training portal and workshop session URL is minted under `*.workshops.example.com`.

Where to enter the pipeline
---------------------------

| You want | Enter at | You maintain |
|---|---|---|
| Quickest path, laptop or cloud | Stage 1 — a CLI config kind | one small YAML file |
| GitOps (ArgoCD, Flux) | Stage 2/applying resources — chart + four custom resources (optionally rendered once from a config kind) | chart version + four manifests |
| Runtime via plain Helm, no operator | the `educates-training-platform` umbrella chart | full chart values, and the cluster services are entirely yours |

Whichever entry point you choose, the later stages are identical — which is also the debugging recipe: when something looks wrong, walk the stages in order (`render` output → custom resource status conditions → `helm get values` of the suspect release → the `educates-config` Secret) and find the first one that doesn't match your intent.
