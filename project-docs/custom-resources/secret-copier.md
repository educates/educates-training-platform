Secret Copier
=============

The ``SecretCopier`` custom resource sets rules for the copying of secrets between namespaces. The ``SecretCopier`` is a cluster scoped resource.

The raw custom resource definition for the ``SecretCopier`` custom resource can be viewed by running:

```shell
kubectl get crd/secretcopiers.secrets.educates.dev -o yaml
```

Specifying the source secret
----------------------------

To copy a secret from the specified namespace into all other namespaces use the following.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
```

Secrets will never be copied into the same namespace they originated from.

A copy is created within seconds of the ``SecretCopier`` being created, and is updated within seconds of its source secret changing. The source secret need not exist when the ``SecretCopier`` is created: it is copied as soon as it is created. A source secret in a namespace which is being deleted is not copied, so no copy is created or updated from it while its namespace is deleted.

Changing the target secret name
-------------------------------

To change the name of the secret when copied to the target namespace, set ``targetSecret.name``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetSecret:
      name: copy-of-registry-credentials
```

The secret will still never be copied into the same namespace even though the name is different.

Adding labels to the target secret
----------------------------------

A copy carries the labels of its source secret. To set further labels on the copies, set ``targetSecret.labels``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetSecret:
      labels:
        registry-pull-secret: ""
```

Where ``targetSecret.labels`` and the source secret give a label with the same key, the value from ``targetSecret.labels`` is used. A copy has these labels from the moment it is created.

A label which the source secret drops, or which is removed from ``targetSecret.labels``, is removed from the copies. Labels which other tools, such as admission webhooks, add to a copy are left alone. A label which another tool also sets on a copy using server-side apply is shared with that tool, and stays on the copy when the rule no longer sets it.

Adding annotations to the target secret
---------------------------------------

The annotations of the source secret are never copied. To set annotations on the copies, for example to mark them for a tool such as Argo CD, kapp or a backup tool, set ``targetSecret.annotations``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetSecret:
      annotations:
        argocd.argoproj.io/compare-options: IgnoreExtraneous
```

An annotation which is removed from ``targetSecret.annotations`` is removed from the copies, and annotations which other tools add to a copy are left alone, as for labels.

Annotation keys starting with ``secrets.educates.dev/`` are reserved for the annotations the secrets manager uses to track copies, and a ``SecretCopier`` whose ``targetSecret.annotations`` use one is rejected when it is created or updated. A rule can give at most 32 annotations.

Selecting the keys copied
-------------------------

A copy holds every key of its source secret. To copy only some of the keys, list them in ``sourceSecret.keys``, beside the name and namespace of the source secret.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: database-credentials
spec:
  rules:
  - sourceSecret:
      name: database-credentials
      namespace: databases
      keys:
      - username
      - password
```

A listed key which the source secret does not hold is not copied, and the ``SecretCopier`` reports it: its ``Ready`` condition is ``False``, with reason ``SourceKeysMissing``, and its message names the rule, the source secret and the keys it lacks. The copies still hold the listed keys which the source secret has, and count as in sync, and once the source secret holds every listed key, the ``SecretCopier`` is ``Ready`` again. When ``keys`` is set it must list at least one key, and a ``SecretCopier`` with an empty ``keys`` list is rejected when it is created or updated. To copy every key, leave ``keys`` out.

The data of a copy always equals what the rule selects from its source secret. A key which the source secret drops, or which is no longer listed, is removed from the copies, and so is a key which anything else adds to a copy.

The type of a copy is that of its source secret, and Kubernetes requires a secret of some types to hold certain keys. A copy of a ``kubernetes.io/tls`` secret must keep both ``tls.crt`` and ``tls.key``, so ``keys`` must list both for such a source secret, beside any other key wanted, such as ``ca.crt``. Where the keys selected leave out a key the type requires, Kubernetes rejects the copy, and the ``SecretCopier`` reports the failure for each namespace the copy could not be written to.

Limiting set of target namespaces
---------------------------------

To limit the set of target namespaces based on name, set ``targetNamespaces.nameSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetNamespaces:
      nameSelector:
        matchNames:
        - example-1
        - example-2
```

