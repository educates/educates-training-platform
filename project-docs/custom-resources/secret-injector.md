Secret Injector
===============

The ``SecretInjector`` custom resource sets rules for injecting secrets, image pull secrets in particular, into service accounts. The ``SecretInjector`` is a cluster scoped resource.

The raw custom resource definition for the ``SecretInjector`` custom resource can be viewed by running:

```shell
kubectl get crd/secretinjectors.secrets.educates.dev -o yaml
```

Specifying the source secrets
-----------------------------

To inject the named secrets in any namespace, into all service accounts in the same namespace as the secret, set ``sourceSecrets.nameSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - registry-credentials
```

A secret of type ``kubernetes.io/dockerconfigjson`` or ``kubernetes.io/dockercfg`` holds image pull credentials, and is added to the ``imagePullSecrets`` of the service account. A secret of any other type is added to the ``secrets`` of the service account.

The list of names can be the exact value, or you can also use a shell style glob expression to match a set of names, in which ``*`` matches any sequence of characters, ``?`` any single character, and ``[seq]`` any character in ``seq``. A name or glob expression with a leading ``!`` excludes the secrets it matches. A secret is selected when it matches one of the entries without a leading ``!``, or when every entry has one, and matches none of the entries with a leading ``!``. As YAML reads a value starting with ``!`` as a tag, such an entry must be quoted.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - "*-pull-secret"
        - "!legacy-pull-secret"
```

A rule must select its source secrets with at least one of ``nameSelector``, ``labelSelector``, ``uidSelector`` and ``ownerSelector`` under ``sourceSecrets``, so that a rule cannot inject every secret by accident. An empty selector counts as no selector at all: a ``nameSelector`` whose ``matchNames`` is an empty list, a ``uidSelector`` whose ``matchUIDs`` is an empty list, an ``ownerSelector`` whose ``matchOwners`` is an empty list, and a ``labelSelector`` with neither ``matchLabels`` nor ``matchExpressions``. The other selectors of ``sourceSecrets`` then decide which secrets are injected. A rule left with no selector which is not empty selects no secret, and the ``SecretInjector`` reports ``InvalidSelector`` in its status. This applies only to ``sourceSecrets``: an empty ``labelSelector`` under ``targetNamespaces`` or ``serviceAccounts`` restricts nothing, as an empty Kubernetes label selector does.

Selecting source secrets by owner or UID
----------------------------------------

To inject the secrets owned by a given resource, set ``sourceSecrets.ownerSelector``. A secret matches when one of its owner references has the ``apiVersion``, ``kind``, ``name`` and ``uid`` of one of the owners listed.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      ownerSelector:
        matchOwners:
        - apiVersion: training.educates.dev/v1beta1
          kind: WorkshopSession
          name: educates-cli-w01-s001
          uid: dcc4cf0f-6774-4eb4-bbf7-77a4ecaefa42
```

To inject only specific instances of secrets, set ``sourceSecrets.uidSelector`` to the ``uid`` of each, as set by Kubernetes in the ``metadata`` section of the secret.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      uidSelector:
        matchUIDs:
        - 4f0d6c43-9b2e-4d8a-a7a5-2c1e7d3b5f60
```

Service account names to match
------------------------------

If you only want the secret injected into the ``default`` service account set ``serviceAccounts.nameSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - registry-credentials
    serviceAccounts:
      nameSelector:
        matchNames:
        - default
```

More than one service account name can be specified if required. Service account names are exact names, not glob expressions. A rule without ``serviceAccounts`` injects into every service account in the namespaces it targets.

Filtering based on resource labels
----------------------------------

Labels can instead be used on both the source secret and service accounts using ``sourceSecrets.labelSelector`` and ``serviceAccounts.labelSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      labelSelector:
        matchLabels:
          image-pull-secret: ""
    serviceAccounts:
      labelSelector:
        matchLabels:
          inject-image-pull-secrets: ""
```

