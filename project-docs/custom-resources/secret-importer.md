Secret Importer
===============

The ``SecretImporter`` custom resource is required to be used in conjunction with ``SecretExporter`` and ``SecretCopier`` when necessary to provide a shared secret value in order to authorize the copying of a secret into a target namespace. The ``SecretImporter`` is namespaced and thus users of a cluster could be given selective ability via RBAC to use them in a namespace.

The raw custom resource definition for the ``SecretImporter`` custom resource can be viewed by running:

```shell
kubectl get crd/secretimporters.secrets.educates.dev -o yaml
```

Providing authorization to copy
-------------------------------

The primary purpose of the ``SecretImporter`` is to provide a shared secret value that indicates that a namespace is entitled to receive a secret exported from another namespace. The ``SecretImporter`` must have the name of the secret it accepts, the name the copy has in its namespace.

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

The shared secret value is mandatory so that if a ``SecretExporter`` or ``SecretCopier`` is created that targets namespaces it shouldn't, that someone else in control of one of the targeted namespaces can't steal the secret. For someone to be able to get a copy of the secret, they would need to know the shared secret value.

As such, the ``SecretImporter`` needs to use the same shared secret values as would be defined in the ``SecretExporter``:

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

or a ``SecretCopier``:

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

When the rule for copying a secret is defined by a ``SecretExporter``, it is possible that it may not define a shared secret value. In this case a shared secret value is still required in the corresponding ``SecretImporter``, but what should be used is the Kubernetes object ID of the ``SecretExporter`` resource.

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

That is, the shared secret value would be the ``uid`` from the ``metadata`` section of the ``SecretExporter``.

A ``SecretCopier`` rule which does not set ``copyAuthorization`` copies its secret into the namespaces it selects whether or not a ``SecretImporter`` is there. A ``SecretImporter`` named after the copy can still refuse such a copy through its ``sourceNamespaces``, described below.

Limiting the source namespaces
------------------------------

If multiple rules within different ``SecretExporter`` or ``SecretCopier`` custom resource instances target the same secret, the one which created the target secret keeps it, and the others leave it untouched, as the page for the ``SecretCopier`` custom resource describes.

When using ``SecretImporter`` it is possible to qualify from which namespace a source secret may be copied from, by listing the namespaces in ``sourceNamespaces.nameSelector.matchNames``.

```yaml
apiVersion: secrets.educates.dev/v1beta1
kind: SecretImporter
metadata:
  name: registry-credentials
  namespace: developer-namespace
spec:
  sourceNamespaces:
    nameSelector:
      matchNames:
      - registry
  copyAuthorization:
    sharedSecret: my-shared-secret
```

As in ``targetNamespaces.nameSelector`` of the rules, the list can hold exact names and shell style glob expressions, and an entry with a leading ``!`` excludes the namespaces it matches. When ``sourceNamespaces`` is not set, or lists no names, a source secret is accepted from any namespace.

The ``SecretImporter`` refuses a copy of a source secret from a namespace its ``sourceNamespaces`` do not admit, whether the copy is offered by a ``SecretExporter`` or by a ``SecretCopier`` rule, with or without ``copyAuthorization``.

Revoking authorization to copy
------------------------------

Changing the shared secret of the ``SecretImporter`` so that it no longer matches that of the rule which made the copy, or deleting the ``SecretExporter`` which made the copy, leaves a copy already made in place, and that rule no longer updates the copy. A rule of another resource which targets the copy, and which the ``SecretImporter`` accepts, takes the copy over within seconds and updates it from its own source secret. What happens to a copy made by a ``SecretCopier`` when the ``SecretCopier`` is deleted is set by the reclaim policy of its rule.

Deletion of the secret importer
-------------------------------

When a secret is copied into a namespace due to use of paired ``SecretExporter`` and ``SecretImporter``, the target secret is owned by the ``SecretImporter`` custom resource instance, through an owner reference which marks it as the controller of the copy. If the ``SecretImporter`` custom resource instance is deleted, then the target secret will also be deleted.

A copy made by a ``SecretCopier`` is owned by the ``SecretCopier``, or by nothing under the ``Retain`` reclaim policy, so deleting a ``SecretImporter`` which authorized it leaves the copy in place, and the rule which made it no longer updates it.

Status of the secret importer
-----------------------------

The status of a ``SecretImporter`` says whether its secret has been imported, from which ``SecretExporter`` or ``SecretCopier``, or why not. Listing ``SecretImporter`` resources shows the ``Ready`` condition and its reason:

```shell
kubectl get secretimporters -n developer-namespace
```

```
NAME                   READY   REASON     AGE
registry-credentials   True    Imported   5m
```

The ``Ready`` condition is ``True``, with reason ``Imported``, when the secret named after the ``SecretImporter`` exists in its namespace, written by a ``SecretExporter`` or ``SecretCopier`` rule whose offer of the secret the ``SecretImporter`` accepts. Otherwise it is ``False``, with one of these reasons:

* ``AwaitingCopy``: the ``SecretImporter`` accepts the offer of a ``SecretExporter`` or ``SecretCopier``, and the copy is not there yet. Either it is about to be written, or it cannot be, such as when the source secret does not exist or another secret has the name of the copy, as the status of the offering resource says.

* ``AwaitingAuthorization``: ``SecretExporter`` or ``SecretCopier`` rules offer the secret to the namespace, and the ``SecretImporter`` accepts none of the offers, as it holds another shared secret, or its ``sourceNamespaces`` do not admit the namespace of the source secret. The message names each offer and why it is refused.

* ``NoMatchingExporter``: no ``SecretExporter`` or ``SecretCopier`` rule offers a secret of that name to the namespace.

``status.boundTo`` names the ``SecretExporter`` or ``SecretCopier`` the copy comes from, by its ``kind``, ``name`` and, for a ``SecretExporter``, ``namespace``: the one which wrote the copy once imported, or one whose offer the ``SecretImporter`` accepts while the copy is not there yet. ``status.targetSecretName`` is the name of the secret imported.

```yaml
status:
  observedGeneration: 1
  conditions:
  - type: Ready
    status: "True"
    reason: Imported
    message: Imported Secret registry-credentials from SecretExporter registry/registry-credentials
    observedGeneration: 1
    lastTransitionTime: "2026-11-21T10:15:00Z"
  boundTo:
    kind: SecretExporter
    namespace: registry
    name: registry-credentials
  targetSecretName: registry-credentials
```

Events of the secret importer
-----------------------------

When a ``SecretImporter`` imports its secret, the secrets manager records a ``Normal`` Event with reason ``Imported`` in its namespace. When a ``SecretImporter`` has not been ``Ready`` for a minute, it records a ``Warning`` Event with reason ``NotReady``, giving the reason and message of the ``Ready`` condition, and when a ``SecretImporter`` which got that Event is ``Ready`` again, a ``Normal`` Event with reason ``Recovered``. No ``Warning`` is recorded for a ``SecretImporter`` which imports within a minute, such as one created moments before its ``SecretExporter``, while its status shows where it stands at once. Run ``kubectl describe secretimporter <name> -n <namespace>`` to see them.

Access to secret importers
--------------------------

The shared secret of a ``SecretImporter`` is what authorizes the import of a secret, so the roles for viewing resources do not cover ``SecretImporter`` resources. The ``educates:rbac:view:secrets`` cluster role, and the built-in ``view`` cluster role into which it is aggregated, grant no access to them, so a user who can only view a namespace cannot read the shared secrets of its importers. The ``educates:rbac:admin:secrets`` cluster role grants full access to them, and is aggregated into the built-in ``admin`` cluster role.
