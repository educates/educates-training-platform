Cluster Monitoring
==================

The lookup service monitors one or more Kubernetes clusters for training portals and their workshop environments. This page covers how to configure monitoring for both the local cluster and remote clusters.

Local cluster monitoring
------------------------

To monitor the cluster where the lookup service is deployed, create a ``ClusterConfig`` resource without a ``spec.credentials`` section. The lookup service will then access the Kubernetes API as the ``remote-access`` service account in the ``educates`` namespace, using the token of that service account which is mounted into the lookup service pod. This is the same account a kubeconfig generated for a remote cluster uses, as described below, so the lookup service has the same access to the local cluster as it has to a remote one.

The token of the ``remote-access`` service account exists only while remote access is installed on the cluster. With ``spec.remoteAccess.mode`` at its default of ``Auto`` on the ``SessionManager`` resource, remote access is installed whenever a ``LookupService`` resource exists, so a cluster running the lookup service has it. With ``spec.remoteAccess.mode`` set to ``Disabled``, the lookup service still starts and monitors remote clusters, but a ``ClusterConfig`` without ``spec.credentials`` is not connected, and the training portals of the local cluster are not offered. The lookup service keeps checking for the token, and connects to the local cluster without a restart once remote access is installed again.

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

The context of the kubeconfig the lookup service uses, as ``spec.credentials.kubeconfig.context`` of the ``ClusterConfig`` selects it, must authenticate with a bearer token, and its cluster must give the address of the API server and the CA certificate to verify it with, as ``certificate-authority-data``. A context whose user authenticates with an exec plugin, an authentication provider or a client certificate is not supported: the kubeconfig is refused, and the cluster is not connected. Only that context is checked, so the kubeconfig may hold other contexts, whatever they use. The kubeconfig generated as described below meets these requirements.

The service account used in the kubeconfig should have limited permissions. Specifically, it needs ``list`` and ``watch`` access to the following Educates resource types:

* ``TrainingPortal``
* ``WorkshopEnvironment``
* ``WorkshopSession``

The lookup service also logs in to the REST API of each training portal it manages, using the robot credentials of that training portal. These are held in the secret ``training-portal-robot-credentials`` in the namespace of the training portal, ``<portal-name>-ui``, which the training portal's status names under ``status.educates.credentialSecrets.robot``. The lookup service reads that secret through the same kubeconfig, so the account it uses needs ``get`` access on the robot credentials secret in the namespace of every training portal.

The ``remote-access`` service account in the ``educates`` namespace, which is what the kubeconfig generated as described below uses, is given this access by Educates. For every training portal, whether or not the lookup service or remote access is enabled, the session manager creates a ``Role`` named ``educates-remote-access`` in the namespace of the training portal, granting ``get`` on the robot credentials secret by name and on no other secret, and a ``RoleBinding`` of the same name binding it to the ``remote-access`` service account. Turning on the lookup service for a cluster where training portals already exist therefore needs no training portal to be recreated. The account can never read the admin credentials of a training portal, which are held in the separate secret ``training-portal-admin-credentials``, so a kubeconfig for the lookup service cannot be used to log in to the admin pages of a training portal.

If the kubeconfig you give the lookup service uses an identity other than the ``remote-access`` service account, that identity needs to be granted ``get`` on the ``training-portal-robot-credentials`` secret in the namespace of each training portal, in addition to the access to the Educates resources listed above. Without it, the lookup service cannot obtain an access token for the training portals of that cluster and session requests for their workshops fail, with an error in the lookup service logs naming the secret it could not read. Grant ``get`` on that secret alone, by name, rather than on all secrets in the namespace, so that the identity cannot read the admin credentials secret.

It is strongly advised against using a kubeconfig with ``cluster-admin`` privileges for monitoring a remote cluster. The lookup service only needs read access to the Educates resources listed above and to the robot credentials secret of each training portal.

The lookup service calls the REST API of a training portal on a remote cluster at the public URL of the training portal, and that of a training portal on the local cluster through its service inside the cluster. It trusts certificates issued by a public certificate authority, and certificates which chain to the CA of the ingress of the Educates installation it runs in, when that installation has one, as [Secure HTTP connections](../installation-guides/secure-http-connections) describes. A remote training portal whose certificate is issued by another private CA cannot be reached. Every call to a training portal times out after 10 seconds.

Generating a kubeconfig
-----------------------

The Educates CLI provides a command to generate a kubeconfig with the appropriate RBAC permissions for the lookup service:

```
educates admin lookup kubeconfig --context cluster-1 --output kubeconfig-cluster-1.yaml
```

The generated kubeconfig accesses the remote cluster as the ``remote-access`` service account in the ``educates`` namespace, using the token Educates creates for that account when remote access is installed on the remote cluster. The ``--context`` option specifies which Kubernetes context to use when connecting to the remote cluster to read that token. The ``--output`` option specifies the file to write the generated kubeconfig to.

You can verify the generated kubeconfig has the correct permissions by testing it:

```
kubectl get trainingportals --kubeconfig kubeconfig-cluster-1.yaml
```

The kubeconfig should allow listing training portals but should not grant broader access. You can verify this by confirming that listing namespaces is denied:

```
kubectl get namespaces --kubeconfig kubeconfig-cluster-1.yaml
```

This should return a forbidden error, confirming that the kubeconfig has appropriately scoped permissions.

For a training portal on the cluster, reading its robot credentials secret should succeed, and reading its admin credentials secret should return a forbidden error:

```
kubectl get secret training-portal-robot-credentials -n <portal-name>-ui --kubeconfig kubeconfig-cluster-1.yaml
kubectl get secret training-portal-admin-credentials -n <portal-name>-ui --kubeconfig kubeconfig-cluster-1.yaml
```

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

A label without a value is matched by tenant selectors as a label whose value is the empty string.

Changing and removing clusters
------------------------------

Changes take effect without restarting the lookup service. When a ``ClusterConfig`` is created or changed, or the secret holding its kubeconfig is changed, as when the credentials of a cluster are rotated, the lookup service connects to the cluster again with the new configuration. When a ``ClusterConfig`` is deleted, the lookup service stops placing sessions on that cluster at once.

Clusters which cannot be reached
--------------------------------

A cluster the lookup service cannot reach, or whose resources it cannot list, contributes no training portals, and requests for the training portals of the other clusters are not delayed by it. When the first listing of a cluster's resources does not complete within 30 seconds, the lookup service gives up on that attempt and tries again 10 seconds later, and keeps trying until it succeeds. The lookup service reports itself ready once it has loaded its own configuration, without waiting for any cluster.

Verifying cluster registration
------------------------------

After creating ``ClusterConfig`` resources, you can verify they have been registered by querying the lookup service REST API using an admin client:

```
curl --silent -X GET -H "Authorization: Bearer ${ACCESS_TOKEN}" \
  https://lookup.<ingress-domain>/api/v1/clusters | jq
```

This will return a list of all registered clusters with their names and labels. A cluster whose kubeconfig the lookup service refused is not listed. You can also check the lookup service logs, in which each cluster is named by the ``cluster`` field of a log line:

```
kubectl logs -n educates --follow deployment/lookup-service
```

The message ``The cluster's cache has synced`` shows the lookup service has connected to a cluster and listed its resources. The message ``The cluster is not connected`` gives the reason it could not connect to a cluster, such as a refused kubeconfig, a context the kubeconfig does not hold, or a missing remote access token for the local cluster.