The list of names can be the exact value, or you can also use a shell style glob expression to match a set of names.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetNamespaces:
      nameSelector:
        matchNames:
        - example-*
```

In a glob expression ``*`` matches any sequence of characters, ``?`` any single character, and ``[seq]`` any character in ``seq``. A name or glob expression with a leading ``!`` excludes the namespaces it matches. A namespace is selected when it matches one of the entries without a leading ``!``, or when every entry has one, and matches none of the entries with a leading ``!``. As YAML reads a value starting with ``!`` as a tag, such an entry must be quoted, as in ``- "!kube-*"``.

Alternatively, you can match on labels on the namespace by setting a ``targetNamespaces.labelSelector.matchLabels``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetNamespaces:
      labelSelector:
        matchLabels:
          developer-namespace: ""
```

Or if more flexible matching of labels is required, you can supply a list of expressions by setting ``targetNamespaces.labelSelector.matchExpressions``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetNamespaces:
      labelSelector:
        matchExpressions:
        - key: developer-namespace
          operator: In
          values:
          - ""
```

A ``labelSelector`` behaves as a Kubernetes label selector does. For an expression the list of operators is ``In``, ``NotIn``, ``Exists`` and ``DoesNotExist``. For ``In`` and ``NotIn`` a non empty list of values must be supplied, and ``Exists`` and ``DoesNotExist``, which only pertain to the existence of the label key, take no values. A rule whose ``labelSelector`` Kubernetes would not accept as a label selector, such as one with an ``In`` expression without values, an ``Exists`` expression with values, or a label key or value which is not valid, selects no namespace, and the ``SecretCopier`` reports ``InvalidSelector`` in its status.

When targeting a specific namespace and the namespace is deleted, then recreated, the rule will still match the new instance of the namespace if selectors match the new instance of the namespace, even though it may notionally be used for a different purpose and the secret shouldn't be copied there.

If you need to ensure that a secret is only copied to that specific instance of the namespace, and not a future version of the namespace created subsequent to its deletion, you can require the namespace have a specific ``uid`` by setting ``targetNamespaces.uidSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetNamespaces:
      uidSelector:
        matchUIDs:
        - 9b030c48-b773-4ffb-9d8f-7eacf7dbcfbe
```

The ``uid`` should be that set by Kubernetes in the ``metadata`` section of the namespace resource.

Alternatively, you can restrict whether the secret should be copied to a specific instance of a namespace based on whether that resource is owned by another. This can be done by providing the details of the owner by setting ``targetNamespaces.ownerSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetNamespaces:
      ownerSelector:
        matchOwners:
        - apiVersion: training.educates.dev/v1beta1
          kind: WorkshopSession
          name: educates-cli-w01-s001
          uid: dcc4cf0f-6774-4eb4-bbf7-77a4ecaefa42
```

A namespace matches when one of its owner references has the ``apiVersion``, ``kind``, ``name`` and ``uid`` of one of the owners listed.

Only namespaces which are active are targeted, so a namespace which is being deleted receives no copy. A namespace created after the ``SecretCopier``, or whose labels or owners change so that a rule selects it, receives its copy within seconds.

Kubernetes reserved namespaces
------------------------------

When a rule has no ``nameSelector``, or its ``matchNames`` is an empty list, the rule behaves as if it had been given:

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    targetNamespaces:
      nameSelector:
        matchNames:
        - "!kube-*"
```

That is, neither ``kube-public``, ``kube-system`` or other namespaces starting with ``kube-`` will be targeted, they being generally reserved by Kubernetes itself. The ``!`` before the names indicates the namespace will be excluded.

