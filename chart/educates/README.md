# Educates Helm Chart

Helm chart for the **Educates package** (session manager, secrets manager, operator configuration, and related CRDs). This chart is derived from the Educates ytt overlays in `carvel-packages/installer/bundle/config/ytt/_ytt_lib/packages/educates/` and is intended to be as configurable as possible via `values.yaml`.

## What this chart installs

- **Namespace** (default: `educates`)
- **Config secret** containing the full Educates operator configuration (and optional Kyverno policies)
- **Service accounts**: session-manager, image-puller, secrets-manager
- **ClusterRoles and ClusterRoleBindings** for session manager, secrets manager, and (optionally) pod-security-policies or security-context-constraints
- **Deployments**: session-manager, secrets-manager
- **Optional**: image-puller DaemonSet, node-injector (CA injection), inline TLS/CA secrets, default-website-theme secret
- **CRDs**: SecretCopier, SecretInjector, SecretImporter, SecretExporter (secrets manager)
- **Conditional**: PodSecurityPolicies (when `clusterSecurity.policyEngine: pod-security-policies`), SecurityContextConstraints (when `clusterSecurity.policyEngine: security-context-constraints`)

## Training CRDs

The **training** CRDs (TrainingPortal, Workshop, WorkshopSession, WorkshopEnvironment, WorkshopAllocation, WorkshopRequest) are required for the session manager but are not included in this chart due to size and versioning. Install them from the repository using ytt, or use the Educates CLI/Carvel package to install Educates (including CRDs) first. See [Helm Installation](../../project-docs/installation-guides/helm-installation.md) in the project docs.

## Quick start

```bash
# From the repository root
helm upgrade --install educates ./chart/educates \
  --namespace educates \
  --create-namespace \
  -f my-values.yaml
```

Minimal `my-values.yaml` example:

```yaml
clusterIngress:
  domain: workshops.example.com
  tlsCertificateRef:
    namespace: default
    name: workshops.example.com-tls
```

## Configuration

All values mirror the Educates package schema. Key sections:

| Section | Description |
|--------|-------------|
| `operator` | Namespace, API group, name prefix |
| `sessionManager` | `clusterAdmin` (default true) |
| `imageRegistry` | Host and namespace for Educates images |
| `imageVersions` | List of `{name, image}` overrides |
| `clusterIngress` | Domain, class, protocol, TLS/CA certs or refs |
| `clusterSecurity` | `policyEngine`: none, pod-security-policies, pod-security-standards, security-context-constraints, kyverno |
| `workshopSecurity` | `rulesEngine`: kyverno or none |
| `trainingPortal` | Default admin/robot credentials and OAuth client |
| `clusterStorage` | Storage class, user, group |
| `clusterSecrets` | `pullSecretRefs` for image pull secrets |
| `imagePuller` | Enable and list images to pre-pull |
| `websiteStyling` | HTML/script/style overrides and theme refs |

See [Configuration Settings](../../project-docs/installation-guides/configuration-settings.md) and the chart’s `values.yaml` for full options.

## Requirements

- Kubernetes >= 1.25
- Ingress controller and TLS (or external termination) configured separately
- Training CRDs installed (see above) for session manager to reconcile TrainingPortal/Workshop resources

## Verifying the chart

From the repository root:

```bash
helm lint chart/educates
helm template test chart/educates --namespace educates
```

## Documentation

- [Helm Installation](https://github.com/vmware-tanzu-labs/educates-training-platform/blob/main/project-docs/installation-guides/helm-installation.md) (in project docs)
- [Configuration Settings](https://github.com/vmware-tanzu-labs/educates-training-platform/blob/main/project-docs/installation-guides/configuration-settings.md)
