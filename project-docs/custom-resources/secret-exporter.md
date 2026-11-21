Secret Exporter
===============

The ``SecretExporter`` custom resource can set up rules for copying a single secret from the same namespace to other namespaces, but where the recipient namespace must include a corresponding ``SecretImporter`` resource. Both ``SecretExporter`` and ``SecretImporter`` are namespaced resources and thus users of a cluster could be given selective ability via RBAC to use them in a namespace.

The raw custom resource definition for the ``SecretExporter`` custom resource can be viewed by running:

```shell
kubectl get crd/secretexporters.secrets.educates.dev -o yaml
```

Pairing of exporter and importer
--------------------------------

The ``SecretExporter`` custom resource instance must have the same name as the secret you want to export to other namespaces.

The ``SecretExporter`` must include a set of rules which stipulate which other namespaces the secret can be exported to. Each rule must select its target namespaces with at least one of ``nameSelector``, ``uidSelector``, ``ownerSelector`` and ``labelSelector`` under ``targetNamespaces``.

Although not necessarily recommended, a ``SecretExporter`` can declare a rule which denotes that a secret should be exported to all other namespaces.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      nameSelector:
        matchNames:
        - "*"
    copyAuthorization:
      sharedSecret: my-shared-secret
```

Secrets will never be copied into the same namespace they originated from.

No matter whether a single namespace or all is targeted, the secret will not be copied to the target namespace unless a ``SecretImporter`` custom resource instance of the same name as the target secret exists. Further, the ``SecretExporter`` and ``SecretImporter`` custom resources must include the same shared secret value to authorize that copying of the secret between the namespaces is allowed, and the ``sourceNamespaces`` of the ``SecretImporter``, when it has them, must admit the namespace of the ``SecretExporter``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretImporter
metadata:
  name: registry-credentials
  namespace: developer-namespace
spec:
  copyAuthorization:
    sharedSecret: my-shared-secret
```

The shared secret value is mandatory so that if a ``SecretExporter`` is created that targets namespaces it shouldn't, that someone else in control of one of the targeted namespaces can't steal the secret. For someone to be able to get a copy of the secret, they would need to know the shared secret value.

A copy is made within seconds of both resources and the secret existing, whichever of them is created last, and is updated within seconds of the secret changing. A secret in a namespace which is being deleted is not copied, so no copy is created or updated from it while its namespace is deleted.

Changing the target secret name
-------------------------------

To change the name of the secret when copied to the target namespace, set ``targetSecret.name``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      nameSelector:
        matchNames:
        - "*"
    copyAuthorization:
      sharedSecret: my-shared-secret
    targetSecret:
      name: copy-of-registry-credentials
```

The name of the ``SecretImporter`` must now match the new secret name.

The secret will still never be copied into the same namespace even though the name is different.

Adding labels to the target secret
----------------------------------

A copy carries the labels of the secret exported. To set further labels on the copies, set ``targetSecret.labels``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      nameSelector:
        matchNames:
        - "*"
    copyAuthorization:
      sharedSecret: my-shared-secret
    targetSecret:
      labels:
        registry-pull-secret: ""
```

Where ``targetSecret.labels`` and the secret exported give a label with the same key, the value from ``targetSecret.labels`` is used. A copy has these labels from the moment it is created.

A label which the secret exported drops, or which is removed from ``targetSecret.labels``, is removed from the copies. Labels which other tools, such as admission webhooks, add to a copy are left alone. A label which another tool also sets on a copy using server-side apply is shared with that tool, and stays on the copy when the rule no longer sets it.

Adding annotations to the target secret
---------------------------------------

The annotations of the secret exported are never copied. To set annotations on the copies, set ``targetSecret.annotations``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      nameSelector:
        matchNames:
        - "*"
    copyAuthorization:
      sharedSecret: my-shared-secret
    targetSecret:
      annotations:
        argocd.argoproj.io/compare-options: IgnoreExtraneous
```

An annotation which is removed from ``targetSecret.annotations`` is removed from the copies, and annotations which other tools add to a copy are left alone, as for labels.

Annotation keys starting with ``secrets.educates.dev/`` are reserved for the annotations the secrets manager uses to track copies, and a ``SecretExporter`` whose ``targetSecret.annotations`` use one is rejected when it is created or updated. A rule can give at most 32 annotations.

Selecting the keys copied
-------------------------

A copy holds every key of the secret exported. To copy only some of the keys, list them in ``targetSecret.keys``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: database-credentials
  namespace: databases
spec:
  rules:
  - targetNamespaces:
      nameSelector:
        matchNames:
        - "*"
    copyAuthorization:
      sharedSecret: my-shared-secret
    targetSecret:
      keys:
      - username
      - password
```

