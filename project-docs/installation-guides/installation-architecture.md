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
* **`educates-training-platform`** — the runtime umbrella chart with `secrets-manager`, `lookup-service`, and `session-manager` subcharts (plus the optional `node-ca-injector` and `remote-access` extras). The operator installs the components from vendored copies of these subcharts, one Helm release per component resource. The umbrella is also installable standalone, without the operator, for users who want to manage runtime values directly with Helm. Its custom resource definitions then need applying by hand before every install and upgrade, as [Helm based installation](helm-based-installation) describes.

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
| `policyEnforcement.*` | `kyverno` / `kyverno` | chart defaults |

When `imageRegistry.prefix` is set, each chart also gets the values that move all of its images, hooks included, to their upstream repository paths under the prefix: `imageRegistry` for cert-manager, `global.imageRegistry` for Contour, `image.repository` for external-dns, and `global.image.registry`, `webhooksCleanup.image.registry` and `test.image.registry` for Kyverno.

The external-dns chart carries two custom resource definitions, `DNSEndpoint` and `DNSRecord`. Helm applies a chart's custom resource definitions only when it first installs a release, so the reconciler applies these itself before every install or upgrade of `external-dns`, and has Helm skip them, which makes the operator their only writer. An operator upgrade that bundles a newer external-dns brings their new schemas with it. Custom resource definitions are shared by the whole cluster, and the operator always replaces these with the version it bundles, so any other external-dns installation on the same cluster runs against that version too. cert-manager, Contour and Kyverno ship their custom resource definitions as part of their releases, which upgrade them along with everything else.

With the `StaticCertificate` certificates provider no cert-manager is installed: the reconciler checks the wildcard certificate you name and copies only its `tls.crt` and `tls.key` into the operator-owned `educates-wildcard-tls`, which it publishes in place of a cert-manager issued one.

Kyverno is installed only with the `Bundled` Kyverno provider, and only when the `clusterPolicy` or `workshopPolicy` engine is `Kyverno`. With either provider, `Bundled` or `External`, a `Kyverno` engine also makes the stage wait until the cluster serves the Kyverno `ValidatingPolicy` kinds Educates creates, with the `PolicyEnforcementReady` condition `False` and reason `KyvernoNotFound` until it does.

In Inline mode this stage installs nothing: the reconciler only validates that the referenced Secrets and IngressClass exist, and that Kyverno serves the policy kinds Educates creates when a declared engine is `Kyverno`, checks the wildcard certificate as it checks a `StaticCertificate`, and derives `educates-ingress-ca` from the CA Secret when one is named.

Either way, the stage ends by publishing **status**: the validated ingress contract (`status.ingress.domain`, wildcard certificate and CA Secret references, optional ClusterIssuer; whenever a CA is configured, in either mode, the CA Secret reference names `educates-ingress-ca`, an operator-owned Secret in the operator namespace holding only `ca.crt`, which the operator derives from your CA Secrets and keeps up to date), resolved policy engines, image registry, the resolved extension package delivery (`status.packageDelivery.imageMount`, settled against what the cluster supports), and installed chart versions.

```shell
helm -n contour get values contour                  # what the operator computed for a cluster service
kubectl get educatesclusterconfig cluster -o yaml   # the published status contract
```

### Stage 4 — component resources + `EducatesClusterConfig.status` → runtime chart values

Each component reconciler waits for its gates, then merges its own spec with the cluster config's *status* into values for its vendored runtime subchart, and installs it into the shared `educates` namespace:

* `SecretsManager` → release `secrets-manager`: `logLevel`, image override, resources, pull secrets from `status.imageRegistry`.
* `LookupService` → release `lookup-service`: `ingress.host` composed as `<spec.ingress.prefix>.<status.ingress.domain>`, TLS reference from status (or the per-resource override), publishing the result as `status.url`.
* `SessionManager` → release `session-manager`: `clusterIngress.{domain,tlsCertificateRef,caCertificateRef}` from status, `clusterSecurity.policyEngine` and `workshopSecurity.rulesEngine` from status, the latter replaced by its `workshopPolicyOverride` when one is set, `packageDelivery.imageMount.enabled`, set from the resolved `status.packageDelivery.imageMount` (which decides whether extension packages declared as an image are mounted into a workshop session or have their contents fetched), plus everything from its own spec (themes, analytics, storage, network, ingress overrides). Its `nodeCATrust` and `remoteAccess` modes decide two extra releases, `node-ca-injector` and `remote-access`. In `Auto` mode, `node-ca-injector` is installed whenever status publishes a CA Secret reference or the `SessionManager` sets a CA override, with the CA the session manager uses as its `clusterIngress.caCertificateRef`: `educates-session-manager-ingress-ca` when overridden, otherwise the reference from status. While either engine is `Kyverno`, the session manager installs only once the cluster serves the Kyverno policy kinds Educates creates, because its chart installs Kyverno policies and its runtime creates them for every workshop environment; its `KyvernoAvailable` condition reports the outcome.

Every runtime release also takes its images from `status.imageRegistry`, which mirrors `spec.imageRegistry` in Managed mode and `spec.inline.imageRegistry` in Inline mode. With a prefix, the chart's `development.imageRegistry` is set so that its Educates images, the session manager's image inventory included, keep their upstream repository paths under the prefix, as the cluster-service images do: the host is the prefix up to its first slash, and the namespace is the rest of the prefix followed by the chart's `educates.dev/image-registry-namespace` annotation. The pull secrets are named in the chart's `imagePullSecrets`, though the Secrets themselves stay in the operator namespace.

Each runtime subchart also carries the custom resource definitions of its component: the `secrets.educates.dev` kinds such as `SecretCopier` for `secrets-manager`, the `training.educates.dev` kinds such as `Workshop` and `TrainingPortal` for `session-manager`, and the `lookup.educates.dev` kinds for `lookup-service`. Helm applies a chart's custom resource definitions only when it first installs a release, so the reconciler applies them itself before every install or upgrade of the release, and has Helm skip them, which makes the operator their only writer. After an operator upgrade they carry the schemas of the new release, and a field a new release adds to one of them is kept on the resources you write. The `session-manager` and `lookup-service` releases create `SecretCopier` resources themselves, so after an operator upgrade their reconcilers wait until `SecretsManager` reports the `secrets-manager` chart version the new operator bundles, which it does once its release has converged and its custom resource definitions are current. Until then `SessionManager` and `LookupService` report `WaitingForSecretsManager`. Deleting a component, or Educates as a whole, leaves these custom resource definitions in place, together with every resource of their kinds.

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
