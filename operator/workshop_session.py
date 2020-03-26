import os
import time

import kopf
import kubernetes
import kubernetes.client
import kubernetes.utils

from objects import create_from_dict

__all__ = ["workshop_session_create", "workshop_session_delete"]


_resource_budgets = {
    "small": {
        "resource-limits": {
            "kind": "LimitRange",
            "apiVersion": "v1",
            "metadata": {
                "name": "resource-limits",
                "annotations": {"resource-budget": "small"},
            },
            "spec": {
                "limits": [
                    {
                        "type": "Pod",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "1", "memory": "1Gi"},
                    },
                    {
                        "type": "Container",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "1", "memory": "1Gi"},
                        "default": {"cpu": "250m", "memory": "256Mi"},
                        "defaultRequest": {"cpu": "50m", "memory": "128Mi"},
                    },
                    {
                        "type": "PersistentVolumeClaim",
                        "min": {"storage": "1Gi"},
                        "max": {"storage": "1Gi"},
                    },
                ]
            },
        },
        "compute-resources": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources",
                "annotations": {"resource-budget": "small"},
            },
            "spec": {
                "hard": {"limits.cpu": "1", "limits.memory": "1Gi"},
                "scopes": ["NotTerminating"],
            },
        },
        "compute-resources-timebound": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources-timebound",
                "annotations": {"resource-budget": "small"},
            },
            "spec": {
                "hard": {"limits.cpu": "1", "limits.memory": "1Gi"},
                "scopes": ["Terminating"],
            },
        },
        "object-counts": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "object-counts",
                "annotations": {"resource-budget": "small"},
            },
            "spec": {
                "hard": {
                    "persistentvolumeclaims": "3",
                    "replicationcontrollers": "10",
                    "secrets": "20",
                    "services": "5",
                }
            },
        },
    },
    "medium": {
        "resource-limits": {
            "kind": "LimitRange",
            "apiVersion": "v1",
            "metadata": {
                "name": "resource-limits",
                "annotations": {"resource-budget": "medium"},
            },
            "spec": {
                "limits": [
                    {
                        "type": "Pod",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "2", "memory": "2Gi"},
                    },
                    {
                        "type": "Container",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "2", "memory": "2Gi"},
                        "default": {"cpu": "500m", "memory": "512Mi"},
                        "defaultRequest": {"cpu": "50m", "memory": "128Mi"},
                    },
                    {
                        "type": "PersistentVolumeClaim",
                        "min": {"storage": "1Gi"},
                        "max": {"storage": "5Gi"},
                    },
                ]
            },
        },
        "compute-resources": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources",
                "annotations": {"resource-budget": "medium"},
            },
            "spec": {
                "hard": {"limits.cpu": "2", "limits.memory": "2Gi"},
                "scopes": ["NotTerminating"],
            },
        },
        "compute-resources-timebound": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources-timebound",
                "annotations": {"resource-budget": "medium"},
            },
            "spec": {
                "hard": {"limits.cpu": "2", "limits.memory": "2Gi"},
                "scopes": ["Terminating"],
            },
        },
        "object-counts": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "object-counts",
                "annotations": {"resource-budget": "medium"},
            },
            "spec": {
                "hard": {
                    "persistentvolumeclaims": "6",
                    "replicationcontrollers": "15",
                    "secrets": "25",
                    "services": "10",
                }
            },
        },
    },
    "large": {
        "resource-limits": {
            "kind": "LimitRange",
            "apiVersion": "v1",
            "metadata": {
                "name": "resource-limits",
                "annotations": {"resource-budget": "large"},
            },
            "spec": {
                "limits": [
                    {
                        "type": "Pod",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "4", "memory": "4Gi"},
                    },
                    {
                        "type": "Container",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "4", "memory": "4Gi"},
                        "default": {"cpu": "500m", "memory": "1Gi"},
                        "defaultRequest": {"cpu": "50m", "memory": "128Mi"},
                    },
                    {
                        "type": "PersistentVolumeClaim",
                        "min": {"storage": "1Gi"},
                        "max": {"storage": "10Gi"},
                    },
                ]
            },
        },
        "compute-resources": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources",
                "annotations": {"resource-budget": "large"},
            },
            "spec": {
                "hard": {"limits.cpu": "4", "limits.memory": "4Gi"},
                "scopes": ["NotTerminating"],
            },
        },
        "compute-resources-timebound": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources-timebound",
                "annotations": {"resource-budget": "large"},
            },
            "spec": {
                "hard": {"limits.cpu": "4", "limits.memory": "4Gi"},
                "scopes": ["Terminating"],
            },
        },
        "object-counts": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "object-counts",
                "annotations": {"resource-budget": "large"},
            },
            "spec": {
                "hard": {
                    "persistentvolumeclaims": "12",
                    "replicationcontrollers": "25",
                    "secrets": "35",
                    "services": "20",
                }
            },
        },
    },
    "x-large": {
        "resource-limits": {
            "kind": "LimitRange",
            "apiVersion": "v1",
            "metadata": {
                "name": "resource-limits",
                "annotations": {"resource-budget": "x-large"},
            },
            "spec": {
                "limits": [
                    {
                        "type": "Pod",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "8", "memory": "8Gi"},
                    },
                    {
                        "type": "Container",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "8", "memory": "8Gi"},
                        "default": {"cpu": "500m", "memory": "2Gi"},
                        "defaultRequest": {"cpu": "50m", "memory": "128Mi"},
                    },
                    {
                        "type": "PersistentVolumeClaim",
                        "min": {"storage": "1Gi"},
                        "max": {"storage": "20Gi"},
                    },
                ]
            },
        },
        "compute-resources": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources",
                "annotations": {"resource-budget": "x-large"},
            },
            "spec": {
                "hard": {"limits.cpu": "8", "limits.memory": "8Gi"},
                "scopes": ["NotTerminating"],
            },
        },
        "compute-resources-timebound": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources-timebound",
                "annotations": {"resource-budget": "x-large"},
            },
            "spec": {
                "hard": {"limits.cpu": "8", "limits.memory": "8Gi"},
                "scopes": ["Terminating"],
            },
        },
        "object-counts": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "object-counts",
                "annotations": {"resource-budget": "x-large"},
            },
            "spec": {
                "hard": {
                    "persistentvolumeclaims": "18",
                    "replicationcontrollers": "35",
                    "secrets": "45",
                    "services": "30",
                }
            },
        },
    },
    "xx-large": {
        "resource-limits": {
            "kind": "LimitRange",
            "apiVersion": "v1",
            "metadata": {
                "name": "resource-limits",
                "annotations": {"resource-budget": "xx-large"},
            },
            "spec": {
                "limits": [
                    {
                        "type": "Pod",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "12", "memory": "12Gi"},
                    },
                    {
                        "type": "Container",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "12", "memory": "12Gi"},
                        "default": {"cpu": "500m", "memory": "2Gi"},
                        "defaultRequest": {"cpu": "50m", "memory": "128Mi"},
                    },
                    {
                        "type": "PersistentVolumeClaim",
                        "min": {"storage": "1Gi"},
                        "max": {"storage": "20Gi"},
                    },
                ]
            },
        },
        "compute-resources": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources",
                "annotations": {"resource-budget": "xx-large"},
            },
            "spec": {
                "hard": {"limits.cpu": "12", "limits.memory": "12Gi"},
                "scopes": ["NotTerminating"],
            },
        },
        "compute-resources-timebound": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources-timebound",
                "annotations": {"resource-budget": "xx-large"},
            },
            "spec": {
                "hard": {"limits.cpu": "12", "limits.memory": "12Gi"},
                "scopes": ["Terminating"],
            },
        },
        "object-counts": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "object-counts",
                "annotations": {"resource-budget": "xx-large"},
            },
            "spec": {
                "hard": {
                    "persistentvolumeclaims": "24",
                    "replicationcontrollers": "45",
                    "secrets": "55",
                    "services": "40",
                }
            },
        },
    },
    "xxx-large": {
        "resource-limits": {
            "kind": "LimitRange",
            "apiVersion": "v1",
            "metadata": {
                "name": "resource-limits",
                "annotations": {"resource-budget": "xxx-large"},
            },
            "spec": {
                "limits": [
                    {
                        "type": "Pod",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "16", "memory": "16Gi"},
                    },
                    {
                        "type": "Container",
                        "min": {"cpu": "50m", "memory": "32Mi"},
                        "max": {"cpu": "16", "memory": "16Gi"},
                        "default": {"cpu": "500m", "memory": "2Gi"},
                        "defaultRequest": {"cpu": "50m", "memory": "128Mi"},
                    },
                    {
                        "type": "PersistentVolumeClaim",
                        "min": {"storage": "1Gi"},
                        "max": {"storage": "20Gi"},
                    },
                ]
            },
        },
        "compute-resources": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources",
                "annotations": {"resource-budget": "xxx-large"},
            },
            "spec": {
                "hard": {"limits.cpu": "16", "limits.memory": "16Gi"},
                "scopes": ["NotTerminating"],
            },
        },
        "compute-resources-timebound": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "compute-resources-timebound",
                "annotations": {"resource-budget": "xxx-large"},
            },
            "spec": {
                "hard": {"limits.cpu": "16", "limits.memory": "16Gi"},
                "scopes": ["Terminating"],
            },
        },
        "object-counts": {
            "kind": "ResourceQuota",
            "apiVersion": "v1",
            "metadata": {
                "name": "object-counts",
                "annotations": {"resource-budget": "xxx-large"},
            },
            "spec": {
                "hard": {
                    "persistentvolumeclaims": "30",
                    "replicationcontrollers": "55",
                    "secrets": "65",
                    "services": "50",
                }
            },
        },
    },
}