If you need to target these namespaces, you will need to list them explicitly using ``matchNames``, which replaces the default. When using ``labelSelector`` and you need to exclude certain namespaces even though they may have matching labels, you can list those namespaces in ``matchNames`` with the leading ``!`` to have them excluded. When overriding ``matchNames`` to list excluded namespaces, you will need to include the exclusion for ``kube-*`` yourself if necessary.

Authorizing copying of a secret
-------------------------------

For extra control over whether a secret should be copied to a namespace, even when selectors match, a shared secret can be defined.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    copyAuthorization:
      sharedSecret: my-shared-secret
```

In this case the target namespace must contain an instance of the namespaced custom resource ``SecretImporter``, with name the same as that which would be used for the secret were the secret copied to that namespace. The shared secret must match that set by the rule in the ``SecretCopier`` instance. An empty ``sharedSecret`` also requires a ``SecretImporter``, holding an empty shared secret.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretImporter
metadata:
  name: registry-credentials
  namespace: target-namespace
spec:
  copyAuthorization:
    sharedSecret: my-shared-secret
```

A ``SecretImporter`` named after the copy can also refuse it, through its ``sourceNamespaces``, when the source secret is in a namespace it does not list. This holds for every rule, with or without ``copyAuthorization``.

Changing the shared secret of the ``SecretImporter`` so that it no longer matches, or deleting the ``SecretImporter``, leaves a copy already made in place, and the copy is no longer updated.

Multiple target namespace selectors
-----------------------------------

More than one type of selector can be listed under ``targetNamespaces``, in which case all selectors must match else the secret will not be copied to the namespace.

Multiple rules for copying secrets
----------------------------------

The ``rules`` property is a list, so rules related to more than one secret or set of target namespaces can be specified in the one custom resource. A ``SecretCopier`` can have at most 100 rules.

Overlapping rules for target secret
-----------------------------------

A copy is managed by the rule which created it, or took it over, which records itself in the copy's tracking annotations, described below. When rules of several ``SecretCopier`` or ``SecretExporter`` resources, or several rules of the same one, target the same secret name in a namespace:

* Rules which copy the same source secret agree, whatever keys they select. The resource whose rule created the copy keeps it, and the ``targetSecret`` labels and annotations and the ``sourceSecret`` keys of that rule decide what the copy holds, even where those of the other rules differ. Where several rules of that resource copy the source secret under the name, the first of them in ``spec.rules`` decides. The other rules count the copy as in sync and write nothing.

* A rule which copies a different source secret leaves the copy untouched, and reports a ``Conflict`` for that namespace.

A secret which the secrets manager did not create, being one without the ``secrets.educates.dev/copier-rule`` annotation, is never changed, even when it has the name a rule gives its copies. The rule leaves the secret untouched and reports a ``Conflict`` for that namespace. So does a secret whose ``secrets.educates.dev/copier-rule`` annotation does not name a ``SecretCopier`` or ``SecretExporter``. If you meant the rule to manage that secret, delete the secret, and the rule creates its copy in its place.

A copy stays with the resource whose rule created it or took it over for as long as one of that resource's rules still targets the copy: a rule which selects the namespace of the copy, gives its copies the name of the copy, and copies the source secret its ``secrets.educates.dev/secret-name`` annotation names. A rule which the ``SecretImporter`` named after the copy refuses, or a rule with ``copyAuthorization`` where there is no such ``SecretImporter``, does not target the copy, and no rule of a resource which is being deleted does. Once none does, because the rule no longer selects the namespace, copies another source secret, gives its copies another name, was removed, or is no longer accepted by the ``SecretImporter``, the copy is left in place, and within seconds another rule which targets the copy takes it over and updates it from its own source secret, clearing the ``Conflict`` it reported if its source secret differs. When that resource is deleted, the copy is deleted or left in place as the reclaim policy of its rule says, described below, and within seconds another rule which targets the copy creates it again, or takes over the copy left in place in the same way.

Deletion of the secret copier
-----------------------------