A ``labelSelector`` behaves as a Kubernetes label selector does, here and under ``targetNamespaces``, described below.

Limiting set of target namespaces
---------------------------------

You can be selective about what namespaces injection is performed. This can be done by specifying the names of the namespaces using ``targetNamespaces.nameSelector``:

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - registry-credentials
    targetNamespaces:
      nameSelector:
        matchNames:
        - developer-1
        - developer-2
```

The list of names can be the exact value, or you can also use a shell style glob expression to match a set of names, and an entry with a leading ``!`` excludes the namespaces it matches, as for source secrets.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - registry-credentials
    targetNamespaces:
      nameSelector:
        matchNames:
        - developer-*
```

Rather than matching namespaces by their name, you can match any labels on a namespace by setting ``targetNamespaces.labelSelector.matchLabels``:

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - registry-credentials
    targetNamespaces:
      labelSelector:
        matchLabels:
          developer-namespace: ""
```

Or if more flexible matching of labels is required, you can supply a list of expressions by setting ``targetNamespaces.labelSelector.matchExpressions``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - registry-credentials
    targetNamespaces:
      labelSelector:
        matchExpressions:
        - key: developer-namespace
          operator: In
          values:
          - ""
```

For an expression the list of operators is ``In``, ``NotIn``, ``Exists`` and ``DoesNotExist``. For ``In`` and ``NotIn`` a non empty list of values must be supplied, and ``Exists`` and ``DoesNotExist``, which only pertain to the existence of the label key, take no values. A rule with a ``labelSelector`` which Kubernetes would not accept as a label selector, such as one with an ``In`` expression without values, an ``Exists`` expression with values, or a label key or value which is not valid, selects nothing, and the ``SecretInjector`` reports ``InvalidSelector`` in its status.

When targeting a specific namespace and the namespace is deleted, then recreated, the rule will still match the new instance of the namespace if selectors match the new instance of the namespace, even though it may notionally be used for a different purpose and the secret shouldn't be copied there.

If you need to ensure that secrets should only be injected into service accounts in context of that specific instance of the namespace, and not a future version of the namespace created subsequent to its deletion, you can require the namespace have a specific ``uid`` by setting ``targetNamespaces.uidSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - registry-credentials
    targetNamespaces:
      uidSelector:
        matchUIDs:
        - 9b030c48-b773-4ffb-9d8f-7eacf7dbcfbe
```

The ``uid`` should be that set by Kubernetes in the ``metadata`` section of the namespace resource.

Alternatively, you can restrict injection to namespaces owned by another resource, by providing the details of the owner by setting ``targetNamespaces.ownerSelector``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - registry-credentials
    targetNamespaces:
      ownerSelector:
        matchOwners:
        - apiVersion: training.educates.dev/v1beta1
          kind: WorkshopSession
          name: educates-cli-w01-s001
          uid: dcc4cf0f-6774-4eb4-bbf7-77a4ecaefa42
```

Only namespaces which are active are targeted.

Kubernetes reserved namespaces
------------------------------

When a rule has no ``targetNamespaces.nameSelector``, or its ``matchNames`` is an empty list, the rule behaves as if it had been given:

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretInjector
metadata:
  name: registry-credentials
spec:
  rules:
  - sourceSecrets:
      nameSelector:
        matchNames:
        - registry-credentials
    targetNamespaces:
      nameSelector:
        matchNames:
        - "!kube-*"
