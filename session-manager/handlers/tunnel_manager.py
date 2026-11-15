"""Objects session-manager creates in a workshop environment's namespace to
run tunnel-manager, the proxy that carries SSH connections to the
environment's workshop sessions over a websocket.

tunnel-manager serves a network-facing proxy, so its service account holds
only what it uses. Cluster-wide, the `educates:tunnel-manager` ClusterRole
lets it get, list and watch WorkshopSessions. In the environment namespace,
where it runs and where it posts its events about WorkshopSessions, the
`educates-tunnel-manager` Role lets it create events.

tunnel-manager serves its kopf liveness endpoint, whose probe proves the
Kubernetes API still accepts its token, and its Deployment probes that
endpoint, so a wedged tunnel-manager is restarted.
"""

from .role_bindings import service_account_role_binding

TUNNEL_MANAGER_SERVICE_ACCOUNT = "tunnel-manager"

TUNNEL_MANAGER_ROLE = "educates-tunnel-manager"

TUNNEL_MANAGER_PROXY_PORT = 8080

TUNNEL_MANAGER_LIVENESS_PORT = 8081


def tunnel_manager_deployment(
    workshop_namespace,
    environment_name,
    image,
    image_pull_policy,
    memory,
    ingress_domain,
    storage_group,
    labels,
):
    """Returns the Deployment which runs tunnel-manager as its own service
    account in the environment namespace, restarting it when its liveness
    endpoint fails.

    """

    return {
        "apiVersion": "apps/v1",
        "kind": "Deployment",
        "metadata": {
            "namespace": workshop_namespace,
            "name": "tunnel-manager",
            "labels": dict(labels),
        },
        "spec": {
            "replicas": 1,
            "selector": {"matchLabels": {"deployment": "tunnel-manager"}},
            "strategy": {"type": "Recreate"},
            "template": {
                "metadata": {
                    "labels": {"deployment": "tunnel-manager", **labels},
                },
                "spec": {
                    "serviceAccountName": TUNNEL_MANAGER_SERVICE_ACCOUNT,
                    "containers": [
                        {
                            "name": "tunnel",
                            "image": image,
                            "imagePullPolicy": image_pull_policy,
                            "securityContext": {
                                "allowPrivilegeEscalation": False,
                                "capabilities": {"drop": ["ALL"]},
                                "runAsNonRoot": True,
                                # "seccompProfile": {"type": "RuntimeDefault"},
                            },
                            "resources": {
                                "limits": {"memory": memory},
                                "requests": {"memory": memory},
                            },
                            "ports": [
                                {
                                    "containerPort": TUNNEL_MANAGER_PROXY_PORT,
                                    "protocol": "TCP",
                                },
                                {
                                    "containerPort": TUNNEL_MANAGER_LIVENESS_PORT,
                                    "protocol": "TCP",
                                },
                            ],
                            "livenessProbe": {
                                "httpGet": {
                                    "path": "/healthz",
                                    "port": TUNNEL_MANAGER_LIVENESS_PORT,
                                },
                                "initialDelaySeconds": 15,
                                "periodSeconds": 10,
                            },
                            "env": [
                                {"name": "INGRESS_DOMAIN", "value": ingress_domain},
                                {
                                    "name": "ENVIRONMENT_NAME",
                                    "value": environment_name,
                                },
                            ],
                        }
                    ],
                    "securityContext": {
                        "runAsUser": 1001,
                        "fsGroup": storage_group,
                        "supplementalGroups": [storage_group],
                    },
                },
            },
        },
    }


def tunnel_manager_role(workshop_namespace, labels):
    """Returns the Role which lets tunnel-manager create the events it posts
    about WorkshopSessions in the environment namespace it runs in.

    """

    return {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "Role",
        "metadata": {
            "name": TUNNEL_MANAGER_ROLE,
            "namespace": workshop_namespace,
            "labels": dict(labels),
        },
        "rules": [
            {
                "apiGroups": [""],
                "resources": ["events"],
                "verbs": ["create"],
            }
        ],
    }


def tunnel_manager_role_binding(workshop_namespace, labels):
    """Returns the RoleBinding which grants the events Role to tunnel-manager's
    service account in the environment namespace.

    """

    return service_account_role_binding(
        name=TUNNEL_MANAGER_ROLE,
        namespace=workshop_namespace,
        role_kind="Role",
        role_name=TUNNEL_MANAGER_ROLE,
        service_account=TUNNEL_MANAGER_SERVICE_ACCOUNT,
        service_account_namespace=workshop_namespace,
        labels=labels,
    )