The secrets manager never deletes a copy because its source secret is deleted, or because the rules change such that it would no longer be copied, such as a rule which is removed or which no longer selects the namespace. Such a copy stays as it is, and is no longer updated, unless another rule which targets it takes it over, as described above.

What happens to the copies when the ``SecretCopier`` is deleted is set by each rule's ``reclaimPolicy``. With ``Delete``, the default, each copy has an owner reference to the ``SecretCopier``, and Kubernetes deletes the copies when the ``SecretCopier`` is deleted. With ``Retain``, copies have no owner reference to the ``SecretCopier``, and stay when the ``SecretCopier`` is deleted.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretCopier
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    reclaimPolicy: Retain
```

Changing the ``reclaimPolicy`` of a rule adds or removes the owner reference on the copies the rule has already made, so the policy applies to every copy of the rule, whenever it was made.

The owner reference marks the ``SecretCopier`` as the controller of the copy, and blocks a foreground deletion of the ``SecretCopier`` until its copies are gone. An owner reference which another tool sets on a copy is left alone. Kubernetes allows only one owner reference of an object to be its controller, so when another tool has set an owner reference marked as controller on a copy, a rule with the ``Delete`` reclaim policy cannot write the copy, and reports a failure for that namespace.

The secrets manager adds no finalizer to a ``SecretCopier``, so deleting a ``SecretCopier`` never waits for the secrets manager, even when it is not running.

Changes to a copy
-----------------

A copy which is deleted is created again within seconds. A copy whose data, or whose labels or annotations set by its rule, are changed by someone else, is restored within seconds.

When the source secret is replaced by one of a different ``type``, its copies are deleted and created again with the new type, since Kubernetes does not let the type of a secret change. A copy is deleted only once Kubernetes has said it would accept the new one. Where it would not, such as when ``sourceSecret.keys`` leaves out a key the new type requires, the copy is kept as it is and the rule reports a failure for that namespace.

A copy which someone else has marked with ``immutable: true`` is never deleted. When its data or type must change to follow its source secret, the rule reports a failure for that namespace, retried until someone deletes the copy, as Kubernetes never lets the data of an immutable secret change, and the rule then creates it again. The message of the ``Ready`` condition says that the copy is immutable and was not updated, and the secrets manager records a ``Warning`` Event with reason ``CopyNotUpdated``, described below.

Tracking the source of a secret
-------------------------------

When secrets are copied, the copy of a secret will have the following annotation added so that it can be determined where the secret was copied from:

```
secrets.educates.dev/secret-name: namespace/name
```

The ``SecretCopier`` instance containing the rules which resulted in the secret being copied is recorded using the annotation:

```
secrets.educates.dev/copier-rule: secretcopier/name
```

The ``resourceVersion`` of the source secret the copy was last written from is recorded using the annotation:

```
secrets.educates.dev/source-version: "123456"
```

A copy is written only when what it should hold changes. A change to the source secret which alters nothing the copy holds, such as an annotation added to the source secret, leaves the copy as it is, recording an earlier version of the source secret.

Status of the secret copier
---------------------------

The status of a ``SecretCopier`` says whether every copy its rules make is in sync with its source secret, and why not. Listing ``SecretCopier`` resources shows the ``Ready`` condition, its reason, and how many of the target namespaces selected hold a copy which is in sync:

```shell
kubectl get secretcopiers
```

```
NAME                   READY   REASON          IN SYNC   AGE
registry-credentials   True    InSync          4/4       5m
theme-credentials      False   SourceMissing   0/2       30s
```

The ``Ready`` condition is ``True``, with reason ``InSync``, when every target namespace each rule selects holds a copy in sync with its source secret, or a copy another rule copying the same source secret keeps, and each source secret holds every key its rule lists. Otherwise it is ``False``, with the reason of the first of these problems which applies:

* ``InvalidSelector``: a rule's ``targetNamespaces`` is not valid, so the rule selects no namespace.

* ``SourceMissing``: a rule's source secret does not exist, or is in a namespace which is being deleted, so it is not copied.

* ``SourceKeysMissing``: a rule's source secret lacks keys its ``sourceSecret.keys`` lists. The copies hold the listed keys it has, and count as in sync.

* ``TargetErrors``: a copy could not be written in some namespaces, and is retried.

* ``Conflict``: in some namespaces a secret with the name of the copy exists which the secrets manager did not create, or which another rule copies from another source secret, and it is left untouched.

* ``AwaitingAuthorization``: in some namespaces no ``SecretImporter`` accepts the copy. Either a rule with ``copyAuthorization`` finds no ``SecretImporter`` there holding its shared secret, or the ``SecretImporter`` named after the copy does not admit the namespace of the source secret.

The message of the condition names up to five of the namespaces affected, what failed where a copy could not be written, and the keys a source secret lacks. The ``status.summary`` counts, for all the rules together, the ``targetNamespaces`` selected and, of them, those whose copy is ``inSync``, those in ``conflicts``, those ``awaitingAuthorization`` and the ``failures``. Its ``inSyncRatio`` is the ``In Sync`` column. The ``status.rules`` list gives the same counts for each rule, in the order of ``spec.rules``, with the name and namespace of its source secret, as ``sourceSecret``, and whether that secret exists, as ``sourceExists``.

```yaml
status:
  observedGeneration: 1
  conditions:
  - type: Ready
    status: "True"
    reason: InSync
    message: Every target is in sync
    observedGeneration: 1
    lastTransitionTime: "2026-11-21T10:15:00Z"
  summary:
    targetNamespaces: 4
    inSync: 4
    conflicts: 0
    awaitingAuthorization: 0
    failures: 0
    inSyncRatio: 4/4
  rules:
  - sourceSecret:
      name: registry-credentials
      namespace: registry
    sourceExists: true
    targetNamespaces: 4
    inSync: 4
    conflicts: 0
    awaitingAuthorization: 0
    failures: 0
