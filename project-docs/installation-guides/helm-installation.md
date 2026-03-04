(helm-installation)=
Helm Installation
=================

You can install the Educates training platform (Educates package only) using the provided Helm chart. This installs the session manager, secrets manager, CRDs for the Educates operators, and related configuration—without the extra components (cert-manager, contour, external-dns, kyverno, etc.) that the CLI or Carvel installer may add for a given infrastructure provider.

Prerequisites
-------------

* A Kubernetes cluster (1.25 or later) with an ingress controller already installed and configured.
* ``kubectl`` configured to use that cluster.
* Helm 3 installed.

The Helm chart does **not** install cert-manager, an ingress controller, or Kyverno. You must provide TLS and ingress according to your environment. For cluster and workshop security policy enforcement (e.g. Kyverno), see [Defining cluster policy engine](configuration-settings#defining-cluster-policy-engine) and configure ``clusterSecurity.policyEngine`` and, if using Kyverno, ``clusterSecurity.kyvernoPolicies``.

Custom Resource Definitions
----------------------------

The Helm chart includes **all** Custom Resource Definitions (CRDs) required for Educates to function:

* **Secrets Manager CRDs**: SecretCopier, SecretInjector, SecretImporter, SecretExporter
* **Training Platform CRDs**: TrainingPortal, Workshop, WorkshopSession, WorkshopEnvironment, WorkshopAllocation, WorkshopRequest
* **Lookup Service CRDs** (when enabled): ClusterConfig, ClientConfig, TenantConfig

These CRDs are installed automatically when you install the Helm chart—no separate ytt or kubectl apply step is needed. The chart uses the ``educates.dev`` API group and ``educates`` namespace and resource name prefix.

Installing the chart
--------------------

1. Add or use the chart from the repository root. The chart lives in the top-level ``chart/educates`` directory.

2. Create a values file (e.g. ``my-values.yaml``) with at least the required settings. Minimal example for a cluster where you control the ingress domain:

   .. code-block:: yaml

     clusterIngress:
       domain: workshops.example.com
       # If TLS is in a secret in another namespace:
       # tlsCertificateRef:
       #   namespace: default
       #   name: workshops.example.com-tls

3. Install the release:

   .. code-block:: bash

     helm upgrade --install educates ./chart/educates \
       --namespace educates \
       --create-namespace \
       -f my-values.yaml

   The chart creates and uses the ``educates`` namespace. Installing with ``--namespace educates`` ensures the release is tracked in that namespace; the chart will still create the namespace if it does not exist.

4. Verify the session manager and secrets manager are running:

   .. code-block:: bash

     kubectl get pods -n educates
     kubectl get trainingportals -A   # after training CRDs are installed

Training CRDs
-------------

The Helm chart automatically installs all 6 training-related Custom Resource Definitions:

* **Workshop**: Defines workshop content, instructions, and environment requirements
* **WorkshopSession**: Represents an individual user's workshop session
* **WorkshopAllocation**: Allocates workshop sessions to specific users
* **WorkshopEnvironment**: Defines the environment configuration for running workshops
* **WorkshopRequest**: Manages user requests for workshop access
* **TrainingPortal**: Provides a web portal for accessing and managing workshops

**Important notes about CRDs:**

* CRDs are installed automatically when you run ``helm install`` or ``helm upgrade``—no separate installation step is required
* CRDs are **not** removed by ``helm uninstall`` by default (this is standard Helm behavior to prevent accidental data loss)
* To remove CRDs after uninstalling, you must delete them manually:

  .. code-block:: bash

    kubectl delete crd workshops.training.educates.dev
    kubectl delete crd workshopsessions.training.educates.dev
    kubectl delete crd workshopallocations.training.educates.dev
    kubectl delete crd workshopenvironments.training.educates.dev
    kubectl delete crd workshoprequests.training.educates.dev
    kubectl delete crd trainingportals.training.educates.dev

* **Warning**: Deleting CRDs will cascade delete all custom resources of those types, removing all your workshops, training portals, and active sessions. Always backup or export important resources before deleting CRDs.

Secret Management
-----------------

The Helm chart automatically configures **SecretCopier** and **SecretInjector** resources to manage secrets across namespaces. This eliminates the need to manually copy secrets into the Educates namespace or workshop environments.

**Automatic Secret Copying**

When you reference secrets from external namespaces in your configuration, the chart creates SecretCopier resources to copy them into the Educates operator namespace. Supported references include:

* **TLS certificates** (``clusterIngress.tlsCertificateRef``):

  .. code-block:: yaml

    clusterIngress:
      domain: workshops.example.com
      tlsCertificateRef:
        namespace: cert-manager
        name: workshops-tls

  The chart creates a SecretCopier to copy ``workshops-tls`` from the ``cert-manager`` namespace into the ``educates`` namespace.

* **Image pull secrets** (``clusterSecrets.pullSecretRefs``):

  .. code-block:: yaml

    clusterSecrets:
      pullSecretRefs:
      - namespace: default
        name: private-registry-creds
      - namespace: ci-system
        name: harbor-robot-token

  Each secret is copied into the Educates namespace and automatically injected into workshop environments and sessions.

* **Theme data references** (``websiteStyling.themeDataRefs``):

  .. code-block:: yaml

    websiteStyling:
      themeDataRefs:
      - namespace: branding
        name: custom-portal-theme

  Theme data secrets are copied and made available to training portals for custom styling.

**Automatic Secret Injection**

The chart also creates **SecretInjector** resources to ensure copied secrets are automatically injected into:

* The training portal namespace
* Workshop environment namespaces
* Individual workshop session namespaces

This ensures that image pull secrets, TLS certificates, and other credentials are available wherever needed without manual intervention. Secrets are kept in sync—if you update the source secret, the SecretCopier will detect the change and update the copy.

Kyverno Policy Management
--------------------------

When Kyverno is configured as the policy engine, the Helm chart embeds and deploys **16 Kyverno policies** that enforce Pod Security Standards for workshop environments.

**Enabling Kyverno Policies**

Set the policy engine to Kyverno in your values:

.. code-block:: yaml

  clusterSecurity:
    policyEngine: kyverno

The chart will create 16 ClusterPolicy resources that enforce security controls on workshop namespaces.

**Policy Categories**

The embedded policies are divided into two categories:

* **11 Baseline Policies**: Implement the Kubernetes Pod Security Standards "baseline" profile, including:

  * Disallow privileged containers
  * Disallow host namespaces (hostNetwork, hostPID, hostIPC)
  * Disallow host paths
  * Disallow hostPorts
  * Disallow capabilities beyond the default set
  * Disallow SELinux modifications
  * Restrict sysctls
  * Restrict /proc mount types
  * Disallow privilege escalation
  * Require non-root user (optional)
  * Restrict seccomp profiles

* **5 Restricted Policies**: Implement the stricter "restricted" profile, adding:

  * Require runAsNonRoot
  * Restrict volume types
  * Require dropping ALL capabilities
  * Restrict seccomp to RuntimeDefault or Localhost
  * Disallow privilege escalation (stricter enforcement)

**Namespace Targeting**

All policies use a namespace selector to only apply to workshop-related namespaces:

.. code-block:: yaml

  namespaceSelector:
    matchLabels:
      training.educates.dev/policy.engine: kyverno

Workshop environment and session namespaces are automatically labeled with this label when created by the session manager.

**Policy Implementation**

* Policies use **CEL (Common Expression Language)** expressions for validation
* Minimum Kyverno version required: **1.11.0+** (for CEL support)
* Policies run in ``Enforce`` mode by default (can be configured via ``clusterSecurity.kyvernoPolicies.mode``)
* Policy violations will prevent non-compliant pods from being created

**Customization**

You can customize policy behavior via the ``clusterSecurity.kyvernoPolicies`` values:

.. code-block:: yaml

  clusterSecurity:
    policyEngine: kyverno
    kyvernoPolicies:
      mode: Enforce  # or Audit for monitoring without blocking
      baseline: true
      restricted: false

Lookup Service
--------------

The Helm chart can optionally deploy the **Educates Lookup Service**, an aggregation API server that provides centralized discovery and management of workshops across multiple clusters or tenants.

**What the Lookup Service Does**

The lookup service provides:

* A unified API for discovering workshops across multiple Educates installations
* Tenant-based workshop catalogs for multi-tenant deployments
* Cluster configuration management
* Client configuration for integrating external tools

**Enabling the Lookup Service**

To enable the lookup service, set ``lookupService.enabled`` to ``true``:

.. code-block:: yaml

  lookupService:
    enabled: true
    ingressPrefix: educates-lookup

This creates:

* A Deployment running the lookup service aggregation API server
* A Service exposing the API
* An Ingress (if configured) at ``{ingressPrefix}.{clusterIngress.domain}``
* Three additional CRDs: **ClusterConfig**, **ClientConfig**, and **TenantConfig**

**Ingress Configuration**

The lookup service requires an ingress prefix for external access:

.. code-block:: yaml

  lookupService:
    enabled: true
    ingressPrefix: educates-lookup  # Creates https://educates-lookup.workshops.example.com

  clusterIngress:
    domain: workshops.example.com
    tlsCertificateRef:
      namespace: cert-manager
      name: workshops-tls

If you configure ``clusterIngress.caCertificateRef``, the chart automatically injects the CA certificate into the lookup service for mutual TLS support.

**Lookup Service CRDs**

When enabled, the lookup service installs three additional CRDs:

* **ClusterConfig**: Defines configuration for a cluster registered with the lookup service, including API endpoints, credentials, and workshop catalog settings
* **ClientConfig**: Defines configuration for external clients that consume the lookup service API, including authentication and authorization settings
* **TenantConfig**: Defines configuration for multi-tenant deployments, mapping tenants to specific workshop catalogs and access controls

These CRDs are used to configure the lookup service's behavior and are managed independently of the core training platform CRDs.

Upgrading and configuration
---------------------------

* To change configuration, update your values file and run:

  .. code-block:: bash

    helm upgrade educates ./chart/educates -n educates -f my-values.yaml

* The chart encodes the full Educates operator configuration in a secret (``educates-config`` by default). Changing values and upgrading will update that secret; the session manager and secrets manager load configuration at startup.

* All options from the main [Configuration Settings](configuration-settings) document apply. Use the same structure under the chart’s ``values.yaml`` (e.g. ``clusterIngress``, ``clusterSecurity``, ``trainingPortal``, ``sessionManager``, ``imagePuller``, ``imageVersions``, etc.).

Uninstall
---------

To remove the release:

.. code-block:: bash

  helm uninstall educates -n educates

Note: Custom Resource Definitions (CRDs) installed by the chart are **not** removed by Helm by default. To remove them you would need to delete the CRD resources manually. Deleting CRDs will also remove all custom resources of that type (e.g. all TrainingPortals, Workshops). Plan accordingly.

Relationship to CLI and Carvel installs
---------------------------------------

* **CLI / Carvel**: Install Educates plus optional infrastructure (cert-manager, contour, external-dns, kyverno, etc.) using package configuration and ytt overlays. Good for first-time installs and GitOps.

* **Helm chart**: Installs the complete Educates platform (operators, RBAC, all CRDs, configuration, secrets management, embedded Kyverno policies, and optional lookup service). The Helm chart now has **feature parity** with the ytt package for all core Educates functionality. You manage infrastructure components (cert-manager, ingress controllers, external-dns, kyverno installation, etc.) separately. Useful when you already use Helm and want maximum configurability via ``values.yaml`` without Carvel.

**What's included in both:**

* All training and secrets management CRDs
* Session manager and secrets manager operators
* Automatic secret copying and injection
* Embedded Kyverno policies (when policy engine is set to Kyverno)
* Lookup service (when enabled)
* Full configuration support for all Educates features

**What's only in CLI/Carvel:**

* Automated installation of infrastructure components (cert-manager, Contour, external-dns, Kyverno itself, etc.)
* Infrastructure-specific configuration and integrations

You can use the same configuration layout (domain, TLS refs, credentials, storage, security policies, etc.) in both the package configuration file and the Helm chart values.
