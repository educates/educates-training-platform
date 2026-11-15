"""The RoleBindings session-manager creates to grant a service account a Role
or ClusterRole within a single namespace."""


def service_account_role_binding(
    name,
    namespace,
    role_kind,
    role_name,
    service_account,
    service_account_namespace,
    labels,
):
    """Returns the body of the RoleBinding named name in namespace, granting
    the role_kind, Role or ClusterRole, named role_name to the service
    account named service_account in service_account_namespace.

    """

    return {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {
            "name": name,
            "namespace": namespace,
            "labels": dict(labels),
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": role_kind,
            "name": role_name,
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": service_account,
                "namespace": service_account_namespace,
            }
        ],
    }
