(installation-instructions)=
Installation Instructions
=========================

The installation instructions given here are only needed if you are installing into a dedicated Kubernetes cluster and not using the [local Educates environment](quick-start-guide). Ensure you have read the general documentation about [cluster requirements](cluster-requirements) before proceeding with trying to install Educates into an existing Kubernetes cluster.

How installation works
----------------------

Educates is installed in two layers:

1. The **`educates-installer` Helm chart** installs a Kubernetes operator along with four custom resource definitions (CRDs).
2. **Four custom resources** (all cluster scoped, all singletons named `cluster`) then drive the operator:

   * `EducatesClusterConfig` (`config.educates.dev/v1alpha1`) — cluster-wide infrastructure and services: ingress, TLS certificates, DNS, and security policy enforcement.
   * `SecretsManager` (`platform.educates.dev/v1alpha1`) — the secrets management component.
   * `LookupService` (`platform.educates.dev/v1alpha1`) — the optional lookup service component.
   * `SessionManager` (`platform.educates.dev/v1alpha1`) — the workshop session management component (requires `SecretsManager`).

The `EducatesClusterConfig` resource has two modes:

* **Managed mode** — the operator installs and manages the cluster services Educates depends on (cert-manager, Contour, external-dns, Kyverno) from charts bundled inside the operator. Use this on a fresh, dedicated cluster.
* **Inline mode** — you declare the equivalent pre-existing resources in your cluster (your ingress class, your wildcard TLS certificate secret, your policy engine), and the operator only validates and consumes them. Use this when the cluster already has its own ingress controller and certificate management — for example on OpenShift.

Choosing an installation method
-------------------------------

There are two ways to drive this machinery:

* **The `educates` CLI** — you describe your scenario in a single high-level config file and the CLI does everything: installs the chart, applies the custom resources, and waits until everything reports ready. This is the most convenient path and the right choice for most users. See [CLI-based installation](cli-based-installation).
* **Helm and kubectl directly** — you `helm install` the operator chart from the OCI registry and `kubectl apply` the custom resources yourself. This is the right choice for GitOps tooling (ArgoCD, Flux) or when you want full control over the resources. See [Helm-based installation](helm-based-installation).

Both methods install exactly the same artifacts; the CLI is a convenience wrapper, not a separate mechanism. You can start with the CLI and switch to GitOps later: `educates admin platform render` writes the chart values and the custom resources the CLI would apply, to standard output or to a file each, ready to commit to a Git repository.

Supported scenarios
-------------------

The CLI provides purpose-built configuration kinds for common scenarios:

* **Local kind cluster** (`EducatesLocalConfig`) — laptop environment; see the [quick start guide](quick-start-guide).
* **GKE** (`EducatesGKEConfig`) — Workload Identity, CloudDNS based ACME certificates and DNS records.
* **EKS** (`EducatesEKSConfig`) — IRSA, Route53 based ACME certificates and DNS records.
* **Bring-your-own cluster services** (`EducatesInlineConfig`) — existing ingress controller and wildcard certificate, including OpenShift.
* **Full control** (`EducatesConfig`) — an escape hatch carrying the raw custom resource specs verbatim, with no CLI defaulting.

See [infrastructure providers](infrastructure-providers) for scenario-specific requirements and [configuration settings](configuration-settings) for the full configuration reference.

If you are trying Educates for the first time it is recommended not to use an existing Kubernetes cluster, but to use the Educates CLI to create a local Educates environment, including a Kubernetes cluster, for you:

* [Quick Start Guide](quick-start-guide) - Quick start guide for installing Educates and deploying a workshop.
* [Local Environment](local-environment) - More detailed guide for installing a local Educates environment.