```

A copy which cannot be written in one namespace never stops copies into the other namespaces. It is retried with an increasing delay, at most 5 minutes apart, so a problem which is fixed clears without restarting anything. Every ``SecretCopier`` is also checked again 5 minutes after it was last checked, which repairs any copy a missed change left out of sync.

Events of the secret copier
---------------------------

When a ``SecretCopier`` has not been ``Ready`` for a minute, the secrets manager records a ``Warning`` Event with reason ``NotReady``, giving the reason and message of the ``Ready`` condition. When a ``SecretCopier`` which got that Event is ``Ready`` again, it records a ``Normal`` Event with reason ``Recovered``. No Event is recorded for a ``SecretCopier`` which is ``Ready`` again within a minute, such as one created moments before its source secret, while its status shows the problem at once. When the secrets manager restarts, or another of its replicas takes over, it records the ``NotReady`` Event again for a resource which has still not been ``Ready`` for a minute, and the ``Recovered`` Event only follows a ``NotReady`` Event the running secrets manager recorded.

When a copy which someone else has marked with ``immutable: true`` cannot follow a change of its source secret, the secrets manager records a ``Warning`` Event with reason ``CopyNotUpdated``, naming the copy, as ``namespace/name``, its source secret, and whether the data or the type of the source secret changed. The Event is recorded once for each change of the source secret the copy cannot follow, not each time the copy is retried, nor when the change alters nothing the copy would hold, and once more after the secrets manager restarts.

As a ``SecretCopier`` has no namespace, its Events are recorded in the ``default`` namespace. Run ``kubectl describe secretcopier <name>`` to see them.

Access to secret copiers
------------------------

The ``educates:rbac:admin:secrets`` cluster role grants full access to ``SecretCopier`` resources, and the ``educates:rbac:view:secrets`` cluster role grants read access to them. Their rules are also aggregated into the built-in ``admin`` and ``view`` cluster roles.
