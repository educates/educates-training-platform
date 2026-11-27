The CLI keeps the Secrets it places in a cluster in the operator namespace
==========================================================================

A deploy of a local configuration, and ``educates local secrets sync``, copy
the Secrets of the local secrets cache that the install needs into the
operator namespace, ``educates-installer``, creating the namespace before the
operator chart is installed. They are the CA key pair, the CA trust Secret and
the wildcard certificate of the local certificate decision, and the image pull
Secrets the configuration names. The ``EducatesClusterConfig`` references each
of them by name alone, which its ``SecretReference`` defines as a Secret in the
operator namespace. A CLI configuration names its image pull Secrets the same
way, in ``imageRegistry.pullSecrets``, one list of names that the CLI writes
into both the operator chart's ``imagePullSecrets`` and the
``EducatesClusterConfig``'s ``pullSecrets``. The ``educates-secrets``
namespace, where earlier releases synced the cache, is retired: the CLI no
longer creates or references it, and the operator no longer watches it.

The operator pod pulls its own image before anything in the cluster runs, so a
pull Secret for a private registry has to be in the operator's namespace when
the operator chart is installed. The ``EducatesClusterConfig`` is not Ready
until each Secret it names exists there, and the ``SecretsManager``, which
brings the ``SecretCopier`` resource and the controller that acts on it,
installs only once the ``EducatesClusterConfig`` is Ready. Nothing in the
cluster can place the Secrets the platform starts from, so the CLI places them
where the operator reads them, and the operator API stays as it was: its
references name Secrets in the operator namespace.

Configurations name their pull Secrets without a namespace, every local
cluster references its certificates by name alone, and the docs send
workshops to the copies the operator makes in the ``educates`` namespace, so
reversing this would mean giving configurations a namespace again and moving
every cluster's Secrets, and the workshops that copy them, a second time.

Considered options
------------------

A ``{name, namespace}`` entry for each pull Secret, copied into the operator
namespace by a ``SecretCopier``, would let a configuration name a Secret
wherever its user keeps it. A ``SecretCopier`` can't place it: it acts only
once the ``SecretsManager`` is installed, which waits for the
``EducatesClusterConfig`` to be Ready, which waits for the Secret. Nor could it
serve the operator's own pod, which has pulled its image, or failed to, long
before. With the CLI placing the Secrets instead, a namespace could only mean
"copy from there at deploy time", which ``educates admin platform render``
can't do, so one configuration would behave one way under ``deploy`` and
another under GitOps. Plain names also match
``EducatesInlineConfig.imageRegistry.pullSecrets`` and the CRD.

Keeping ``educates-secrets`` as a deprecated copy for one release, by also
syncing into it the cache's Secrets that hold no CA key, would have kept
working the workshops whose ``spec.environment.secrets`` copy from it, as the
docs' own package example did. It was rejected because 4.0 is not yet
released, so the namespace can go in the release that introduces the operator
namespace, with a release note, and because the case the docs showed, a
registry credential, has a replacement: the operator copies the platform's
pull Secrets into the ``educates`` namespace, and a workshop copies one from
there.

Keeping ``educates-secrets`` for good, as the namespace workshops copy Secrets
from, would give workshop authors a source that never moves. Only a deploy of
a local configuration synced into it, so a workshop that copied from it found
nothing on any other install, and the operator would keep the name of a
namespace that only a CLI convention creates in the set its Secret cache
always covers. The operator's copies in ``educates`` exist on every install
that names pull Secrets, however it was installed.

Consequences
------------

For ``EducatesGKEConfig``, ``EducatesEKSConfig`` and ``EducatesInlineConfig``
the CLI places no Secrets. The user creates the pull Secrets in
``educates-installer``, as the air-gapped guide already told them to, and
``educates admin platform deploy`` checks, before it installs the operator
chart, that each one ``imageRegistry.pullSecrets`` names exists there as a
``kubernetes.io/dockerconfigjson`` Secret, failing with the ``kubectl``
commands that create it. ``educates admin platform render`` can place nothing,
so it adds a comment naming the Secrets the operator namespace must hold
before ``helm install``, and for a local configuration says that ``educates
local secrets sync`` puts them there. ``EducatesConfig`` declares the operator
chart's ``imagePullSecrets`` and the ``EducatesClusterConfig``'s
``pullSecrets`` itself, and the CLI fills in neither.

The CLI creates the operator namespace with the ``name`` label Helm gives a
namespace it creates, and Helm's install accepts a namespace that already
exists, so creating a cluster and deploying later, a full ``educates local
cluster create``, and a rendered install applied by hand all meet the same
namespace.

A cluster deployed by an earlier 4.0 build moves on its next deploy. The
``EducatesClusterConfig``'s CEL rules hold only how each cluster service is
provided immutable, as
[0006](0006-cluster-service-providers-are-fixed-for-the-life-of-an-install.md)
records, so its Secret references may change namespace, and the operator
copies the CA key pair from its new source into the cert-manager namespace
with the same content. A deploy that finds ``educates-secrets`` says it is no
longer used and how to remove it, and leaves it, since deleting a namespace on
deploy is destructive and a user may have put Secrets of their own there.
``educates admin platform delete --purge`` removes it with the operator
namespace. Without ``--purge``, the operator namespace and the Secrets synced
into it stay for the next deploy.

The operator's Secret cache always covers the operator namespace, and no
longer ``educates-secrets``. A raw install whose ``EducatesClusterConfig``
names a Secret in another namespace has that namespace cached when the
``EducatesClusterConfig`` exists at operator startup, and otherwise after a
restart.

A workshop whose ``spec.environment.secrets`` copies a Secret from
``educates-secrets`` finds nothing on a new local cluster. It can copy one of
the platform's image pull Secrets from the ``educates`` namespace, or name the
namespace that holds its Secret. A package's ``pullSecretRef`` still needs
such an entry, since the session manager builds the credentials for fetching a
package only from the Secrets that list names. The version 3 migration carries
each ``clusterSecrets.pullSecretRefs`` entry in ``educates-secrets``, a Secret
of the cache both versions share, over to ``imageRegistry.pullSecrets``.
