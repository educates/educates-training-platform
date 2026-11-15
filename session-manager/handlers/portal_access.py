"""Objects session-manager creates in a workshop environment namespace for
the training portal that owns the environment.

A training portal reads no Secrets. When it creates a session it creates the
Secret passing the session's config password to session-manager, and when it
allocates a session the Secrets holding the session's user details and
request parameters, all in the session's workshop environment namespace. It
may do so only in its own environment namespaces, through a RoleBinding in
each of them to a ClusterRole granting `create` on Secrets alone. The binding
trusts the environment's portal label, which an admission policy keeps a
portal from setting to another portal's name.
"""

from .portal_labels import portal_label
from .role_bindings import service_account_role_binding

PORTAL_ENVIRONMENT_ROLE_BINDING = "educates-training-portal"

PORTAL_ENVIRONMENT_CLUSTER_ROLE = "educates:training-portal:environment"

PORTAL_SERVICE_ACCOUNT = "training-portal"


def portal_environment_role_binding(environment, labels):
    """Returns the RoleBinding which lets the training portal named by the
    workshop environment's portal label create Secrets in the environment's
    namespace, which is named after the environment. The binding's subject
    is the portal's service account, in the portal's own namespace. An
    environment without a portal label, or with an empty one, is no
    portal's, and gets no binding, so this returns None.

    """

    portal_name = portal_label(environment)

    if not portal_name:
        return None

    return service_account_role_binding(
        name=PORTAL_ENVIRONMENT_ROLE_BINDING,
        namespace=environment["metadata"]["name"],
        role_kind="ClusterRole",
        role_name=PORTAL_ENVIRONMENT_CLUSTER_ROLE,
        service_account=PORTAL_SERVICE_ACCOUNT,
        service_account_namespace=f"{portal_name}-ui",
        labels=labels,
    )