A listed key which the secret exported does not hold is not copied. An empty list copies every key, as when ``keys`` is not set.

The data of a copy always equals what the rule selects from the secret exported. A key which the secret exported drops, or which is no longer listed, is removed from the copies, and so is a key which anything else adds to a copy.

The type of a copy is that of the secret exported, and Kubernetes requires a secret of some types to hold certain keys. A copy of a ``kubernetes.io/tls`` secret must keep both ``tls.crt`` and ``tls.key``, so ``keys`` must list both for such a secret, beside any other key wanted, such as ``ca.crt``. Where the keys selected leave out a key the type requires, Kubernetes rejects the copy, and the ``SecretExporter`` reports the failure for each namespace the copy could not be written to.

Limiting set of target namespaces
---------------------------------

To limit the set of target namespaces based on name, set ``targetNamespaces.nameSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      nameSelector:
        matchNames:
        - example-1
        - example-2
    copyAuthorization:
      sharedSecret: my-shared-secret
```

The list of names can be the exact value, or you can also use a shell style glob expression to match a set of names.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      nameSelector:
        matchNames:
        - example-*
    copyAuthorization:
      sharedSecret: my-shared-secret
```

In a glob expression ``*`` matches any sequence of characters, ``?`` any single character, and ``[seq]`` any character in ``seq``. A name or glob expression with a leading ``!`` excludes the namespaces it matches. A namespace is selected when it matches one of the entries without a leading ``!``, or when every entry has one, and matches none of the entries with a leading ``!``. As YAML reads a value starting with ``!`` as a tag, such an entry must be quoted, as in ``- "!kube-*"``.

Alternatively, you can match on labels on the namespace by setting a ``targetNamespaces.labelSelector.matchLabels``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      labelSelector:
        matchLabels:
          developer-namespace: ""
    copyAuthorization:
      sharedSecret: my-shared-secret
```

Or if more flexible matching of labels is required, you can supply a list of expressions by setting ``targetNamespaces.labelSelector.matchExpressions``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      labelSelector:
        matchExpressions:
        - key: developer-namespace
          operator: In
          values:
          - ""
    copyAuthorization:
      sharedSecret: my-shared-secret
```

A ``labelSelector`` behaves as a Kubernetes label selector does. For an expression the list of operators is ``In``, ``NotIn``, ``Exists`` and ``DoesNotExist``. For ``In`` and ``NotIn`` a non empty list of values must be supplied, and ``Exists`` and ``DoesNotExist``, which only pertain to the existence of the label key, take no values. A rule whose ``labelSelector`` Kubernetes would not accept as a label selector, such as one with an ``In`` expression without values, an ``Exists`` expression with values, or a label key or value which is not valid, selects no namespace, and the ``SecretExporter`` reports ``InvalidSelector`` in its status.

When targeting a specific namespace and the namespace is deleted, then recreated, the rule will still match the new instance of the namespace if selectors match the new instance of the namespace, even though it may notionally be used for a different purpose and the secret shouldn't be copied there.

If you need to ensure that a secret is only copied to that specific instance of the namespace, and not a future version of the namespace created subsequent to its deletion, you can require the namespace have a specific ``uid`` by setting ``targetNamespaces.uidSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      uidSelector:
        matchUIDs:
        - 9b030c48-b773-4ffb-9d8f-7eacf7dbcfbe
    copyAuthorization:
      sharedSecret: my-shared-secret
```

The ``uid`` should be that set by Kubernetes in the ``metadata`` section of the namespace resource.

Alternatively, you can restrict whether the secret should be copied to a specific instance of a namespace based on whether that resource is owned by another. This can be done by providing the details of the owner by setting ``targetNamespaces.ownerSelector``, which a rule can also use on its own.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      ownerSelector:
        matchOwners:
        - apiVersion: training.educates.dev/v1beta1
          kind: WorkshopSession
          name: educates-cli-w01-s001
          uid: dcc4cf0f-6774-4eb4-bbf7-77a4ecaefa42
    copyAuthorization:
      sharedSecret: my-shared-secret
```

A namespace matches when one of its owner references has the ``apiVersion``, ``kind``, ``name`` and ``uid`` of one of the owners listed.

