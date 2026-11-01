(configuration-settings)=
Configuration Settings
======================

Educates installations driven by the CLI are described by a single YAML configuration file using one of the `cli.educates.dev/v1alpha1` kinds. Each kind targets a scenario: the narrow kinds (`EducatesLocalConfig`, `EducatesGKEConfig`, `EducatesEKSConfig`, `EducatesInlineConfig`) expose only the choices that scenario actually has, while `EducatesConfig` is the escape hatch giving full control over the underlying custom resources.

Every kind has a published JSON schema at `https://schemas.educates.dev/cli/v1alpha1/<Kind>.json`. Add a modeline at the top of your file to get completion and validation in any editor with a YAML language server:

```yaml
# yaml-language-server: $schema=https://schemas.educates.dev/cli/v1alpha1/EducatesGKEConfig.json
```

Files created by `educates local config init` include the modeline automatically.

You can also describe the fields of a kind from the command line, without a cluster or a configuration file, using `educates local config explain`. It reads the same embedded schema and prints a field's type, default and description in the style of `kubectl explain`:

```
educates local config explain ingress.insecure
educates local config explain gcp.project --kind gke
```

The argument is a dotted field path; with no argument the top-level fields are listed. The kind defaults to `EducatesLocalConfig` and is chosen with `--kind`, which accepts a kind name or a short alias (`local`, `gke`, `eks`, `inline`, `escape`). The descriptions are the same ones the schema gives editors on hover, so a field documented here is documented in your editor too.

EducatesLocalConfig
-------------------

The laptop scenario: a local kind cluster with operator-managed cluster services and a self-signed CA. This is the only kind that lives at a fixed location (`<data-home>/config.yaml`, where `<data-home>` is `$XDG_DATA_HOME/educates`, overridable via `EDUCATES_CLI_DATA_HOME`) and is managed with the `educates local config` commands rather than edited as a project file. See [local environment](local-environment).

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesLocalConfig
ingress:
  domain: workshops.educates.test    # empty = <host-IP>.nip.io fallback
cluster:
  listenAddress: 127.0.0.1
lookupService: true                  # default true
clusterAdmin: true                   # default true
operator:
  logLevel: info
```

Key fields (all optional — an `apiVersion` + `kind` stub is a valid config):

* `ingress.domain` — wildcard ingress domain. When empty and deploying with `--local-config`, the CLI falls back to `<host-IP>.nip.io`.
* `cluster.*` — kind cluster shape: `listenAddress`, API server overrides, pod/service subnets, host volume mounts, registry pull-through mirrors.
* `resolver.*` — local DNS resolver settings (macOS `*.educates.test` style resolution).
* `clusterAdmin`, `lookupService` — component toggles (both default `true`).
* `secretPropagation.imagePullSecretNames` — locally cached pull secrets to propagate into the cluster.
* `imageVersions` — per-image reference overrides.
* `operator.image.*`, `operator.imagePullSecrets`, `operator.logLevel` — operator deployment settings. Image repository and tag default from the CLI binary's own version, so they normally stay unset.

Settings outside the laptop scenario — DNS providers, ACME, image registry prefixes, alternative ingress — are deliberately rejected by this kind's schema. Use `EducatesConfig` for those.

EducatesGKEConfig
-----------------

The GKE production scenario: Contour with a LoadBalancer service, cert-manager issuing a wildcard certificate via ACME with the CloudDNS DNS01 solver, external-dns managing the DNS records, Kyverno policy enforcement. Authentication to Google Cloud uses Workload Identity.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesGKEConfig
gcp:
  project: my-gcp-project
  # certManagerServiceAccount / externalDNSServiceAccount default to
  # cert-manager@{project}.iam.gserviceaccount.com / external-dns@...
domain: workshops.example.com
acme:
  email: admin@example.com
  # server defaults to the Let's Encrypt production endpoint
```

`gcp.project`, `domain` and `acme.email` are required. The component toggles (`lookupService`, `clusterAdmin`, ...) and `operator` block from `EducatesLocalConfig` are available here too. When TLS for the ingress domain is terminated outside the cluster (a cloud load balancer or proxy forwarding plain HTTP inward), set `externalTLSTermination: true` so generated portal and workshop URLs use `https` — see [secure HTTP connections](secure-http-connections). The scenario's other architecture choices are locked — to deviate, use `EducatesConfig`.

