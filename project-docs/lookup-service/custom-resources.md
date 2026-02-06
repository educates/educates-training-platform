Custom Resources
================

The lookup service uses three custom resource definitions (CRDs) for its configuration. All three belong to the ``lookup.educates.dev`` API group with version ``v1beta1``, and all are namespaced resources that should be created in the ``educates-config`` namespace.

ClusterConfig
-------------

A ``ClusterConfig`` resource registers a Kubernetes cluster for monitoring by the lookup service. The lookup service will watch the cluster for training portals, workshop environments, and workshop sessions.

```yaml
apiVersion: lookup.educates.dev/v1beta1
kind: ClusterConfig
metadata:
  name: local-cluster
  namespace: educates-config
```

When no ``spec.credentials`` section is provided, the lookup service will monitor the local cluster where it is deployed, using the service account credentials available to the lookup service pod. A local cluster can still include ``spec.labels`` for use with tenant label selectors:

```yaml
apiVersion: lookup.educates.dev/v1beta1
kind: ClusterConfig
metadata:
  name: local-cluster
  namespace: educates-config
spec:
  labels:
    - name: environment
      value: production
```

For remote clusters, the ``spec.credentials`` section provides the kubeconfig needed to access the remote cluster:

```yaml
apiVersion: lookup.educates.dev/v1beta1
kind: ClusterConfig
metadata:
  name: remote-cluster-1
  namespace: educates-config
spec:
  credentials:
    kubeconfig:
      secretRef:
        name: kubeconfig-cluster-1
  labels:
    - name: customer
      value: customer-1
    - name: environment
      value: production
```

The full set of fields in the ``spec`` is:

* ``spec.labels`` - An optional list of label objects for categorizing the cluster. Each label has a ``name`` field (required) and a ``value`` field (optional). These labels are used by tenant configurations to select which clusters are accessible through a given tenant.

* ``spec.credentials.kubeconfig.secretRef.name`` - The name of a Kubernetes secret in the ``educates-config`` namespace containing the kubeconfig for accessing the remote cluster. Required for remote clusters.

* ``spec.credentials.kubeconfig.secretRef.key`` - The key within the secret that contains the kubeconfig data. Defaults to ``config`` if not specified.

* ``spec.credentials.kubeconfig.context`` - The name of the context to use from the kubeconfig file. If not specified, the current context in the kubeconfig will be used.

TenantConfig
------------

A ``TenantConfig`` resource defines a logical tenant and the rules for determining which clusters and training portals are accessible through that tenant.

```yaml
apiVersion: lookup.educates.dev/v1beta1
kind: TenantConfig
metadata:
  name: tenant-1
  namespace: educates-config
spec:
  clusters:
    nameSelector:
      matchNames:
        - local-cluster
  portals:
    nameSelector:
      matchNames:
        - portal-1
```

The ``spec`` section contains two optional top-level fields, ``clusters`` and ``portals``, each of which can use name-based or label-based selectors to determine which resources are included in the tenant.

**Cluster selectors:**

* ``spec.clusters.nameSelector.matchNames`` - A list of cluster names to include. Names can contain wildcard patterns (e.g., ``cluster-*``).

* ``spec.clusters.labelSelector.matchLabels`` - A map of key-value pairs. A cluster must have all specified labels to match.

* ``spec.clusters.labelSelector.matchExpressions`` - A list of label match expressions for more advanced selection. Each expression has a ``key``, an ``operator`` (one of ``In``, ``NotIn``, ``Exists``, ``DoesNotExist``), and for the ``In`` and ``NotIn`` operators, a ``values`` list.

**Portal selectors:**

* ``spec.portals.nameSelector.matchNames`` - A list of portal names to include. Names can contain wildcard patterns.

* ``spec.portals.labelSelector.matchLabels`` - A map of key-value pairs. A portal must have all specified labels to match.

* ``spec.portals.labelSelector.matchExpressions`` - A list of label match expressions with the same structure as for cluster selectors.

If neither ``clusters`` nor ``portals`` is specified, the tenant will have no access to any resources. If ``clusters`` is specified but ``portals`` is not, all portals on the matched clusters will be accessible. If ``portals`` is specified but ``clusters`` is not, the portal selector will apply across all clusters.

The following example uses label selectors to match clusters and portals for a production environment:

```yaml
apiVersion: lookup.educates.dev/v1beta1
kind: TenantConfig
metadata:
  name: customer-1-production
  namespace: educates-config
spec:
  clusters:
    labelSelector:
      matchLabels:
        customer: customer-1
  portals:
    labelSelector:
      matchLabels:
        environment: production
```

Note that the labels used by the lookup service for matching are Educates-specific labels defined in the ``spec`` of the custom resources, not standard Kubernetes metadata labels. For training portals, labels are defined in the ``spec.portal.labels`` section of the ``TrainingPortal`` resource and can be set when creating a portal using the ``-l`` flag with the Educates CLI:

```
educates create-portal -p portal-1 -l environment=production
```

ClientConfig
------------

A ``ClientConfig`` resource defines a client that can authenticate against the lookup service REST API.

```yaml
apiVersion: lookup.educates.dev/v1beta1
kind: ClientConfig
metadata:
  name: custom-portal
  namespace: educates-config
spec:
  client:
    password: my-secret
  roles:
    - tenant
  tenants:
    - tenant-1
```

The full set of fields in the ``spec`` is:

* ``spec.client.password`` - The password for the client. Must be at least 8 characters. This is used together with the resource name (``metadata.name``) as the username when authenticating via the REST API.

* ``spec.roles`` - A list of roles assigned to the client. At least one role must be specified. Supported roles are ``admin`` and ``tenant``.

* ``spec.tenants`` - A list of tenant names the client is permitted to access. For clients with the ``tenant`` role, this restricts which tenants they can query workshops for and request sessions against. Wildcard patterns are supported (e.g., ``customer-*``). For clients with the ``admin`` role, this can be set to ``["*"]`` to grant access to all tenants.

* ``spec.user`` - An optional user identifier to associate with the client.

An admin client typically uses a wildcard for tenant access:

```yaml
apiVersion: lookup.educates.dev/v1beta1
kind: ClientConfig
metadata:
  name: admin
  namespace: educates-config
spec:
  client:
    password: super-secret
  roles:
    - admin
  tenants:
    - "*"
```

Managing resources
------------------

All configuration resources are created in the ``educates-config`` namespace. If this namespace does not already exist, create it before applying any configuration:

```
kubectl create ns educates-config
```

Resources can then be created, updated, and deleted using standard ``kubectl`` commands:

```
kubectl apply -f cluster-config.yaml
kubectl apply -f tenant-config.yaml
kubectl apply -f client-config.yaml
```

To list the current configuration resources:

```
kubectl get clusterconfigs -n educates-config
kubectl get tenantconfigs -n educates-config
kubectl get clientconfigs -n educates-config
```

Changes to configuration resources are picked up by the lookup service automatically. You can monitor the lookup service logs to verify that configuration changes are being processed:

```
kubectl logs -n educates --follow deployment/lookup-service
```
