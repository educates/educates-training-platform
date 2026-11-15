Training portals are confined by admission policy, not by RBAC alone
====================================================================

A training portal creates the ``WorkshopEnvironment``, ``WorkshopSession``
and ``WorkshopAllocation`` resources for its own workshops, and the session
manager then acts on them with its own rights. It creates each environment's
namespace, creates the objects listed in an environment's
``spec.environment.objects`` in whatever namespace they name, and copies the
Secrets named in ``spec.environment.secrets``. The session manager can bind
``cluster-admin``, since it creates the RBAC that workshops ask for. These
training resources are cluster scoped, so RBAC can grant a portal create,
patch and delete on all of them or on none of them, never on only the ones it
owns. Without anything more, a portal that can create a
``WorkshopEnvironment`` can have the session manager create anything the
session manager may, or deploy any workshop in the cluster and open its
sessions.

Educates therefore confines each portal at admission. The session-manager
chart ships a ``ValidatingAdmissionPolicy`` that applies only to the training
portal service accounts, ``system:serviceaccount:<portal>-ui:training-portal``,
on create, update and delete of workshop environments, sessions and
allocations. It admits an object only when:

* its ``training.educates.dev/portal.name`` label names that portal, and so
  does the old object's on update and delete;
* its name, and the environment and session it refers to, follow the
  portal's naming: ``<portal>-wNN`` for environments, and names starting with
  the environment's name for sessions;
* for a workshop environment, ``spec.environment.objects`` and
  ``spec.environment.secrets`` are empty.

Because of that policy, the session manager can trust the portal label, and
it refuses a portal's workshop environment whose workshop is not listed in
that ``TrainingPortal``'s ``spec.workshops``. A policy cannot make that check
itself, as it cannot read the ``TrainingPortal``, and a portal cannot change
its own ``TrainingPortal``.

Admission does not cover reads. A portal still reads every training resource
in the cluster, so those resources carry no credentials: portal credentials
and each session's config password live in Secrets that only their consumers
can read.

The policy is part of the install contract. The portal relies on it to be
confined, and the session manager relies on it to trust the portal label, so
removing it later would give every portal the reach of the session manager
again.

Considered options
------------------

Kyverno policies could enforce the same rules, and could also look up the
``TrainingPortal``. Kyverno is the default policy engine, but an install can
run without it, as the workshop policy engine can be ``None``, and a portal
has to be confined on every install. ``ValidatingAdmissionPolicy`` is
generally available in every Kubernetes version Educates supports.

Making the training resources namespaced would let RBAC do the scoping, but it
would change the API of every training resource, and of every tool that reads
them, for this one property.

Moving the portal off kopf would let it list and watch with server-side
selectors and limit its reads with ``resourceNames``. That narrows what a
portal reads, not what the session manager does with what a portal creates,
so it would not replace the policy.

Documenting that one cluster hosts portals for a single trust domain would
cost nothing, but it would leave a compromised portal able to reach
``cluster-admin`` on every cluster, whoever the portals serve.
