Helm Based Installation
=======================

The instructions below pertain to installing Educates into an existing Kubernetes cluster using Helm and `kubectl` directly, without the Educates CLI. This is the appropriate path for GitOps tooling (ArgoCD, Flux) or wherever you want to manage the installation as plain Kubernetes resources.

The moving parts are described in [installation instructions](installation-instructions): one Helm chart installing the operator and CRDs, then four custom resources driving it.

Installing the operator
-----------------------

The `educates-installer` chart is published to an OCI registry with every release:

```shell
helm install educates-installer oci://ghcr.io/educates/charts/educates-installer \
  --version X.Y.Z \
  --namespace educates-installer --create-namespace
```

This installs the operator and the four CRDs. At this point nothing else has happened — the operator waits for custom resources.

Applying the platform resources
-------------------------------

Author the four custom resources for your scenario. Worked examples for each supported scenario live in [installer/samples](https://github.com/educates/educates-training-platform/tree/develop/installer/samples) in the project repository, and the [configuration settings](configuration-settings) documentation describes the specs. If you use the Educates CLI anywhere, `educates admin platform render --config config.yaml` emits ready-to-apply resources for a high-level config file.

Apply them in dependency order:

```shell
kubectl apply -f educates-cluster-config.yaml
kubectl wait --for=condition=Ready educatesclusterconfig/cluster --timeout=600s

kubectl apply -f educates-secrets-manager.yaml
kubectl apply -f educates-lookup-service.yaml       # optional
kubectl apply -f educates-session-manager.yaml

kubectl wait --for=condition=Ready secretsmanager/cluster sessionmanager/cluster --timeout=600s
```

Strict ordering is a convenience, not a requirement: each component gates itself on its dependencies' status (`SessionManager` refuses to proceed until `EducatesClusterConfig` and `SecretsManager` report ready), so applying everything at once also converges. Waiting per resource just gives clearer feedback on where a problem lies. The status conditions on each resource are the troubleshooting surface:

```shell
kubectl describe educatesclusterconfig cluster
```

Note that any Secrets referenced by name from `EducatesClusterConfig` (for example a wildcard TLS certificate Secret in Inline mode, or a custom CA) must live in the operator's namespace (`educates-installer` above) before the resource can become ready.

GitOps
------

The chart plus the four custom resources are the entire GitOps surface. With Flux:

```yaml
apiVersion: source.toolkit.fluxcd.io/v1
kind: HelmRepository
metadata:
  name: educates
  namespace: educates-installer
spec:
  type: oci
  url: oci://ghcr.io/educates/charts
  interval: 1h
---
apiVersion: helm.toolkit.fluxcd.io/v2
kind: HelmRelease
metadata:
  name: educates-installer
  namespace: educates-installer
spec:
  chart:
    spec:
      chart: educates-installer
      version: "X.Y.Z"
      sourceRef:
        kind: HelmRepository
        name: educates
  interval: 1h
```

With ArgoCD, register `ghcr.io/educates/charts` as an OCI Helm repository and create an Application with `chart: educates-installer`. Put the four custom resources in a second Application (or a later sync wave) so they apply after the CRDs exist; from there the operator's own dependency gating handles ordering.

Uninstalling
------------

Delete the custom resources before uninstalling the chart, so the operator's finalizers can drain what they installed:

```shell
kubectl delete sessionmanager/cluster lookupservice/cluster secretsmanager/cluster
kubectl delete educatesclusterconfig/cluster
helm uninstall educates-installer --namespace educates-installer
```

Delete `EducatesClusterConfig` last: its finalizer removes the operator-managed cluster services (Kyverno among them), and the platform components need those services present while draining. The components also use Secrets the `EducatesClusterConfig` owns, which go with it: `educates-ingress-ca` and the copies of the image pull secrets in the `educates` namespace. In either mode, a deleted `EducatesClusterConfig` therefore waits while any platform resource exists, reporting `Ready` as `False` with the reason `PlatformCRsPresent` and naming the resources, and finishes once they are gone. Uninstalling the chart first would remove the operator that processes the finalizers, wedging deletion.

Installing the runtime chart without the operator
-------------------------------------------------

The `educates-training-platform` chart installs the runtime components, `secrets-manager`, `lookup-service` and `session-manager`, straight from Helm with no operator. You then provide the cluster services yourself and set the chart's values directly, as `helm show values` lists them.

The chart's subcharts carry the custom resource definitions of the runtime components, the `secrets.educates.dev`, `lookup.educates.dev` and `training.educates.dev` kinds, in their `crds/` directories. Helm applies a chart's custom resource definitions only when it installs a release, never when it upgrades or uninstalls one. Apply them yourself before every install and upgrade, and tell Helm to leave them alone:

```shell
helm show crds oci://ghcr.io/educates/charts/educates-training-platform --version X.Y.Z \
  | kubectl apply --server-side --force-conflicts -f -

helm install educates-runtime oci://ghcr.io/educates/charts/educates-training-platform \
  --version X.Y.Z \
  --namespace educates --create-namespace \
  --skip-crds \
  --values values.yaml
```

To upgrade, apply the custom resource definitions of the new version the same way, then run `helm upgrade` with that version. Skipping that step leaves the custom resource definitions at the schema of the release that first installed them, and the API server then drops every field a newer release adds from the resources you write.

`--force-conflicts` lets `kubectl` take over the fields that Helm, or an earlier apply under another field manager, set. `--skip-crds` matters with Helm 4, which creates a chart's custom resource definitions with a server-side apply that does not force conflicts: once they exist under another field manager, a `helm install` without `--skip-crds` can fail with `conflict occurred while applying object`.

`helm uninstall` leaves the custom resource definitions in place, together with every resource of their kinds. Delete them, with `helm show crds ... | kubectl delete -f -`, only when you want those resources gone as well.

With Flux, set `spec.install.crds` and `spec.upgrade.crds` to `CreateReplace` on the `HelmRelease`. Its default for both, `Create`, never updates a custom resource definition that already exists.

Air-gapped and mirrored registries
----------------------------------

See [air-gapped installation](airgapped-installation) for mirroring the release images into an internal registry and pointing the installation at it.