See [infrastructure providers](infrastructure-providers) for the Google Cloud IAM and DNS zone prerequisites.

EducatesEKSConfig
-----------------

The EKS equivalent: the same managed stack, with ACME using the Route53 DNS01 solver and IAM Roles for Service Accounts (IRSA) for authentication.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesEKSConfig
aws:
  accountId: "123456789012"
  region: us-east-1
  route53HostedZoneId: Z0123456789ABCDEF
  # certManagerRoleARN / externalDNSRoleARN default to
  # arn:aws:iam::{accountId}:role/educates-cert-manager / educates-external-dns
domain: workshops.example.com
acme:
  email: admin@example.com
```

`aws.accountId`, `aws.region`, `aws.route53HostedZoneId`, `domain` and `acme.email` are required. As with the GKE kind, `externalTLSTermination: true` asserts `https` URLs when TLS is terminated at an external load balancer.

(defining-configuration-for-ingress)=
EducatesInlineConfig
--------------------

The bring-your-own scenario: the cluster already has an ingress controller, a wildcard TLS certificate, and (optionally) a policy engine, and Educates integrates with them instead of installing anything at cluster scope. This is the path for OpenShift and for shared or centrally-managed clusters.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesInlineConfig
domain: workshops.example.com
ingressClassName: contour                      # or e.g. openshift-default
wildcardCertificateSecret: wildcard-tls        # kubernetes.io/tls Secret for *.{domain}
caCertificateSecret: corporate-ca              # optional, for non-public CAs
policyEnforcement:
  clusterEngine: Kyverno                       # Kyverno | PodSecurityStandards | OpenShiftSCC | None
  workshopEngine: Kyverno                      # Kyverno | None
imageRegistry:
  prefix: registry.internal/educates           # optional mirror prefix
externalTLSTermination: false                  # true when a proxy/LB terminates TLS in front of the cluster
```

`domain`, `ingressClassName` and `wildcardCertificateSecret` are required. The referenced Secrets must exist in the operator's namespace before deployment. Educates components never receive the `caCertificateSecret` itself, only a copy of its `ca.crt`. To keep the Secrets in another namespace, use the `EducatesConfig` escape hatch, whose `inline.ingress.wildcardCertificateSecretRef` and `inline.ingress.caCertificateSecretRef` take an optional `namespace`. See [secure HTTP connections](secure-http-connections) for the certificate options across all scenarios.

Note that with `workshopEngine: None` there is no workshop-level security policy enforcement. See the [cluster requirements](cluster-requirements) discussion before serving untrusted users. When either engine is `Kyverno`, the operator checks that the Kyverno you installed serves the policy kinds Educates creates, and the installation does not become ready until it does.

EducatesConfig (escape hatch)
-----------------------------

`EducatesConfig` carries the four custom resource specs verbatim, with no CLI defaulting and no locked invariants — every field of the CRDs is reachable. Use it when a narrow kind almost fits but you need to deviate, or for scenarios with no dedicated kind.

```yaml
apiVersion: cli.educates.dev/v1alpha1
kind: EducatesConfig
target:
  provider: kind            # optional; controls CLI side effects (local cluster bootstrap)
educatesClusterConfig:
  # verbatim EducatesClusterConfig.spec
  mode: Managed
  ingress:
    domain: workshops.example.com
  policyEnforcement:        # required in Managed mode, see below
    clusterPolicy:
      engine: Kyverno
    workshopPolicy:
      engine: Kyverno
    kyverno:
      provider: Bundled
secretsManager: {}
lookupService: {}           # omit the block entirely to not deploy the component
sessionManager: {}
```

The `target` block only drives side effects of the CLI, and `kind` is its only `provider`. With `provider: kind`, `educates local cluster create --config <file>` creates the local kind cluster from `target.cluster`, which takes the same fields as the `cluster` block of `EducatesLocalConfig`, and then deploys the custom resources as the file declares them. None of the defaults of `EducatesLocalConfig` apply to the custom resources: the ingress domain is not derived from the host IP address, no certificate is chosen from the local secrets cache, and the lookup service is deployed only when the `lookupService` block is present. The loopback service that `educates serve-workshop` relies on resolves under the ingress domain of the `educatesClusterConfig` block. `target.resolver` takes the same fields as the `resolver` block of `EducatesLocalConfig`, and `educates local resolver deploy --config <file>` sets up the local DNS resolver for that same ingress domain.

