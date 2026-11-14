"""Unit tests for a training portal's admin and robot credentials.

Each portal gets its own credentials, generated once when the portal is
created unless its definition supplies them, and kept in two Secrets in the
portal's namespace that its Deployment reads them from. The remote-access
service account, as which lookup-service logs in to the portal, may read the
robot credentials Secret alone.
"""

import base64
import string

from handlers.portal_credentials import (
    credentials_env,
    credentials_secrets,
    portal_credentials,
    remote_access_rbac,
)

ALPHANUMERIC = set(string.ascii_letters + string.digits)


def _generated(value):
    return len(value) == 32 and set(value) <= ALPHANUMERIC


def test_generates_credentials_for_a_portal_whose_definition_sets_none():
    credentials = portal_credentials({})

    assert credentials["admin"]["username"] == "educates"
    assert credentials["robot"]["username"] == "robot@educates"

    assert _generated(credentials["admin"]["password"])
    assert _generated(credentials["robot"]["password"])
    assert _generated(credentials["robot"]["client-id"])
    assert _generated(credentials["robot"]["client-secret"])


def test_reuses_the_values_of_credentials_secrets_that_already_exist():
    admin = _secret(
        "training-portal-admin-credentials",
        username="educates",
        password="existing-admin-password",
    )
    robot = _secret(
        "training-portal-robot-credentials",
        **{
            "username": "robot@educates",
            "password": "existing-robot-password",
            "client-id": "existing-client-id",
            "client-secret": "existing-client-secret",
        },
    )

    assert portal_credentials({}, admin_secret=admin, robot_secret=robot) == {
        "admin": {"username": "educates", "password": "existing-admin-password"},
        "robot": {
            "username": "robot@educates",
            "password": "existing-robot-password",
            "client-id": "existing-client-id",
            "client-secret": "existing-client-secret",
        },
    }


def test_generates_the_credentials_of_a_secret_that_does_not_exist_yet():
    admin = _secret(
        "training-portal-admin-credentials",
        username="educates",
        password="existing-admin-password",
    )

    credentials = portal_credentials({}, admin_secret=admin)

    assert credentials["admin"]["password"] == "existing-admin-password"
    assert _generated(credentials["robot"]["password"])
    assert _generated(credentials["robot"]["client-id"])
    assert _generated(credentials["robot"]["client-secret"])


def test_an_existing_secret_wins_over_the_definition():
    # The Secret holds what the portal was deployed with and recorded in its
    # database on first start, so it stays what the portal uses.

    spec = {"portal": {"credentials": {"admin": {"password": "changed-password"}}}}
    admin = _secret(
        "training-portal-admin-credentials",
        username="educates",
        password="existing-admin-password",
    )

    credentials = portal_credentials(spec, admin_secret=admin)

    assert credentials["admin"]["password"] == "existing-admin-password"


def _secret(name, **values):
    """A Secret as the API server returns it, its values base64 encoded."""

    return {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {"name": name, "namespace": "lab-portal-ui"},
        "type": "Opaque",
        "data": {
            key: base64.b64encode(value.encode()).decode()
            for key, value in values.items()
        },
    }


def _generated_values(credentials):
    return [
        credentials["admin"]["password"],
        credentials["robot"]["password"],
        credentials["robot"]["client-id"],
        credentials["robot"]["client-secret"],
    ]


def test_each_portal_gets_values_of_its_own():
    first = _generated_values(portal_credentials({}))
    second = _generated_values(portal_credentials({}))

    assert len(set(first + second)) == 8


def test_uses_the_credentials_the_definition_supplies():
    spec = {
        "portal": {
            "credentials": {
                "admin": {"username": "admin-user", "password": "admin-secret"},
                "robot": {"username": "robot-user", "password": "robot-secret"},
            },
            "clients": {"robot": {"id": "client-id", "secret": "client-secret"}},
        }
    }

    assert portal_credentials(spec) == {
        "admin": {"username": "admin-user", "password": "admin-secret"},
        "robot": {
            "username": "robot-user",
            "password": "robot-secret",
            "client-id": "client-id",
            "client-secret": "client-secret",
        },
    }


