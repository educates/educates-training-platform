"""Unit tests for the objects session-manager creates in a workshop
environment namespace for the training portal that owns the environment.

A training portal reads no Secrets. It creates the Secrets carrying a
session's config password, user details and request parameters in its own
workshop environment namespaces only, through a RoleBinding in each of them
to a ClusterRole granting ``create`` on Secrets alone.
"""

from handlers.portal_access import portal_environment_role_binding

LABELS = {
    "training.educates.dev/component": "environment",
    "training.educates.dev/environment.name": "event-w01",
}


def _environment(name, portal=None):
    labels = {}

    if portal is not None:
        labels["training.educates.dev/portal.name"] = portal

    return {
        "apiVersion": "training.educates.dev/v1beta1",
        "kind": "WorkshopEnvironment",
        "metadata": {"name": name, "labels": labels},
        "spec": {"workshop": {"name": "lab-k8s-fundamentals"}},
    }


def test_binds_the_portal_service_account_to_create_secrets_in_its_environment():
    body = portal_environment_role_binding(
        _environment("event-w01", portal="event"), labels=LABELS
    )

    assert body == {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {
            "name": "educates-training-portal",
            "namespace": "event-w01",
            "labels": {
                "training.educates.dev/component": "environment",
                "training.educates.dev/environment.name": "event-w01",
            },
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "ClusterRole",
            "name": "educates:training-portal:environment",
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": "training-portal",
                "namespace": "event-ui",
            }
        ],
    }


def test_gives_no_binding_in_an_environment_without_a_portal_label():
    environment = _environment("lab-k8s-fundamentals-w01")

    assert portal_environment_role_binding(environment, labels=LABELS) is None


def test_gives_no_binding_in_an_environment_with_an_empty_portal_label():
    environment = _environment("lab-k8s-fundamentals-w01", portal="")

    assert portal_environment_role_binding(environment, labels=LABELS) is None