def _setup_limits_and_quotas(
    workshop_namespace, target_namespace, service_account, role, budget
):
    core_api = kubernetes.client.CoreV1Api()
    rbac_authorization_api = kubernetes.client.RbacAuthorizationV1Api()

    # Create role binding in the namespace so the service account under
    # which the workshop environment runs can create resources in it.

    role_binding_body = {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "RoleBinding",
        "metadata": {"name": "eduk8s"},
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "ClusterRole",
            "name": role,
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": service_account,
                "namespace": workshop_namespace,
            }
        ],
    }

    rbac_authorization_api.create_namespaced_role_binding(
        namespace=target_namespace, body=role_binding_body
    )

    # Determine which limit ranges and resources quotas to be used.

    if budget != "custom":
        if budget not in _resource_budgets:
            budget = "default"
        elif not _resource_budgets[budget]:
            budget = "default"

    if budget not in ("default", "custom"):
        budget_item = _resource_budgets[budget]

        resource_limits_definition = budget_item["resource-limits"]
        compute_resources_definition = budget_item["compute-resources"]
        compute_resources_timebound_definition = budget_item[
            "compute-resources-timebound"
        ]
        object_counts_definition = budget_item["object-counts"]

    # Delete any limit ranges applied to the namespace that may conflict
    # with the limit range being applied. For the case of custom, we
    # delete any being applied but don't replace it. It is assumed that
    # the session objects for the workshop will define any limit ranges
    # and resource quotas itself.

    if budget != "default":
        limit_ranges = core_api.list_namespaced_limit_range(namespace=target_namespace)

        for limit_range in limit_ranges.items:
            core_api.delete_namespaced_limit_range(
                namespace=target_namespace, name=limit_range["metadata"]["name"]
            )

    # Create limit ranges for the namespace so any deployments will have
    # default memory/cpu min and max values.

    if budget not in ("default", "custom"):
        resource_limits_body = resource_limits_definition
        core_api.create_namespaced_limit_range(
            namespace=target_namespace, body=resource_limits_body
        )

    # Delete any resource quotas applied to the namespace that may
    # conflict with the resource quotas being applied.

    if budget != "default":
        resource_quotas = core_api.list_namespaced_resource_quota(
            namespace=target_namespace
        )

        for resource_quota in resource_quotas.items:
            core_api.delete_namespaced_resource_quota(
                namespace=target_namespace, name=resource_quota["metadata"]["name"]
            )

    # Create resource quotas for the namespace so there is a maximum for
    # what resources can be used.

    if budget not in ("default", "custom"):
        resource_quota_body = compute_resources_definition
        core_api.create_namespaced_resource_quota(
            namespace=target_namespace, body=resource_quota_body
        )

        resource_quota_body = compute_resources_timebound_definition
        core_api.create_namespaced_resource_quota(
            namespace=target_namespace, body=resource_quota_body
        )

        resource_quota_body = object_counts_definition
        core_api.create_namespaced_resource_quota(
            namespace=target_namespace, body=resource_quota_body
        )

        # Verify that the status of the resource quotas have been
        # updated. If we don't do this, then the calculated hard limits
        # may not be calculated before we start creating resources in
        # the namespace resulting in a failure. If we can't manage to
        # verify quotas after a period of, give up. This may result in a
        # subsequent failure.

        for _ in range(25):
            resource_quotas = core_api.list_namespaced_resource_quota(
                namespace=target_namespace
            )

            if not resource_quotas.items:
                break

            for resource_quota in resource_quotas.items:
                if not resource_quota.status or not resource_quota.status.hard:
                    time.sleep(0.1)
                    continue

            break