def test_generates_only_the_values_the_definition_leaves_out():
    spec = {"portal": {"credentials": {"admin": {"password": "admin-secret"}}}}

    credentials = portal_credentials(spec)

    assert credentials["admin"] == {"username": "educates", "password": "admin-secret"}
    assert credentials["robot"]["username"] == "robot@educates"
    assert _generated(credentials["robot"]["password"])
    assert _generated(credentials["robot"]["client-id"])
    assert _generated(credentials["robot"]["client-secret"])


CREDENTIALS = {
    "admin": {"username": "educates", "password": "admin-password"},
    "robot": {
        "username": "robot@educates",
        "password": "robot-password",
        "client-id": "robot-client-id",
        "client-secret": "robot-client-secret",
    },
}

PORTAL_LABELS = {
    "training.educates.dev/component": "portal",
    "training.educates.dev/portal.name": "lab-portal",
    "training.educates.dev/portal.uid": "1234",
}


def test_keeps_the_admin_credentials_in_a_secret_of_their_own():
    admin, _ = credentials_secrets("lab-portal", "1234", "lab-portal-ui", CREDENTIALS)

    assert admin == {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": "training-portal-admin-credentials",
            "namespace": "lab-portal-ui",
            "labels": PORTAL_LABELS,
        },
        "type": "Opaque",
        "stringData": {"username": "educates", "password": "admin-password"},
    }


def test_keeps_the_robot_credentials_and_client_in_a_secret_of_their_own():
    _, robot = credentials_secrets("lab-portal", "1234", "lab-portal-ui", CREDENTIALS)

    assert robot == {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": "training-portal-robot-credentials",
            "namespace": "lab-portal-ui",
            "labels": PORTAL_LABELS,
        },
        "type": "Opaque",
        "stringData": {
            "username": "robot@educates",
            "password": "robot-password",
            "client-id": "robot-client-id",
            "client-secret": "robot-client-secret",
        },
    }


def _from_secret(name, secret, key):
    return {"name": name, "valueFrom": {"secretKeyRef": {"name": secret, "key": key}}}


def test_the_portal_takes_its_credentials_from_the_secrets():
    admin = "training-portal-admin-credentials"
    robot = "training-portal-robot-credentials"

    assert credentials_env() == [
        _from_secret("ADMIN_USERNAME", admin, "username"),
        _from_secret("ADMIN_PASSWORD", admin, "password"),
        _from_secret("ROBOT_USERNAME", robot, "username"),
        _from_secret("ROBOT_PASSWORD", robot, "password"),
        _from_secret("ROBOT_CLIENT_ID", robot, "client-id"),
        _from_secret("ROBOT_CLIENT_SECRET", robot, "client-secret"),
    ]


def test_lets_remote_access_get_the_robot_credentials_secret_alone():
    # lookup-service reads a portal's robot credentials as the remote-access
    # service account, which must never read the portal's admin credentials.

    role, _ = remote_access_rbac("lab-portal", "1234", "lab-portal-ui", "educates")

    assert role == {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "Role",
        "metadata": {
            "name": "educates-remote-access",
            "namespace": "lab-portal-ui",
            "labels": PORTAL_LABELS,
        },
        "rules": [
            {
                "apiGroups": [""],
                "resources": ["secrets"],
                "resourceNames": ["training-portal-robot-credentials"],
                "verbs": ["get"],
            }
        ],
    }


def test_binds_the_remote_access_service_account_of_the_educates_namespace():
    _, role_binding = remote_access_rbac(
        "lab-portal", "1234", "lab-portal-ui", "educates"
    )

    assert role_binding == {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {
            "name": "educates-remote-access",
            "namespace": "lab-portal-ui",
            "labels": PORTAL_LABELS,
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "Role",
            "name": "educates-remote-access",
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": "remote-access",
                "namespace": "educates",
            }
        ],
    }
