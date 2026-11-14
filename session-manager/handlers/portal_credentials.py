"""A training portal's admin and robot credentials.

Each training portal has its own credentials: the admin account's username
and password, and the robot account's username and password with the OAuth
client id and secret a REST API client logs in with. The TrainingPortal's
definition can reference a Secret holding the admin or robot credentials,
whose values are copied when the portal is created, or set them inline, in
deprecated fields whose values are used as given. Every other value is
generated for that portal alone, once, when the portal is created.

They are kept in two Secrets in the portal's namespace, one for the admin
credentials and one for the robot credentials, which the portal's Deployment
reads them from and the TrainingPortal's status names. The remote-access
service account, as which lookup-service reaches the portal's REST API, may
get the robot credentials Secret and no other.
"""

import base64
import secrets
import string

from .helpers import xget

ADMIN_CREDENTIALS_SECRET = "training-portal-admin-credentials"
ROBOT_CREDENTIALS_SECRET = "training-portal-robot-credentials"

REMOTE_ACCESS_ROLE = "educates-remote-access"

REMOTE_ACCESS_SERVICE_ACCOUNT = "remote-access"

CREDENTIAL_LENGTH = 32

CREDENTIAL_CHARACTERS = string.ascii_letters + string.digits

DEFAULT_ADMIN_USERNAME = "educates"
DEFAULT_ROBOT_USERNAME = "robot@educates"

# The keys of the admin and robot credentials Secrets. A Secret the
# TrainingPortal's spec references for either holds the same keys.

CREDENTIALS_KEYS = {
    "admin": ("username", "password"),
    "robot": ("username", "password", "client-id", "client-secret"),
}


def generate_credential():
    """Returns a random alphanumeric value for a password or client value."""

    return "".join(
        secrets.choice(CREDENTIAL_CHARACTERS) for _ in range(CREDENTIAL_LENGTH)
    )


def secret_values(secret):
    """Returns the decoded values of a Secret as the API server returns it,
    or no values when there is no Secret."""

    if not secret:
        return {}

    return {
        key: base64.b64decode(value).decode("utf-8")
        for key, value in (secret.get("data") or {}).items()
    }


def credentials_secret_references(spec):
    """Returns the Secrets the TrainingPortal's spec references under
    portal.credentialSecrets, each a name and namespace, keyed by the
    account whose credentials it holds, admin or robot."""

    return {
        account: reference
        for account in CREDENTIALS_KEYS
        if (reference := xget(spec, f"portal.credentialSecrets.{account}"))
    }


def referenced_secret_problem(account, reference, secret):
    """Returns why the Secret referenced for an account's credentials cannot
    be used, because it does not exist or has no value for one of the
    account's keys, or None when it can be. secret is the referenced Secret
    as the API server returns it, or None when it does not exist."""

    described = (
        f"Secret {reference['name']} in namespace {reference['namespace']},"
        f" referenced by spec.portal.credentialSecrets.{account},"
    )

    if secret is None:
        return f"{described} does not exist."

    values = secret_values(secret)

    missing = [key for key in CREDENTIALS_KEYS[account] if not values.get(key)]

    if not missing:
        return None

    if len(missing) == 1:
        return f"{described} has no value for the key {missing[0]}."

    keys = ", ".join(missing[:-1]) + f" and {missing[-1]}"

    return f"{described} has no value for the keys {keys}."