@kopf.on.create("training.eduk8s.io", "v1alpha1", "workshopsessions", id="eduk8s")
def workshop_session_create(name, spec, logger, **_):
    apps_api = kubernetes.client.AppsV1Api()
    core_api = kubernetes.client.CoreV1Api()
    custom_objects_api = kubernetes.client.CustomObjectsApi()
    extensions_api = kubernetes.client.ExtensionsV1beta1Api()
    rbac_authorization_api = kubernetes.client.RbacAuthorizationV1Api()

    # The namespace created for the session is the name of the workshop
    # namespace suffixed by the session ID. By convention this should be
    # the same as what would be used for the name of the session
    # resource definition, but we can't rely on that being the case, as
    # may be different during development and testing, so we construct
    # the name ourself.

    environment_name = spec["environment"]["name"]
    workshop_namespace = environment_name

    try:
        environment_instance = custom_objects_api.get_cluster_custom_object(
            "training.eduk8s.io", "v1alpha1", "workshopenvironments", workshop_namespace
        )
    except kubernetes.client.rest.ApiException as e:
        if e.status == 404:
            raise kopf.TemporaryError("Namespace doesn't correspond to workshop.")

    session_id = spec["session"]["id"]
    session_namespace = f"{workshop_namespace}-{session_id}"

    # We pull details of the workshop to be deployed from the status of
    # the workspace custom resource. This is a copy of the specification
    # from the custom resource for the workshop. We use a copy so we
    # aren't affected by changes in the original workshop made after the
    # workspace was created.

    if not environment_instance.get("status") or not environment_instance["status"].get(
        "eduk8s"
    ):
        raise kopf.TemporaryError("Environment for workshop not ready.")

    workshop_spec = environment_instance["status"]["eduk8s"]["workshop"]["spec"]

    # Create the primary namespace to be used for the workshop session.
    # Make the namespace for the session a child of the custom resource
    # for the session. This way the namespace will be automatically
    # deleted when the resource definition for the session is deleted
    # and we don't have to clean up anything explicitly.

    namespace_body = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {"name": session_namespace},
    }

    kopf.adopt(namespace_body)

    core_api.create_namespace(body=namespace_body)

    # Create the service account under which the workshop session
    # instance will run. This is created in the workshop namespace. As
    # with the separate namespace, make the session custom resource the
    # parent. We will do this for all objects created for the session as
    # we go along.

    service_account = f"session-{session_id}"

    service_account_body = {
        "apiVersion": "v1",
        "kind": "ServiceAccount",
        "metadata": {"name": service_account},
    }

    kopf.adopt(service_account_body)

    core_api.create_namespaced_service_account(
        namespace=workshop_namespace, body=service_account_body
    )

    # Create the rolebinding for this service account to add access to
    # the additional roles that the Kubernetes web console requires.

    cluster_role_binding_body = {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "ClusterRoleBinding",
        "metadata": {"name": f"{session_namespace}-console"},
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "ClusterRole",
            "name": f"{workshop_namespace}-console",
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "namespace": workshop_namespace,
                "name": service_account,
            }
        ],
    }

    kopf.adopt(cluster_role_binding_body)

    rbac_authorization_api.create_cluster_role_binding(body=cluster_role_binding_body)

    # Setup limit ranges and projects quotas on the primary session namespace.

    role = "admin"
    budget = "default"

    if workshop_spec.get("session"):
        role = workshop_spec["session"].get("role", role)
        budget = workshop_spec["session"].get("budget", budget)

    _setup_limits_and_quotas(
        workshop_namespace, session_namespace, service_account, role, budget,
    )

    # Calculate the hostname and domain being used. Need to do this so
    # we can later set the INGRESS_DOMAIN environment variable on the
    # deployment so that it is available in the workshop environment,
    # but also so we can use it replace variables in list of resource
    # objects being created.

    ingress_domain = os.environ.get("INGRESS_DOMAIN", "training.eduk8s.io")

    domain = spec["session"].get("domain", ingress_domain)
    hostname = f"{session_namespace}.{domain}"

    # Create any additional resource objects required for the session.
    #
    # XXX For now make the session resource definition the parent of
    # all objects. Technically should only do so for non namespaced
    # objects, or objects created in namespaces that already existed.
    # How to work out if a resource type is namespaced or not with the
    # Python Kubernetes client appears to be a bit of a hack.

    def _substitute_variables(obj):
        if isinstance(obj, str):
            obj = obj.replace("$(session_id)", session_id)
            obj = obj.replace("$(session_namespace)", session_namespace)
            obj = obj.replace("$(service_account)", service_account)
            obj = obj.replace("$(environment_name)", environment_name)
            obj = obj.replace("$(workshop_namespace)", workshop_namespace)
            obj = obj.replace("$(ingress_domain)", domain)
            obj = obj.replace("$(ingress_protocol)", "http")
            return obj
        elif isinstance(obj, dict):
            return {k: _substitute_variables(v) for k, v in obj.items()}
        elif isinstance(obj, list):
            return [_substitute_variables(v) for v in obj]
        else:
            return obj

    objects = []

    if workshop_spec.get("session"):
        objects = workshop_spec["session"].get("objects", [])

    for object_body in objects:
        kind = object_body["kind"]
        api_version = object_body["apiVersion"]

        object_body = _substitute_variables(object_body)

        if not object_body["metadata"].get("namespace"):
            object_body["metadata"]["namespace"] = session_namespace

        kopf.adopt(object_body)

        create_from_dict(object_body)

        if api_version == "v1" and kind.lower() == "namespace":
            annotations = object_body["metadata"].get("annotations", {})

            target_role = annotations.get("training.eduk8s.io/session.role", role)
            target_budget = annotations.get("training.eduk8s.io/session.budget", budget)

            secondary_namespace = object_body["metadata"]["name"]

            _setup_limits_and_quotas(
                workshop_namespace,
                secondary_namespace,
                service_account,
                target_role,
                target_budget,
            )

    # Deploy the workshop dashboard environment for the session. First
    # create a secret for the Kubernetes web console that must exist
    # otherwise it will not even start up.

    secret_body = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {"name": "kubernetes-dashboard-csrf"},
    }

    core_api.create_namespaced_secret(namespace=session_namespace, body=secret_body)

    # Next setup the deployment resource for the workshop dashboard.

    username = spec["session"].get("username", "")
    password = spec["session"].get("password", "")

    image = workshop_spec.get("image", "quay.io/eduk8s/workshop-dashboard:master")

    deployment_body = {
        "apiVersion": "apps/v1",
        "kind": "Deployment",
        "metadata": {"name": f"workshop-{session_id}"},
        "spec": {
            "replicas": 1,
            "selector": {"matchLabels": {"deployment": f"workshop-{session_id}"}},
            "strategy": {"type": "Recreate"},
            "template": {
                "metadata": {"labels": {"deployment": f"workshop-{session_id}"}},
                "spec": {
                    "serviceAccountName": service_account,
                    "securityContext": {"fsGroup": 0},
                    "containers": [
                        {
                            "name": "workshop",
                            "image": image,
                            "imagePullPolicy": "Always",
                            "resources": {
                                "requests": {"memory": "512Mi"},
                                "limits": {"memory": "512Mi"},
                            },
                            "ports": [
                                {
                                    "name": "10080-tcp",
                                    "containerPort": 10080,
                                    "protocol": "TCP",
                                }
                            ],
                            "env": [
                                {
                                    "name": "WORKSHOP_NAMESPACE",
                                    "value": workshop_namespace,
                                },
                                {
                                    "name": "SESSION_NAMESPACE",
                                    "value": session_namespace,
                                },
                                {"name": "AUTH_USERNAME", "value": username,},
                                {"name": "AUTH_PASSWORD", "value": password,},
                                {"name": "INGRESS_DOMAIN", "value": domain,},
                                {"name": "INGRESS_PROTOCOL", "value": "http",},
                            ],
                            "volumeMounts": [
                                {"name": "workshop", "mountPath": "/opt/eduk8s/config"}
                            ],
                        },
                    ],
                    "volumes": [
                        {"name": "workshop", "configMap": {"name": "workshop"},}
                    ],
                },
            },
        },
    }

    # Apply any patches for the pod specification for the deployment which
    # are specified in the workshop resource definition. This would be used
    # to set resources and setup volumes.

    deployment_patch = {}

    if workshop_spec.get("session"):
        deployment_patch = workshop_spec["session"].get("patches", {})

    def _smart_overlay_merge(target, patch):
        if isinstance(patch, dict):
            for key, value in patch.items():
                if key not in target:
                    target[key] = value
                elif type(target[key]) != type(value):
                    target[key] = value
                elif isinstance(value, (dict, list)):
                    _smart_overlay_merge(target[key], value)
                else:
                    target[key] = value
        elif isinstance(patch, list):
            for patch_item in patch:
                if isinstance(patch_item, dict) and "name" in patch_item:
                    for i, target_item in enumerate(target):
                        if (
                            isinstance(target_item, dict)
                            and target_item.get("name") == patch_item["name"]
                        ):
                            _smart_overlay_merge(target[i], patch_item)
                            break
                    else:
                        target.append(patch_item)
                else:
                    target.append(patch_item)

    if deployment_patch:
        deployment_patch = _substitute_variables(deployment_patch)

        _smart_overlay_merge(
            deployment_body["spec"]["template"]["spec"], deployment_patch
        )

    # Apply any environment variable overrides for the workshop/environment.

    def _apply_environment_patch(patch):
        if not patch:
            return

        patch = _substitute_variables(patch)

        if (
            deployment_body["spec"]["template"]["spec"]["containers"][0].get("env")
            is None
        ):
            deployment_body["spec"]["template"]["spec"]["containers"][0]["env"] = patch
        else:
            _smart_overlay_merge(
                deployment_body["spec"]["template"]["spec"]["containers"][0]["env"],
                patch,
            )

    if workshop_spec.get("session"):
        _apply_environment_patch(workshop_spec["session"].get("env", []))

    _apply_environment_patch(spec["session"].get("env", []))

    # Set environment variables to enable/disable applications and specify
    # location of content.

    applications = {}

    additional_env = []

    content = workshop_spec.get("content")

    if content:
        additional_env.append({"name": "DOWNLOAD_URL", "value": content})

    if workshop_spec.get("session"):
        applications = workshop_spec["session"].get("applications", {})

    applications_enabled = {
        "editor": False,
        "console": False,
        "slides": True,
        "terminal": True,
    }

    if applications:
        for name in ("terminal", "console", "editor", "slides"):
            if applications.get(name, {}).get("enabled", applications_enabled[name]):
                additional_env.append(
                    {"name": "ENABLE_" + name.upper(), "value": "true"}
                )
            else:
                additional_env.append(
                    {"name": "ENABLE_" + name.upper(), "value": "false"}
                )

        if applications.get("console", {}).get("vendor"):
            additional_env.append(
                {
                    "name": "CONSOLE_VENDOR",
                    "value": applications.get("console", {}).get("vendor"),
                }
            )

        if applications.get("terminal", {}).get("layout"):
            additional_env.append(
                {
                    "name": "TERMINAL_LAYOUT",
                    "value": applications.get("terminal", {}).get("layout"),
                }
            )

    _apply_environment_patch(additional_env)

    # Add in extra container for running OpenShift web console.

    if applications.get("console", {}).get("enabled", applications_enabled["console"]):
        if applications.get("console", {}).get("vendor", "") == "openshift":
            console_version = (
                applications["console"].get("openshift", {}).get("version", "4.3")
            )
            console_image = (
                applications["console"]
                .get("openshift", {})
                .get("image", f"quay.io/openshift/origin-console:{console_version}")
            )
            console_container = {
                "name": "console",
                "image": console_image,
                "command": ["/opt/bridge/bin/bridge"],
                "env": [
                    {"name": "BRIDGE_K8S_MODE", "value": "in-cluster"},
                    {"name": "BRIDGE_LISTEN", "value": "http://127.0.0.1:10087"},
                    {
                        "name": "BRIDGE_BASE_ADDRESS",
                        "value": f"http://{session_namespace}-console/",
                    },
                    {"name": "BRIDGE_PUBLIC_DIR", "value": "/opt/bridge/static"},
                    {"name": "BRIDGE_USER_AUTH", "value": "disabled"},
                    {"name": "BRIDGE_BRANDING", "value": "openshift"},
                ],
                "resources": {
                    "limits": {"memory": "128Mi"},
                    "requests": {"memory": "128Mi"},
                },
            }

            deployment_body["spec"]["template"]["spec"]["containers"].append(
                console_container
            )

    # Finally create the deployment for the workshop environment.

    kopf.adopt(deployment_body)

    apps_api.create_namespaced_deployment(
        namespace=workshop_namespace, body=deployment_body
    )

    # Create a service so that the workshop environment can be accessed.
    # This is only internal to the cluster, so port forwarding or an
    # ingress is still needed to access it from outside of the cluster.

    service_body = {
        "apiVersion": "v1",
        "kind": "Service",
        "metadata": {"name": f"workshop-{session_id}"},
        "spec": {
            "type": "ClusterIP",
            "ports": [
                {
                    "name": "10080-tcp",
                    "port": 10080,
                    "protocol": "TCP",
                    "targetPort": 10080,
                }
            ],
            "selector": {"deployment": f"workshop-{session_id}"},
        },
    }

    kopf.adopt(service_body)

    core_api.create_namespaced_service(namespace=workshop_namespace, body=service_body)

    # Create the ingress for the workshop, including any for extra named
    # named ingresses.

    ingress_rules = [
        {
            "host": hostname,
            "http": {
                "paths": [
                    {
                        "path": "/",
                        "backend": {
                            "serviceName": f"workshop-{session_id}",
                            "servicePort": 10080,
                        },
                    }
                ]
            },
        }
    ]

    ingresses = []
    ingress_hostnames = []

    applications = {}

    if workshop_spec.get("session"):
        applications = workshop_spec["session"].get("applications", {})
        ingresses = workshop_spec["session"].get("ingresses", [])

    if applications:
        if applications.get("console", {}).get("enabled", True):
            ingress_hostnames.append(f"{session_namespace}-console.{domain}")
        if applications.get("editor", {}).get("enabled", False):
            ingress_hostnames.append(f"{session_namespace}-editor.{domain}")

    for ingress in ingresses:
        ingress_hostnames.append(f"{session_namespace}-{ingress['name']}.{domain}")

    for ingress_hostname in ingress_hostnames:
        ingress_rules.append(
            {
                "host": ingress_hostname,
                "http": {
                    "paths": [
                        {
                            "path": "/",
                            "backend": {
                                "serviceName": f"workshop-{session_id}",
                                "servicePort": 10080,
                            },
                        }
                    ]
                },
            }
        )

    ingress_body = {
        "apiVersion": "extensions/v1beta1",
        "kind": "Ingress",
        "metadata": {
            "name": f"workshop-{session_id}",
            "annotations": {
                "nginx.ingress.kubernetes.io/enable-cors": "true",
                "projectcontour.io/websocket-routes": "/",
            },
        },
        "spec": {"rules": ingress_rules,},
    }

    kopf.adopt(ingress_body)

    extensions_api.create_namespaced_ingress(
        namespace=workshop_namespace, body=ingress_body
    )

    url = f"http://{hostname}"

    return {"url": url}


@kopf.on.delete("training.eduk8s.io", "v1alpha1", "workshopsessions", optional=True)
def workshop_session_delete(name, spec, logger, **_):
    # Nothing to do here at this point because the owner references will
    # ensure that everything is cleaned up appropriately.

    pass