```

That is, neither ``kube-public``, ``kube-system`` or other namespaces starting with ``kube-`` will be targeted, they being generally reserved by Kubernetes itself. The ``!`` before the names indicates the namespace will be excluded.

If you need to target these namespaces, you will need to list them explicitly using ``matchNames``, which replaces the default. When using ``labelSelector`` and you need to exclude certain namespaces even though they may have matching labels, you can list those namespaces in ``matchNames`` with the leading ``!`` to have them excluded. When overriding ``matchNames`` to list excluded namespaces, you will need to include the exclusion for ``kube-*`` yourself if necessary.

Multiple selectors for resources
--------------------------------

More than one type of selector can be listed under ``sourceSecrets``, ``serviceAccounts`` and ``targetNamespaces``, in which case all selectors must match else nothing will be done in respect of the secret, service account or namespace.

Multiple rules for injecting secrets
------------------------------------

The ``rules`` property is a list, so rules for more than one secret can be specified in the one custom resource.

When secrets are injected
-------------------------

Secrets are injected within seconds of the ``SecretInjector`` being created or changed. A service account, a secret or a namespace which is created after the ``SecretInjector``, or whose labels or owners change so that a rule selects it, is injected within seconds too.

A name is never added twice to a list of a service account, and each service account is updated at most once for each change, with every name it lacks from every rule.

Deletion of the target secret
-----------------------------

The secrets manager never removes a name from a service account. The name of a secret stays in the lists of a service account when the secret is deleted, when the rules change so that the secret or the service account would no longer have been selected, and when the ``SecretInjector`` is deleted. A name which is removed by hand from a service account a rule still selects, while the rule still selects its secret, is added back.

The secrets manager adds no finalizer to a ``SecretInjector``, so deleting a ``SecretInjector`` never waits for the secrets manager, even when it is not running.

Status of the secret injector
-----------------------------

The status of a ``SecretInjector`` says whether every service account its rules select holds every secret they inject into it, and why not. Listing ``SecretInjector`` resources shows the ``Ready`` condition, its reason, and how many of the service accounts selected are in sync:

```shell
kubectl get secretinjectors
```

```
NAME                   READY   REASON   IN SYNC   AGE
registry-credentials   True    InSync   12/12     5m
```

The ``Ready`` condition is ``True``, with reason ``InSync``, when every service account each rule selects holds every secret the rule injects into it. Otherwise it is ``False``, with the reason of the first of these problems which applies:

* ``InvalidSelector``: a selector of a rule is not valid, or every selector of its ``sourceSecrets`` is empty, so the rule selects nothing.

* ``TargetErrors``: some service accounts could not be updated, or the contents of some namespaces could not be read, and they are retried.

The message of the condition names up to five of the namespaces affected, and what failed. The ``status.summary`` counts, for all the rules together, the ``targetNamespaces`` selected, the ``serviceAccountsMatched`` in them, the ``injectionsInSync``, being the service accounts matched which hold every secret their rule injects into them, and the ``failures``. Its ``inSyncRatio``, the service accounts in sync out of those matched, is the ``In Sync`` column. The ``status.rules`` list gives the same counts for each rule, in the order of ``spec.rules``.

A service account which cannot be updated never stops injection into the others. It is retried with an increasing delay, at most 5 minutes apart. Every ``SecretInjector`` is also checked again 5 minutes after it was last checked, which repairs any service account a missed change left out of sync.

Events of the secret injector
-----------------------------

When a ``SecretInjector`` has not been ``Ready`` for a minute, the secrets manager records a ``Warning`` Event with reason ``NotReady``, giving the reason and message of the ``Ready`` condition. When a ``SecretInjector`` which got that Event is ``Ready`` again, it records a ``Normal`` Event with reason ``Recovered``. No Event is recorded for a ``SecretInjector`` which is ``Ready`` again within a minute, while its status shows the problem at once.

As a ``SecretInjector`` has no namespace, its Events are recorded in the ``default`` namespace. Run ``kubectl describe secretinjector <name>`` to see them.

Access to secret injectors
--------------------------

The ``educates:rbac:admin:secrets`` cluster role grants full access to ``SecretInjector`` resources, and the ``educates:rbac:view:secrets`` cluster role grants read access to them. Their rules are also aggregated into the built-in ``admin`` and ``view`` cluster roles.