Only namespaces which are active are targeted, so a namespace which is being deleted receives no copy. A namespace created after the ``SecretExporter``, or whose labels or owners change so that a rule selects it, receives its copy within seconds, once it holds the ``SecretImporter``.

Kubernetes reserved namespaces
------------------------------

When a rule has no ``nameSelector``, or its ``matchNames`` is an empty list, the rule behaves as if it had been given:

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      nameSelector:
        matchNames:
        - "!kube-*"
    copyAuthorization:
      sharedSecret: my-shared-secret
```

That is, ``kube-public``, ``kube-system`` or other namespaces starting with ``kube-`` will be excluded, they being generally reserved by Kubernetes itself. The ``!`` before the names indicates the namespace will be excluded.

A rule which lists names in ``matchNames`` replaces this default, so a rule exporting to ``"*"`` also targets the Kubernetes reserved namespaces. Although a corresponding ``SecretImporter`` is always required for a ``SecretExporter``, as well as a shared secret value to authorize that a copy is allowed to be made, when setting up a rule to copy a secret to all namespaces, it is recommended to exclude Kubernetes reserved namespaces by listing ``"!kube-*"`` beside ``"*"``.

In general, rather than relying on a rule which could result in a secret being copied to all namespaces, it is better to be selective by explicitly naming the target namespaces, or use labels to select the target namespaces.

Requirement to authorize copying
--------------------------------

As previously noted, the requirement for a shared secret value to authorize copying is to avoid the chance that someone could steal a secret exported from a different namespace. That they would have knowledge of the shared secret value given in the ``SecretExporter`` implies they have some authority to copy the secret.

Rather than explicitly require a value to be supplied from ``copyAuthorization.sharedSecret`` in the ``SecretExporter``, it is also possible to still leave out that section.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretExporter
metadata:
  name: registry-credentials
  namespace: registry
spec:
  rules:
  - targetNamespaces:
      nameSelector:
        matchNames:
        - "*"
```

In this case, a shared secret value is still required in the corresponding ``SecretImporter``, but what should be used is the Kubernetes object ID of the ``SecretExporter`` resource.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretImporter
metadata:
  name: registry-credentials
  namespace: developer-namespace
spec:
  copyAuthorization:
    sharedSecret: bd380bd5-4ddc-403a-bdba-454bf21bd7b3
