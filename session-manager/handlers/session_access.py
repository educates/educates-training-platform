"""Objects session-manager creates in a session namespace so that the
session's service account, and the console running as it, can reach what
they need there, whatever the session's role.

Attendees cannot list the cluster's namespaces. They reach their own session
namespace through a RoleBinding in it to a ClusterRole granting `get` on
namespaces, which, being namespaced, grants `get` on that namespace alone.
The console, unable to list namespaces, is given its settings up front.

The console writes its own Secrets when they are missing or empty, which a
session role that cannot create or update Secrets refuses. It is given both
Secrets with their keys up front, and a RoleBinding to a ClusterRole granting
`get` on them and on its settings by name, so it only reads them and works
under any session role without attendees being able to write Secrets.
"""

import base64
import json
import secrets

from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

SESSION_NAMESPACE_ROLE_BINDING = "educates-session-namespace"

SESSION_NAMESPACE_CLUSTER_ROLE = "educates:session:namespace"

CONSOLE_ROLE_BINDING = "educates-session-console"

CONSOLE_CLUSTER_ROLE = "educates:session:console"

CONSOLE_SETTINGS_CONFIG_MAP = "kubernetes-dashboard-settings"

CONSOLE_CSRF_SECRET = "kubernetes-dashboard-csrf"

CONSOLE_KEY_HOLDER_SECRET = "kubernetes-dashboard-key-holder"


def _service_account_role_binding(
    name, cluster_role, session_namespace, service_account, workshop_namespace, labels
):
    return {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {
            "name": name,
            "namespace": session_namespace,
            "labels": dict(labels),
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "ClusterRole",
            "name": cluster_role,
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": service_account,
                "namespace": workshop_namespace,
            }
        ],
    }


def session_namespace_role_binding(
    session_namespace, service_account, workshop_namespace, labels
):
    """Returns the RoleBinding which lets the session's service account get
    its own session namespace. It is created for every session, whatever
    its role, so a custom role which does not grant it still resolves its
    own namespace.

    """

    return _service_account_role_binding(
        SESSION_NAMESPACE_ROLE_BINDING,
        SESSION_NAMESPACE_CLUSTER_ROLE,
        session_namespace,
        service_account,
        workshop_namespace,
        labels,
    )


def console_role_binding(session_namespace, service_account, workshop_namespace, labels):
    """Returns the RoleBinding which lets the session's service account, as
    which the console runs, get the console's Secrets and settings by name.
    It is created for every session with the console enabled, whatever its
    role, so the console starts under a role which grants no Secret access.

    """

    return _service_account_role_binding(
        CONSOLE_ROLE_BINDING,
        CONSOLE_CLUSTER_ROLE,
        session_namespace,
        service_account,
        workshop_namespace,
        labels,
    )


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


def console_csrf_secret(session_namespace, labels):
    """Returns the Secret holding the console's CSRF key, which must exist
    before the console starts. It holds a random key of 256 bytes, as the
    console would generate, since the console writes a key into the Secret
    when it finds none, which a role that cannot update Secrets refuses.

    """

    return {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": CONSOLE_CSRF_SECRET,
            "namespace": session_namespace,
            "labels": dict(labels),
        },
        "data": {
            "csrf": base64.b64encode(secrets.token_bytes(256)).decode("ascii"),
        },
    }


def console_key_holder_secret(session_namespace, labels):
    """Returns the Secret holding the key the console encrypts its tokens
    with, which must exist before the console starts, since the console
    creates the Secret when it is missing, which a role that cannot create
    Secrets refuses. It holds a new RSA 2048 key in the console's own
    format: the private key under `priv` as PKCS#1, and its public key
    under `pub` as PKIX, each in a PEM block.

    """

    private_key = rsa.generate_private_key(public_exponent=65537, key_size=2048)

    private_pem = private_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.TraditionalOpenSSL,
        encryption_algorithm=serialization.NoEncryption(),
    )

    public_pem = private_key.public_key().public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )

    return {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": CONSOLE_KEY_HOLDER_SECRET,
            "namespace": session_namespace,
            "labels": dict(labels),
        },
        "data": {
            "priv": base64.b64encode(private_pem).decode("ascii"),
            "pub": base64.b64encode(public_pem).decode("ascii"),
        },
    }