The CLI copies no Secrets from its local secrets cache into the cluster for an `EducatesConfig`, so you create any Secret the custom resources reference, such as a CA key pair or a wildcard certificate, in the cluster yourself. To create them before Educates is deployed, run `educates local cluster create --cluster-only --config <file>`, create the Secrets, and then run `educates admin platform deploy --config <file>`.

For example, the `StaticCertificate` certificates provider, which serves a wildcard certificate you already have and installs no cert-manager, is selected through this kind:

```yaml
educatesClusterConfig:
  mode: Managed
  ingress:
    domain: workshops.example.com
    ingressClassName: contour
    controller:
      provider: BundledContour
    certificates:
      provider: StaticCertificate
      staticCertificate:
        tlsSecretRef:               # tls.crt and tls.key, SANs including *.{domain}
          name: wildcard-tls
          namespace: educates-secrets
        caCertificateSecretRef:     # optional, ca.crt of a CA that is not publicly trusted
          name: wildcard-ca
          namespace: educates-secrets
```

See [secure HTTP connections](secure-http-connections) for how the operator checks and serves the certificate.

In `Managed` mode, `educatesClusterConfig.policyEnforcement` is required. Workshop security is enforced by Kyverno, and both the `clusterPolicy` and the `workshopPolicy` engines default to `Kyverno`, so whenever either engine is `Kyverno` the `kyverno` block must say how Kyverno is provided: `Bundled` has the operator install it, and `External` uses a Kyverno already installed in the cluster. Setting both engines to `None` runs Educates without workshop security, which is only acceptable for trusted users (see [cluster requirements](cluster-requirements)). A spec that leaves out either block fails validation before any cluster service is installed, with the `ValidationSucceeded` condition of the `EducatesClusterConfig` naming the missing field.

```yaml
educatesClusterConfig:
  mode: Managed
  policyEnforcement:
    clusterPolicy:
      engine: Kyverno         # Kyverno | None
    workshopPolicy:
      engine: Kyverno         # Kyverno | None
    kyverno:
      provider: External      # Bundled | External
```

Whichever provider supplies Kyverno, the operator checks that the cluster serves the Kyverno policy kinds Educates creates. Until it does, the `PolicyEnforcementReady` condition of the `EducatesClusterConfig` is `False` with reason `KyvernoNotFound`, and the installation does not become ready. The `workshopPolicyOverride` of the `sessionManager` block replaces the cluster's workshop engine for the session manager. When it selects `Kyverno`, the session manager is installed only once Kyverno is served, as its `KyvernoAvailable` condition reports:

```
kubectl get sessionmanager cluster \
  -o jsonpath='{.status.conditions[?(@.type=="KyvernoAvailable")].message}'
```

The spec blocks are validated by the cluster's CRD schemas at apply time, not by the CLI. The `EducatesConfig` JSON schema is generated from the CRDs, so editors still get full completion. For the custom resource spec reference, see the sample scenarios in [installer/samples](https://github.com/educates/educates-training-platform/tree/develop/installer/samples) and `kubectl explain educatesclusterconfig.spec` against an installed cluster.

(delivery-of-extension-packages)=
Delivery of extension packages
------------------------------

An extension package declared as an image reaches a workshop session in one of two ways. It is either mounted into the session read only, which needs support from the cluster, or its contents are fetched, which works anywhere. Workshop authors write the same workshop definition either way.

Which one is used is controlled by `packageDelivery.imageMount` on the `EducatesClusterConfig` custom resource, reachable via the `educatesClusterConfig` block of `EducatesConfig`:

```yaml
educatesClusterConfig:
  packageDelivery:
    imageMount: Auto        # Auto | Enabled | Disabled
```

``Auto`` is the default and mounts only where the cluster supports it. The operator checks that the Kubernetes API server is version 1.36 or newer, and that every Linux node reports a kubelet of version 1.35 or newer together with ``containerd`` 2.1 or newer or ``CRI-O`` 1.31 or newer. Where any of these is not met, package contents are fetched instead.

``Enabled`` mounts whatever the check finds. Use it on a cluster which supports mounting but which the operator cannot recognise, such as one reporting an unfamiliar container runtime. ``Disabled`` always fetches.

The value in effect is published on the custom resource as ``status.packageDelivery.imageMount``, and the ``PackageImageMountAvailable`` condition explains the outcome, naming the API server version or the specific nodes which fall short:

