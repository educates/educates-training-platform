Cluster Monitoring
==================

The lookup service monitors one or more Kubernetes clusters for training portals and their workshop environments. This page covers how to configure monitoring for both the local cluster and remote clusters.

Local cluster monitoring
------------------------

To monitor the cluster where the lookup service is deployed, create a ``ClusterConfig`` resource without a ``spec.credentials`` section. The lookup service will then use the service account credentials available to its own pod to access the Kubernetes API.

In the simplest case the ``spec`` section can be omitted entirely:

```yaml
apiVersion: lookup.educates.dev/v1beta1
kind: ClusterConfig
metadata:
  name: local-cluster
  namespace: educates-config
```

If you need to assign labels to the local cluster for use with tenant label selectors, you can include ``spec.labels`` without providing ``spec.credentials``:

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

Apply this to the cluster:

```
kubectl apply -f cluster-config.yaml
```

The name given to the cluster (``local-cluster`` in this example) is used to identify the cluster in tenant configurations and in REST API responses. You can choose any name that is meaningful to you.

Remote cluster access
---------------------

To monitor a remote cluster, the lookup service needs a kubeconfig file that provides access to the Educates resources on that cluster. The kubeconfig must be stored in a Kubernetes secret in the ``educates-config`` namespace of the cluster where the lookup service is running.

The service account used in the kubeconfig should have limited permissions. Specifically, it needs ``get``, ``list``, and ``watch`` access to the following Educates resource types:

* ``TrainingPortal``
* ``WorkshopEnvironment``
* ``WorkshopSession``
* ``WorkshopAllocation``
* ``Workshop``

It is strongly advised against using a kubeconfig with ``cluster-admin`` privileges for monitoring a remote cluster. The lookup service only needs read access to the Educates resources listed above.

Generating a kubeconfig
-----------------------

The Educates CLI provides a command to generate a kubeconfig with the appropriate RBAC permissions for the lookup service:

```
educates admin lookup kubeconfig --context cluster-1 --output kubeconfig-cluster-1.yaml
```

The ``--context`` option specifies which Kubernetes context to use when connecting to the remote cluster to create the service account and RBAC resources. The ``--output`` option specifies the file to write the generated kubeconfig to.

You can verify the generated kubeconfig has the correct permissions by testing it:

```
kubectl get trainingportals --kubeconfig kubeconfig-cluster-1.yaml
```

The kubeconfig should allow listing training portals but should not grant broader access. You can verify this by confirming that listing namespaces is denied:

```
kubectl get namespaces --kubeconfig kubeconfig-cluster-1.yaml
```

This should return a forbidden error, confirming that the kubeconfig has appropriately scoped permissions.

Creating kubeconfig secrets
---------------------------

Once you have a kubeconfig file for a remote cluster, store it in a Kubernetes secret in the ``educates-config`` namespace of the cluster where the lookup service is running:

```
kubectl create secret generic kubeconfig-cluster-1 -n educates-config --from-file config=kubeconfig-cluster-1.yaml
```

The key used for the kubeconfig data in the secret defaults to ``config``. If you use a different key, you will need to specify it in the ``ClusterConfig`` resource using ``spec.credentials.kubeconfig.secretRef.key``.

If you are running the lookup service on a separate hub cluster and monitoring multiple remote clusters, you will need to create a secret for each remote cluster:

```
kubectl create secret generic kubeconfig-cluster-1 -n educates-config --from-file config=kubeconfig-cluster-1.yaml
kubectl create secret generic kubeconfig-cluster-2 -n educates-config --from-file config=kubeconfig-cluster-2.yaml
```

Configuring remote clusters
---------------------------

With the kubeconfig secrets in place, create a ``ClusterConfig`` resource for each remote cluster, referencing the corresponding secret:

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
---
apiVersion: lookup.educates.dev/v1beta1
kind: ClusterConfig
metadata:
  name: remote-cluster-2
  namespace: educates-config
spec:
  credentials:
    kubeconfig:
      secretRef:
        name: kubeconfig-cluster-2
  labels:
    - name: customer
      value: customer-1
    - name: environment
      value: staging
```

Apply the configuration:

```
kubectl apply -f cluster-configs.yaml
```

Cluster labels
--------------

Labels on a ``ClusterConfig`` resource are used by tenant configurations to select which clusters are accessible through a given tenant. Labels are defined as a list of name/value pairs in the ``spec.labels`` field.

Common labeling strategies include:

* **By customer** - Use a ``customer`` label to group clusters belonging to the same organization.
* **By environment** - Use an ``environment`` label to distinguish production, staging, and development clusters.
* **By region** - Use a ``region`` label to identify the geographic location of a cluster.

Labels can be updated on an existing ``ClusterConfig`` without needing to recreate the resource. This provides flexibility to reclassify clusters without disrupting monitoring.

Verifying cluster registration
------------------------------

After creating ``ClusterConfig`` resources, you can verify they have been registered by querying the lookup service REST API using an admin client:

```
curl --silent -X GET -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  http://educates-api.<ingress-domain>/api/v1/clusters | jq
```

This will return a list of all registered clusters with their names and labels. You can also check the lookup service logs to confirm that the cluster operator threads have started for each configured cluster:

```
kubectl logs -n educates --follow deployment/lookup-service
```
