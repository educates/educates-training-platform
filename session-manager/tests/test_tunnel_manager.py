"""Unit tests for the objects session-manager creates in a workshop
environment's namespace to run tunnel-manager.

tunnel-manager serves a network-facing proxy, so its service account holds
only what it uses: watching WorkshopSessions cluster-wide, through the
``educates:tunnel-manager`` ClusterRole, and creating events in the
environment namespace it runs in, through a Role and RoleBinding there. Its
Deployment probes its liveness endpoint, so a wedged tunnel-manager is
restarted.
"""

from handlers.tunnel_manager import (
    tunnel_manager_deployment,
    tunnel_manager_role,
    tunnel_manager_role_binding,
)

LABELS = {
    "training.educates.dev/component": "environment",
    "training.educates.dev/environment.name": "lab-w01",
}


def _deployment():
    return tunnel_manager_deployment(
        workshop_namespace="lab-w01",
        environment_name="lab-w01",
        image="registry.test/educates-tunnel-manager:4.0.0",
        image_pull_policy="IfNotPresent",
        memory="128Mi",
        ingress_domain="educates.test",
        storage_group=1,
        labels=LABELS,
    )


def _tunnel_container(deployment):
    (container,) = deployment["spec"]["template"]["spec"]["containers"]
    assert container["name"] == "tunnel"
    return container


def test_deployment_restarts_tunnel_manager_when_its_liveness_endpoint_fails():
    container = _tunnel_container(_deployment())

    assert container["livenessProbe"] == {
        "httpGet": {"path": "/healthz", "port": 8081},
        "initialDelaySeconds": 15,
        "periodSeconds": 10,
    }
    assert {"containerPort": 8081, "protocol": "TCP"} in container["ports"]


def test_deployment_runs_as_the_service_account_the_role_binding_names():
    deployment = _deployment()
    (subject,) = tunnel_manager_role_binding(
        workshop_namespace="lab-w01", labels=LABELS
    )["subjects"]

    assert deployment["metadata"]["namespace"] == subject["namespace"]
    assert deployment["spec"]["template"]["spec"]["serviceAccountName"] == (
        subject["name"]
    )


def test_role_lets_tunnel_manager_create_events_and_nothing_else():
    body = tunnel_manager_role(workshop_namespace="lab-w01", labels=LABELS)

    assert body == {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "Role",
        "metadata": {
            "name": "educates-tunnel-manager",
            "namespace": "lab-w01",
            "labels": {
                "training.educates.dev/component": "environment",
                "training.educates.dev/environment.name": "lab-w01",
            },
        },
        "rules": [
            {
                "apiGroups": [""],
                "resources": ["events"],
                "verbs": ["create"],
            }
        ],
    }


def test_role_binding_grants_the_role_to_the_tunnel_manager_service_account():
    body = tunnel_manager_role_binding(workshop_namespace="lab-w01", labels=LABELS)

    assert body == {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {
            "name": "educates-tunnel-manager",
            "namespace": "lab-w01",
            "labels": {
                "training.educates.dev/component": "environment",
                "training.educates.dev/environment.name": "lab-w01",
            },
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "Role",
            "name": "educates-tunnel-manager",
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": "tunnel-manager",
                "namespace": "lab-w01",
            }
        ],
    }