```
kubectl get educatesclusterconfig cluster \
  -o jsonpath='{.status.conditions[?(@.type=="PackageImageMountAvailable")].message}'
```

This condition is reported separately from the readiness of the installation, because fetching package contents is a normal way to run Educates rather than a fault. The setting is re-evaluated whenever a node joins the cluster, leaves it, or is upgraded, so a cluster which grows support for mounting starts using it without the operator being restarted.

Delivery is settled for a workshop environment when that environment is created, and every session of it follows that choice. Changing this setting therefore affects only workshop environments created afterwards.

This setting applies to workshops deployed to a cluster. A workshop deployed locally with ``educates docker workshop deploy`` makes the same choice against the local Docker daemon instead, and has its own option for overriding it. See [declaring an extension package as an image](declaring-an-extension-package-as-an-image).

(overriding-container-runtime-class)=
(restricting-session-manager-permissions)=
(restricting-network-access)=
(image-registry-pull-through-cache)=
(tracking-using-google-analytics)=
(tracking-using-microsoft-clarity)=
(tracking-using-amplitude)=
(overriding-styling-of-the-workshop)=
(allowing-sites-to-embed-workshops)=
(overriding-session-cookie-domain)=
Session manager settings
------------------------

Runtime behavior settings live on the `SessionManager` custom resource spec, reachable via the `sessionManager` block of `EducatesConfig` (or directly when applying resources yourself):

* `ingressOverrides`: overrides of the cluster's ingress TLS Secret (`tlsSecretRef`, named in the namespace of the cluster's wildcard certificate) and CA (`caCertificateSecretRef`, a Secret holding `ca.crt`, with an optional `namespace` that defaults to the operator namespace; the session manager receives only its `ca.crt`, through the operator-owned `educates-session-manager-ingress-ca`), plus `protocol` to assert `https` URLs when TLS is terminated outside the cluster (this is what the kinds' `externalTLSTermination` translates to).
* `tracking` — analytics integrations (Google Analytics, Amplitude, Microsoft Clarity, webhooks).
* `sessionCookieDomain` — share the authentication cookie across subdomains.
* `allowedEmbeddingHosts` — sites permitted to embed workshop sessions (CSP frame ancestors).
* `storage` — storage class plus user/group fixups for NFS-style storage providers.
* `network` — packet size (MTU) and blocked CIDR ranges for workshop sessions.
* `images` — per-image overrides for runtime-spawned images.
* `themes`/`defaultTheme` — Secret-sourced workshop themes.
* `imagePrePuller` — pre-pull key images on cluster nodes.
* `nodeCATrust`, `remoteAccess`: node-level CA trust injection and cross-cluster CLI access. With `nodeCATrust.mode` at its default, `Auto`, the node CA injector is installed whenever the cluster publishes a CA, which includes every Managed mode install with a custom CA, every `StaticCertificate` install that names a CA trust Secret, and every Inline mode install that names a CA Secret, and also whenever `ingressOverrides.caCertificateSecretRef` is set. Nodes trust the CA the session manager uses: the override's `educates-session-manager-ingress-ca` when one is set, otherwise the cluster's `educates-ingress-ca`. With `Enabled`, the `SessionManager` reports `Ready` as `False` only when neither the cluster nor an override provides a CA. Set it to `Disabled` to keep the privileged node CA injector DaemonSet off.

Use `kubectl explain sessionmanager.spec` for the full schema. A few spec blocks (`defaultAccessCredentials`, `registryMirrors`, and ConfigMap/URL-sourced themes) are reserved in the CRD but rejected as not yet supported in this release.

Updating settings
-----------------

Configuration is applied by re-running `educates admin platform deploy` with the changed file (or re-applying the custom resources in a GitOps flow). The operator reconciles differences in place. The `EducatesClusterConfig` mode (`Managed` vs `Inline`) and how each of its cluster services is provided are immutable once set: the certificates provider (`ingress.certificates.provider`), the ingress controller provider (`ingress.controller.provider`), the DNS provider (`dns.provider`) and the policy enforcement engines and Kyverno provider (`policyEnforcement`). Switching any of them requires deleting and re-creating the installation, for example with `educates admin platform delete` followed by a new deploy. On the local configuration, switching between plain HTTP and HTTPS, by setting or clearing `ingress.insecure` or by setting a custom `ingress.domain` in place of the `nip.io` default, changes the certificates provider. Configuration changes will not necessarily affect training portals or workshop environments which already exist.