def portal_credentials(
    spec,
    admin_secret=None,
    robot_secret=None,
    referenced_admin_secret=None,
    referenced_robot_secret=None,
):
    """Returns the admin and robot credentials for a training portal, keyed
    as the keys of the Secrets that hold them.

    When the portal's admin or robot credentials Secret already exists, its
    values are the ones the portal was deployed with, so they are used as
    they are. Otherwise the values of the Secret the TrainingPortal's spec
    references under portal.credentialSecrets are used, read from the same
    keys. Failing both, a value the spec sets inline, and does not leave
    empty, is used as given, and any other value is generated."""

    existing_admin = secret_values(admin_secret)
    existing_robot = secret_values(robot_secret)

    referenced_admin = secret_values(referenced_admin_secret)
    referenced_robot = secret_values(referenced_robot_secret)

    def value(existing, referenced, key, path, default):
        return existing.get(key) or referenced.get(key) or xget(spec, path) or default()

    def admin(key, path, default):
        return value(existing_admin, referenced_admin, key, path, default)

    def robot(key, path, default):
        return value(existing_robot, referenced_robot, key, path, default)

    return {
        "admin": {
            "username": admin(
                "username",
                "portal.credentials.admin.username",
                lambda: DEFAULT_ADMIN_USERNAME,
            ),
            "password": admin(
                "password", "portal.credentials.admin.password", generate_credential
            ),
        },
        "robot": {
            "username": robot(
                "username",
                "portal.credentials.robot.username",
                lambda: DEFAULT_ROBOT_USERNAME,
            ),
            "password": robot(
                "password", "portal.credentials.robot.password", generate_credential
            ),
            "client-id": robot(
                "client-id", "portal.clients.robot.id", generate_credential
            ),
            "client-secret": robot(
                "client-secret", "portal.clients.robot.secret", generate_credential
            ),
        },
    }


def credentials_secrets(portal_name, portal_uid, portal_namespace, credentials):
    """Returns the bodies of the admin and robot credentials Secrets for a
    training portal, holding the credentials portal_credentials() returns."""

    def secret(name, values):
        return {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {
                "name": name,
                "namespace": portal_namespace,
                "labels": {
                    "training.educates.dev/component": "portal",
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                },
            },
            "type": "Opaque",
            "stringData": dict(values),
        }

    return (
        secret(ADMIN_CREDENTIALS_SECRET, credentials["admin"]),
        secret(ROBOT_CREDENTIALS_SECRET, credentials["robot"]),
    )


def remote_access_rbac(portal_name, portal_uid, portal_namespace, educates_namespace):
    """Returns the bodies of the Role and RoleBinding in a training portal's
    namespace which let the remote-access service account of the Educates
    namespace get the portal's robot credentials Secret, by name, and no
    other Secret. lookup-service reads a portal's robot credentials as that
    account, so the portal's admin credentials stay out of its reach."""

    labels = {
        "training.educates.dev/component": "portal",
        "training.educates.dev/portal.name": portal_name,
        "training.educates.dev/portal.uid": portal_uid,
    }

    role = {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "Role",
        "metadata": {
            "name": REMOTE_ACCESS_ROLE,
            "namespace": portal_namespace,
            "labels": dict(labels),
        },
        "rules": [
            {
                "apiGroups": [""],
                "resources": ["secrets"],
                "resourceNames": [ROBOT_CREDENTIALS_SECRET],
                "verbs": ["get"],
            }
        ],
    }

    role_binding = {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {
            "name": REMOTE_ACCESS_ROLE,
            "namespace": portal_namespace,
            "labels": dict(labels),
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "Role",
            "name": REMOTE_ACCESS_ROLE,
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": REMOTE_ACCESS_SERVICE_ACCOUNT,
                "namespace": educates_namespace,
            }
        ],
    }

    return role, role_binding


def credentials_env():
    """Returns the environment entries of the portal container that take the
    portal's credentials from its credentials Secrets."""

    def from_secret(name, secret, key):
        return {
            "name": name,
            "valueFrom": {"secretKeyRef": {"name": secret, "key": key}},
        }

    return [
        from_secret("ADMIN_USERNAME", ADMIN_CREDENTIALS_SECRET, "username"),
        from_secret("ADMIN_PASSWORD", ADMIN_CREDENTIALS_SECRET, "password"),
        from_secret("ROBOT_USERNAME", ROBOT_CREDENTIALS_SECRET, "username"),
        from_secret("ROBOT_PASSWORD", ROBOT_CREDENTIALS_SECRET, "password"),
        from_secret("ROBOT_CLIENT_ID", ROBOT_CREDENTIALS_SECRET, "client-id"),
        from_secret("ROBOT_CLIENT_SECRET", ROBOT_CREDENTIALS_SECRET, "client-secret"),
    ]