```

That is, the shared secret value would be the ``uid`` from the ``metadata`` section of the ``SecretExporter``, which can be read by running:

```shell
kubectl get secretexporter registry-credentials -n registry -o jsonpath='{.metadata.uid}'
```

A ``SecretExporter`` which is deleted and created again has a new ``uid``, so the ``SecretImporter`` resources then need the new value.

Multiple target namespace selectors
-----------------------------------

More than one type of selector can be listed under ``targetNamespaces``, in which case all selectors must match else the secret will not be copied to the namespace.

Multiple rules for exporting a secret
-------------------------------------

The ``rules`` property is a list, so rules for more than one set of target namespaces can be specified in the one custom resource. A ``SecretExporter`` can have at most 100 rules.

Overlapping rules for target secret
-----------------------------------

A copy is managed by the rule which created it, which records itself in the copy's tracking annotations, described below. A ``SecretExporter`` and a ``SecretCopier`` which copy the same secret under the same name into a namespace agree: the one which created the copy keeps it and decides what it holds, and the other counts the copy as in sync. A rule which would copy another secret under the name of the copy leaves the copy untouched and reports a ``Conflict``, as a rule does for a secret the secrets manager did not create.

The rules for overlapping ``SecretCopier`` and ``SecretExporter`` rules are described in full on the page for the ``SecretCopier`` custom resource.

Deletion of the target secret
-----------------------------

The secrets manager never deletes a copy because the secret exported is deleted, or because the rules defined in the ``SecretExporter`` change such that it wouldn't have been copied in the first place. Such a copy stays as it is, and is no longer updated.

When a secret is copied into a namespace due to use of paired ``SecretExporter`` and ``SecretImporter``, the target secret is owned by the ``SecretImporter`` custom resource instance, through an owner reference which marks it as the controller of the copy. If the ``SecretImporter`` custom resource instance is deleted, then the target secret will also be deleted.

Deleting the ``SecretExporter``, or changing the shared secret of the ``SecretImporter`` so that it no longer matches, leaves a copy already made in place, and the copy is no longer updated. A rule which copies the same secret under the same name takes such a copy over once the ``SecretExporter`` is deleted.

The secrets manager adds no finalizer to a ``SecretExporter``, so deleting a ``SecretExporter``, or its namespace, never waits for the secrets manager, even when it is not running or has been uninstalled.

Changes to a copy
-----------------

A copy which is deleted is created again within seconds. A copy whose data, or whose labels or annotations set by its rule, are changed by someone else, is restored within seconds.

When the secret exported is replaced by one of a different ``type``, its copies are deleted and created again with the new type, since Kubernetes does not let the type of a secret change. A copy which someone else has marked with ``immutable: true`` is never deleted: when its data or type must change, the rule reports a failure for that namespace, retried until someone deletes the copy, as Kubernetes never lets the data of an immutable secret change, and the rule then creates it again.

Tracking the source of a secret
-------------------------------

When secrets are copied, the copy of a secret will have the following annotation added so that it can be determined where the secret was copied from:

```
secrets.educates.dev/secret-name: namespace/name
```

The ``SecretExporter`` instance containing the rules which resulted in the secret being copied is recorded using the annotation:

```
secrets.educates.dev/copier-rule: secretexporter/name
```

The ``resourceVersion`` of the secret exported the copy was last written from is recorded using the annotation:

```
secrets.educates.dev/source-version: "123456"
```

A copy is written only when what it should hold changes, so a change to the secret exported which alters nothing the copy holds leaves the copy as it is, recording an earlier version.

Status of the secret exporter
-----------------------------

The status of a ``SecretExporter`` says whether every copy its rules make is in sync with the secret exported, and why not. Listing ``SecretExporter`` resources shows the ``Ready`` condition, its reason, and how many of the target namespaces selected hold a copy which is in sync:

```shell
kubectl get secretexporters -n registry
```

```
NAME                   READY   REASON                  IN SYNC   AGE
registry-credentials   False   AwaitingAuthorization   2/3       5m
```

The ``Ready`` condition is ``True``, with reason ``InSync``, when every target namespace each rule selects holds a copy in sync with the secret exported. Otherwise it is ``False``, with the reason of the first of these problems which applies:

* ``InvalidSelector``: a rule's ``targetNamespaces`` is not valid, so the rule selects no namespace.

* ``SourceMissing``: the secret exported does not exist, or its namespace is being deleted, so it is not copied.

* ``TargetErrors``: a copy could not be written in some namespaces, and is retried.

* ``Conflict``: in some namespaces a secret with the name of the copy exists which the secrets manager did not create, or which another rule copies from another secret, and it is left untouched.

* ``AwaitingAuthorization``: in some namespaces no ``SecretImporter`` accepts the copy, as none is named after the copy, or the one named after it holds another shared secret or does not admit the namespace of the ``SecretExporter``.

The message of the condition names up to five of the namespaces affected, and what failed where a copy could not be written. ``status.sourceExists`` says whether the secret exported exists. The ``status.summary`` counts, for all the rules together, the ``targetNamespaces`` selected and, of them, those whose copy is ``inSync``, those in ``conflicts``, those ``awaitingAuthorization`` and the ``failures``. Its ``inSyncRatio`` is the ``In Sync`` column. The ``status.rules`` list gives the same counts for each rule, in the order of ``spec.rules``.

A copy which cannot be written in one namespace never stops copies into the other namespaces. It is retried with an increasing delay, at most 5 minutes apart. Every ``SecretExporter`` is also checked again 5 minutes after it was last checked, which repairs any copy a missed change left out of sync.

Events of the secret exporter
-----------------------------

When a ``SecretExporter`` has not been ``Ready`` for a minute, the secrets manager records a ``Warning`` Event with reason ``NotReady`` in its namespace, giving the reason and message of the ``Ready`` condition. When a ``SecretExporter`` which got that Event is ``Ready`` again, it records a ``Normal`` Event with reason ``Recovered``. No Event is recorded for a ``SecretExporter`` which is ``Ready`` again within a minute, while its status shows the problem at once. Run ``kubectl describe secretexporter <name> -n <namespace>`` to see them.

Access to secret exporters
--------------------------

The shared secret of a ``SecretExporter`` is what authorizes the import of its secret, so the roles for viewing resources do not cover ``SecretExporter`` resources. The ``educates:rbac:view:secrets`` cluster role, and the built-in ``view`` cluster role into which it is aggregated, grant no access to them, so a user who can only view a namespace cannot read the shared secrets of its exporters. The ``educates:rbac:admin:secrets`` cluster role grants full access to them, and is aggregated into the built-in ``admin`` cluster role.
