"""Objects session-manager creates in a session namespace so that the
session's service account, and the console running as it, can reach what
they need there, whatever the session's role.

Attendees cannot list the cluster's namespaces. They reach their own session
namespace through a RoleBinding in it to a ClusterRole granting `get` on
namespaces, which, being namespaced, grants `get` on that namespace alone.
The console, unable to list namespaces, is given its settings up front.
"""

import json

SESSION_NAMESPACE_ROLE_BINDING = "educates-session-namespace"

SESSION_NAMESPACE_CLUSTER_ROLE = "educates:session:namespace"

CONSOLE_SETTINGS_CONFIG_MAP = "kubernetes-dashboard-settings"


def session_namespace_role_binding(
    session_namespace, service_account, workshop_namespace, labels
):
    """Returns the RoleBinding which lets the session's service account get
    its own session namespace. It is created for every session, whatever
    its role, so a custom role which does not grant it still resolves its
    own namespace.

    """

    return {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {
            "name": SESSION_NAMESPACE_ROLE_BINDING,
            "namespace": session_namespace,
            "labels": dict(labels),
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "ClusterRole",
            "name": SESSION_NAMESPACE_CLUSTER_ROLE,
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": service_account,
                "namespace": workshop_namespace,
            }
        ],
    }


def console_settings(session_namespace, secondary_namespaces):
    """Returns the console's settings object for a session. The console
    reads it as a whole, so every field is set: one left out would be zero,
    and an `itemsPerPage` of 0 empties every list. The console opens on the
    session namespace and offers it followed by the secondary namespaces,
    and, since it cannot list namespaces, does not report each refused
    request.

    """

    return {
        "clusterName": "",
        "itemsPerPage": 10,
        "labelsLimit": 3,
        "logsAutoRefreshTimeInterval": 5,
        "resourceAutoRefreshTimeInterval": 5,
        "disableAccessDeniedNotifications": True,
        "defaultNamespace": session_namespace,
        "namespaceFallbackList": [session_namespace, *secondary_namespaces],
    }


def console_settings_config_map(session_namespace, secondary_namespaces, labels):
    """Returns the ConfigMap holding the console's settings, which must
    exist before the console starts: the console creates it with its own
    defaults when it is missing, and only reads it when it exists.

    """

    return {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {
            "name": CONSOLE_SETTINGS_CONFIG_MAP,
            "namespace": session_namespace,
            "labels": dict(labels),
        },
        "data": {
            "_global": json.dumps(
                console_settings(session_namespace, secondary_namespaces)
            ),
        },
    }
