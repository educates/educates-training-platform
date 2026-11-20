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

When no ``spec.credentials`` section is provided, the lookup service will monitor the local cluster where it is deployed, accessing it as the ``remote-access`` service account in the ``educates`` namespace, whose token is mounted into the lookup service pod. See [Cluster Monitoring](cluster-monitoring) for the access this account has. A local cluster can still include ``spec.labels`` for use with tenant label selectors:

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

* ``spec.labels`` - An optional list of label objects for categorizing the cluster. Each label has a ``name`` field (required) and a ``value`` field (optional). These labels are used by tenant configurations to select which clusters are accessible through a given tenant. A label without a value is matched as a label whose value is the empty string.

* ``spec.credentials.kubeconfig.secretRef.name`` - The name of a Kubernetes secret in the ``educates-config`` namespace containing the kubeconfig for accessing the remote cluster. Required for remote clusters.

* ``spec.credentials.kubeconfig.secretRef.key`` - The key within the secret that contains the kubeconfig data. Defaults to ``config`` if not specified.

* ``spec.credentials.kubeconfig.context`` - The name of the context to use from the kubeconfig file. If not specified, the current context in the kubeconfig will be used, or the first context when the kubeconfig names no current context. A context which the kubeconfig does not hold is an error: the cluster is not connected, and the lookup service logs the reason. Only the context used is checked, so the kubeconfig can hold other contexts which the lookup service could not use. See [Cluster Monitoring](cluster-monitoring) for what the context must provide.

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

* ``spec.clusters.nameSelector.matchNames`` - A list of cluster names to include. A cluster matches when its name matches any of them. Names can contain wildcard patterns, where ``*`` matches any run of characters, ``?`` any single character, and ``[...]`` any one of the characters in the brackets (e.g., ``cluster-*``).

* ``spec.clusters.labelSelector.matchLabels`` - A map of key-value pairs. A cluster must have all specified labels, with the specified values, to match.

* ``spec.clusters.labelSelector.matchExpressions`` - A list of label match expressions for more advanced selection. Each expression has a ``key``, an ``operator`` (one of ``In``, ``NotIn``, ``Exists``, ``DoesNotExist``), and for the ``In`` and ``NotIn`` operators, a ``values`` list. A cluster must meet every expression to match.

**Portal selectors:**

* ``spec.portals.nameSelector.matchNames`` - A list of portal names to include, matched as for cluster names.

* ``spec.portals.labelSelector.matchLabels`` - A map of key-value pairs. A portal must have all specified labels, with the specified values, to match.

* ``spec.portals.labelSelector.matchExpressions`` - A list of label match expressions with the same structure as for cluster selectors.

When a selector has both a ``nameSelector`` and a ``labelSelector``, a cluster or portal must match both. A selector, or a part of one, which is absent or empty matches everything. A ``TenantConfig`` with an empty ``spec``, or with neither ``clusters`` nor ``portals``, therefore gives access to every portal on every cluster. If ``clusters`` is specified but ``portals`` is not, all portals on the matched clusters will be accessible. If ``portals`` is specified but ``clusters`` is not, the portal selector will apply across all clusters.

A label without a value is matched as a label whose value is the empty string, so ``Exists`` matches it, and so does a ``matchLabels`` entry giving the empty string as the value.

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

* ``spec.client.password`` - The password for the client. Must be at least 8 characters. This is used together with the resource name (``metadata.name``) as the username when authenticating via the REST API. Changing the password revokes every access token the client holds, as [Client Authentication](./client-authentication) describes.

* ``spec.roles`` - A list of roles assigned to the client. At least one role must be specified. Supported roles are ``admin`` and ``tenant``.

* ``spec.tenants`` - A list of tenant names the client is permitted to access. Wildcard patterns are supported, matched as for the names in a ``TenantConfig`` selector (e.g., ``customer-*``). Every client, whatever its roles, can request sessions only through the tenants this list matches. For a client with the ``tenant`` role, it also restricts which tenants it can list workshops for, even when the client has the ``admin`` role too. A client with only the ``admin`` role can list the workshops of any tenant, and is typically given ``["*"]`` so that it can request sessions through every tenant.

* ``spec.user`` - An optional user identifier for the client. When set, every session request of the client is made for this user, in place of the ``clientUserId`` the request gives. This suits a client which acts for a single user.

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

All configuration resources are created in the ``educates-config`` namespace, along with the secrets holding the kubeconfig files of remote clusters. The Educates install creates this namespace when it deploys the lookup service, so you do not need to create it yourself. The namespace is kept when the lookup service or Educates is uninstalled, so your configuration resources and secrets are still there when you install Educates again. The lookup service puts no finalizer on the configuration resources, so you can delete them, or the namespace itself, as usual at any time. If you created the ``educates-config`` namespace by hand, as earlier versions of Educates required, the install takes it over and keeps what it holds. A configuration resource kept from an earlier version of Educates can still hold the finalizer that version's lookup service put on it. The lookup service treats such a resource as gone as soon as you delete it, and uninstalling the lookup service removes the finalizer, which lets the deletion finish.

The lookup service reads the secrets holding kubeconfig files, and its configuration resources, only in the ``educates-config`` namespace. Resources in any other namespace are ignored.

Resources can be created, updated, and deleted using standard ``kubectl`` commands:

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

Changes to configuration resources, and to the secrets holding kubeconfig files, are picked up by the lookup service automatically, without a restart. A new or changed ``ClusterConfig``, or a changed kubeconfig secret, makes the lookup service connect to the cluster again, and a deleted ``ClusterConfig`` stops the lookup service from placing sessions on that cluster at once. You can monitor the lookup service logs to verify that configuration changes are being processed:

```
kubectl logs -n educates --follow deployment/lookup-service
```

The lookup service logs one JSON object per line. Set ``spec.logLevel`` of the ``LookupService`` resource to ``debug`` for more detail, such as a line for every REST API request, or to ``warn`` or ``error`` for less. It defaults to ``info``.
