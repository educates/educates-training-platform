import logging

import kopf
import pykube

from .analytics import report_analytics_event
from .helpers import image_pull_policy, resource_owned_by, xget
from .objects import SecretCopier
from .operator_config import (AMPLITUDE_TRACKING_ID, ANALYTICS_WEBHOOK_URL,
                              CLARITY_TRACKING_ID,
                              CLUSTER_SECURITY_POLICY_ENGINE,
                              CLUSTER_STORAGE_CLASS, CLUSTER_STORAGE_GROUP,
                              CLUSTER_STORAGE_USER, DEFAULT_THEME_NAME,
                              FRAME_ANCESTORS, GOOGLE_TRACKING_ID,
                              INGRESS_CA_SECRET, INGRESS_CLASS, INGRESS_DOMAIN,
                              INGRESS_PROTOCOL, INGRESS_SECRET,
                              OPERATOR_NAMESPACE, PORTAL_ADMIN_PASSWORD,
                              PORTAL_ADMIN_USERNAME, PORTAL_ROBOT_CLIENT_ID,
                              PORTAL_ROBOT_CLIENT_SECRET,
                              PORTAL_ROBOT_PASSWORD, PORTAL_ROBOT_USERNAME,
                              SESSION_COOKIE_DOMAIN, TRAINING_PORTAL_IMAGE)

__all__ = ["training_portal_create", "training_portal_delete"]

logger = logging.getLogger("educates.trainingportal")

api = pykube.HTTPClient(pykube.KubeConfig.from_env())


@kopf.on.resume(
    "training.educates.dev",
    "v1beta1",
    "trainingportals",
)
def training_portal_resume(name, **_):
    """Used to acknowledge that there was an existing training portal resource
    found when the operator started up."""

    logger.info(
        "Training portal %s has been found but was previously processed.",
        name,
    )


@kopf.on.create(
    "training.educates.dev",
    "v1beta1",
    "trainingportals",
    timeout=900,
)
def training_portal_create(name, uid, body, spec, status, patch, runtime, retry, **_):
    """Handle creation of a training portal resource. This involves the
    deployment of the training portal instance which the manages access to
    workshop environments and create workshop sessions."""

    report_analytics_event(
        "Resource/Create",
        {"kind": "TrainingPortal", "name": name, "uid": uid, "retry": retry},
    )

    if retry > 0:
        logger.info(
            "Training portal creation request for %s being retried, retries %d.",
            name,
            retry,
        )
    else:
        logger.info("Training portal creation request for %s being processed.", name)

    # Calculate name for the portal namespace.

    portal_name = name
    portal_uid = uid

    portal_namespace = f"{portal_name}-ui"

    # Calculate access details for the portal. The hostname used to access the
    # portal can be overridden, but the namespace above is always the same.

    ingress_hostname = xget(spec, "portal.ingress.hostname")

    if not ingress_hostname:
        portal_hostname = f"{portal_name}-ui.{INGRESS_DOMAIN}"
    elif not "." in ingress_hostname:
        portal_hostname = f"{ingress_hostname}.{INGRESS_DOMAIN}"
    else:
        # If a FQDN is supplied it must match the wildcard ingress domain when
        # HTTPS is being used and relying on global wildcard certificate. A
        # distinct FQDN not matching the wildcard ingress domain can only be
        # supplied if a TLS certificate reference is supplied in the training
        # portal definition itself. This distinct FQDN must still share a common
        # parent domain at some point with the wildcard ingress domain because
        # of constraints around cross domain cookies.

        portal_hostname = ingress_hostname

    portal_url = f"{INGRESS_PROTOCOL}://{portal_hostname}"

    # Calculate admin password and api credentials for portal management.

    admin_username = xget(
        spec, "portal.credentials.admin.username", PORTAL_ADMIN_USERNAME
    )
    admin_password = xget(
        spec, "portal.credentials.admin.password", PORTAL_ADMIN_PASSWORD
    )

    robot_username = xget(
        spec, "portal.credentials.robot.username", PORTAL_ROBOT_USERNAME
    )
    robot_password = xget(
        spec, "portal.credentials.robot.password", PORTAL_ROBOT_PASSWORD
    )

    robot_client_id = xget(spec, "portal.clients.robot.id", PORTAL_ROBOT_CLIENT_ID)
    robot_client_secret = xget(
        spec, "portal.clients.robot.secret", PORTAL_ROBOT_CLIENT_SECRET
    )

    # Calculate settings for portal web interface.

    portal_title = xget(spec, "portal.title", "Workshops")
    portal_password = xget(spec, "portal.password", "")
    portal_index = xget(spec, "portal.index", "")
    portal_logo = xget(spec, "portal.logo", "")

    theme_name = xget(spec, "portal.theme.name", DEFAULT_THEME_NAME)

    if not theme_name:
        theme_name = "default-website-theme"

    frame_ancestors = ",".join(
        xget(spec, "portal.theme.frame.ancestors", FRAME_ANCESTORS)
    )

    cookie_domain = xget(spec, "portal.cookies.domain")

    if not cookie_domain:
        cookie_domain = SESSION_COOKIE_DOMAIN

    registration_type = xget(spec, "portal.registration.type", "one-step")
    enable_registration = str(xget(spec, "portal.registration.enabled", True)).lower()

    catalog_visibility = xget(spec, "portal.catalog.visibility", "private")

    google_tracking_id = xget(spec, "analytics.google.trackingId", GOOGLE_TRACKING_ID)
    clarity_tracking_id = xget(
        spec, "analytics.clarity.trackingId", CLARITY_TRACKING_ID
    )
    amplitude_tracking_id = xget(
        spec, "analytics.amplitude.trackingId", AMPLITUDE_TRACKING_ID
    )

    analytics_webhook_url = xget(spec, "analytics.webhook.url", ANALYTICS_WEBHOOK_URL)

    # Create the namespace for holding the training portal. Before we attempt to
    # create the namespace, we first see whether it may already exist. This
    # could be because a prior namespace hadn't yet been deleted, or we failed
    # on a prior attempt to create the training portal some point after the
    # namespace had been created but before all other resources could be
    # created.

    try:
        namespace_instance = pykube.Namespace.objects(api).get(name=portal_namespace)

    except pykube.exceptions.ObjectDoesNotExist:
        # Namespace doesn't exist so we should be all okay to continue.

        pass

    except pykube.exceptions.KubernetesError as exc:
        logger.exception("Unexpected error querying namespace %s.", portal_namespace)

        patch["status"] = {
            "educates": {
                "phase": "Unknown",
                "message": f"Unexpected error querying namespace {portal_namespace}.",
            }
        }

        report_analytics_event(
            "Resource/TemporaryError",
            {
                "kind": "TrainingPortal",
                "name": name,
                "uid": uid,
                "retry": retry,
                "message": f"Unexpected error querying namespace {portal_namespace}.",
            },
        )

        raise kopf.TemporaryError(
            f"Unexpected error querying namespace {portal_namespace}.", delay=30
        ) from exc

    else:
        # The namespace already exists. We need to check whether it is owned by
        # this training portal instance.

        if not resource_owned_by(namespace_instance.obj, body):
            # Namespace is owned by another party so we flag a transient error
            # and will check again later to give time for the namespace to be
            # deleted.

            if runtime.total_seconds() >= 300:
                patch["status"] = {
                    "educates": {
                        "phase": "Failed",
                        "message": f"Namespace {portal_namespace} already exists.",
                    }
                }

                report_analytics_event(
                    "Resource/PermanentError",
                    {
                        "kind": "TrainingPortal",
                        "name": name,
                        "uid": uid,
                        "retry": retry,
                        "message": f"Namespace {portal_namespace} already exists.",
                    },
                )

                raise kopf.PermanentError(
                    f"Namespace {portal_namespace} required for training portal {portal_name} already exists."
                )

            else:
                patch["status"] = {
                    "educates": {
                        "phase": "Pending",
                        "message": f"Namespace {portal_namespace} already exists.",
                    }
                }

                report_analytics_event(
                    "Resource/TemporaryError",
                    {
                        "kind": "TrainingPortal",
                        "name": name,
                        "uid": uid,
                        "retry": retry,
                        "message": f"Namespace {portal_namespace} already exists.",
                    },
                )

                raise kopf.TemporaryError(
                    f"Namespace {portal_namespace} required for training portal {portal_name} already exists.",
                    delay=30,
                )

        else:
            # We own the namespace so verify that our current state indicates we
            # previously had an error and want to retry. In this case we will
            # delete the namespace and flag a transient error again.

            phase = xget(status, "educates.phase")

            if phase == "Retrying":
                if runtime.total_seconds() >= 300:
                    patch["status"] = {
                        "educates": {
                            "phase": "Failed",
                            "message": f"Unable to setup training portal {name}.",
                        }
                    }

                    report_analytics_event(
                        "Resource/TemporaryError",
                        {
                            "kind": "TrainingPortal",
                            "name": name,
                            "uid": uid,
                            "retry": retry,
                            "message": f"Unable to setup training portal {name}.",
                        },
                    )

                    raise kopf.PermanentError(
                        f"Unable to setup training portal {name}."
                    )

                else:
                    try:
                        namespace_instance.delete()

                    except pykube.exceptions.KubernetesError as exc:
                        logger.exception(
                            "Unexpected error deleting namespace %s requires by training portal %s.",
                            portal_namespace,
                            portal_name,
                        )

                        patch["status"] = {
                            "educates": {
                                "phase": "Retrying",
                                "message": f"Failed to delete namespace {portal_namespace}.",
                            }
                        }

                        report_analytics_event(
                            "Resource/TemporaryError",
                            {
                                "kind": "TrainingPortal",
                                "name": name,
                                "uid": uid,
                                "retry": retry,
                                "message": f"Failed to delete namespace {portal_namespace}.",
                            },
                        )

                        raise kopf.TemporaryError(
                            f"Failed to delete namespace {portal_namespace} required by training portal {portal_name}.",
                            delay=30,
                        ) from exc

                    patch["status"] = {
                        "educates": {
                            "phase": "Retrying",
                            "message": f"Deleting {portal_namespace} and retrying.",
                        }
                    }

                    report_analytics_event(
                        "Resource/TemporaryError",
                        {
                            "kind": "TrainingPortal",
                            "name": name,
                            "uid": uid,
                            "retry": retry,
                            "message": f"Deleting {portal_namespace} and retrying.",
                        },
                    )

                    raise kopf.TemporaryError(
                        f"Deleting {portal_namespace} and retrying.", delay=30
                    )

            else:
                patch["status"] = {
                    "educates": {
                        "phase": "Unknown",
                        "message": f"Training portal {portal_name} in unexpected state {phase}.",
                    }
                }

                report_analytics_event(
                    "Resource/TemporaryError",
                    {
                        "kind": "TrainingPortal",
                        "name": name,
                        "uid": uid,
                        "retry": retry,
                        "message": f"Training portal {portal_name} in unexpected state {phase}.",
                    },
                )

                raise kopf.TemporaryError(
                    f"Training portal {portal_name} in unexpected state {phase}.",
                    delay=30,
                )

    # Namespace doesn't already exist so we need to create it. We query back
    # the namespace immediately so we can access its unique uid. Note that we
    # set the owner of the namespace to be the training portal so deletion of
    # the training portal results in its deletion, but anything else we create
    # which is not namespaced has the namespace set as the owner.

    namespace_body = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {
            "name": portal_namespace,
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/policy.engine": CLUSTER_SECURITY_POLICY_ENGINE,
                "training.educates.dev/policy.name": "baseline",
            },
            "annotations": {"secretgen.carvel.dev/excluded-from-wildcard-matching": ""},
        },
    }

    if CLUSTER_SECURITY_POLICY_ENGINE == "pod-security-standards":
        namespace_body["metadata"]["labels"][
            "pod-security.kubernetes.io/enforce"
        ] = "baseline"

    kopf.adopt(namespace_body)

    try:
        pykube.Namespace(api, namespace_body).create()

        namespace_instance = pykube.Namespace.objects(api).get(name=portal_namespace)

    except pykube.exceptions.KubernetesError as exc:
        logger.exception("Unexpected error creating namespace %s.", portal_namespace)

        patch["status"] = {
            "educates": {
                "phase": "Retrying",
                "message": f"Failed to create namespace {portal_namespace}.",
            }
        }

        report_analytics_event(
            "Resource/TemporaryError",
            {
                "kind": "TrainingPortal",
                "name": name,
                "uid": uid,
                "retry": retry,
                "message": f"Failed to create namespace {portal_namespace}.",
            },
        )

        raise kopf.TemporaryError(
            f"Failed to create namespace {portal_namespace}.", delay=30
        ) from exc

    # Apply security policies to whole namespace if enabled.

    if CLUSTER_SECURITY_POLICY_ENGINE == "pod-security-policies":
        psp_role_binding_body = {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "RoleBinding",
            "metadata": {
                "name": "educates-security-policy",
                "namespace": portal_namespace,
                "labels": {
                    "training.educates.dev/component": "portal",
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                },
            },
            "roleRef": {
                "apiGroup": "rbac.authorization.k8s.io",
                "kind": "ClusterRole",
                "name": "educates-baseline-psp",
            },
            "subjects": [
                {
                    "apiGroup": "rbac.authorization.k8s.io",
                    "kind": "Group",
                    "name": f"system:serviceaccounts:{portal_namespace}",
                }
            ],
        }

        pykube.RoleBinding(api, psp_role_binding_body).create()

    if CLUSTER_SECURITY_POLICY_ENGINE == "security-context-constraints":
        scc_role_binding_body = {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "RoleBinding",
            "metadata": {
                "name": "educates-security-policy",
                "namespace": portal_namespace,
                "labels": {
                    "training.educates.dev/component": "portal",
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                },
            },
            "roleRef": {
                "apiGroup": "rbac.authorization.k8s.io",
                "kind": "ClusterRole",
                "name": "educates-baseline-scc",
            },
            "subjects": [
                {
                    "apiGroup": "rbac.authorization.k8s.io",
                    "kind": "Group",
                    "name": f"system:serviceaccounts:{portal_namespace}",
                }
            ],
        }

        pykube.RoleBinding(api, scc_role_binding_body).create()

    # Delete any limit ranges applied to the namespace so they don't cause
    # issues with deploying the training portal. This can be an issue where
    # namespace/project templates apply them automatically to a namespace. The
    # problem is that we may do this query too quickly and they may not have
    # been created as yet.

    for limit_range in pykube.LimitRange.objects(api, namespace=portal_namespace).all():
        try:
            limit_range.delete()
        except pykube.exceptions.ObjectDoesNotExist:
            pass

    # Delete any resource quotas applied to the namespace so they don't cause
    # issues with deploying the training portal. This can be an issue where
    # namespace/project templates apply them automatically to a namespace. The
    # problem is that we may do this query too quickly and they may not have
    # been created as yet.

    for resource_quota in pykube.ResourceQuota.objects(
        api, namespace=portal_namespace
    ).all():
        try:
            resource_quota.delete()
        except pykube.exceptions.ObjectDoesNotExist:
            pass

    # Prepare all the resources required for the training portal web interface.
    # First up need to create a service account and bind required roles to it.
    # Note that we set the owner of the cluster role binding to be the namespace
    # so that deletion of the namespace results in its deletion.

    service_account_body = {
        "apiVersion": "v1",
        "kind": "ServiceAccount",
        "metadata": {
            "name": "training-portal",
            "namespace": portal_namespace,
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
            },
        },
    }

    service_account_token_body = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": "training-portal-token",
            "namespace": portal_namespace,
            "annotations": {"kubernetes.io/service-account.name": "training-portal"},
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
            },
        },
        "type": "kubernetes.io/service-account-token",
    }

    cluster_role_binding_body = {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "ClusterRoleBinding",
        "metadata": {
            "name": f"educates-training-portal-{portal_namespace}",
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
            },
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "ClusterRole",
            "name": "educates-training-portal",
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "name": "training-portal",
                "namespace": portal_namespace,
            }
        ],
    }

    kopf.adopt(cluster_role_binding_body, namespace_instance.obj)

    persistent_volume_claim_body = {
        "apiVersion": "v1",
        "kind": "PersistentVolumeClaim",
        "metadata": {
            "name": "training-portal",
            "namespace": portal_namespace,
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
            },
        },
        "spec": {
            "accessModes": ["ReadWriteOnce"],
            "resources": {"requests": {"storage": "1Gi"}},
        },
    }

    if CLUSTER_STORAGE_CLASS:
        persistent_volume_claim_body["spec"]["storageClassName"] = CLUSTER_STORAGE_CLASS

    config_map_body = {
        "apiVersion": "v1",
        "kind": "ConfigMap",
        "metadata": {
            "name": "training-portal",
            "namespace": portal_namespace,
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
            },
        },
        "data": {
            "logo": portal_logo,
        },
    }

    deployment_body = {
        "apiVersion": "apps/v1",
        "kind": "Deployment",
        "metadata": {
            "name": "training-portal",
            "namespace": portal_namespace,
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/portal.services.dashboard": "true",
            },
        },
        "spec": {
            "replicas": 1,
            "selector": {"matchLabels": {"deployment": "training-portal"}},
            "strategy": {"type": "Recreate"},
            "template": {
                "metadata": {
                    "labels": {
                        "deployment": "training-portal",
                        "training.educates.dev/component": "portal",
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/portal.services.dashboard": "true",
                    },
                },
                "spec": {
                    "serviceAccountName": "training-portal",
                    "automountServiceAccountToken": False,
                    "securityContext": {
                        "runAsUser": 1001,
                        "fsGroup": CLUSTER_STORAGE_GROUP,
                        "supplementalGroups": [CLUSTER_STORAGE_GROUP],
                    },
                    "containers": [
                        {
                            "name": "portal",
                            "image": TRAINING_PORTAL_IMAGE,
                            "imagePullPolicy": image_pull_policy(TRAINING_PORTAL_IMAGE),
                            "securityContext": {
                                "allowPrivilegeEscalation": False,
                                "capabilities": {"drop": ["ALL"]},
                                "runAsNonRoot": True,
                                # "seccompProfile": {"type": "RuntimeDefault"},
                            },
                            "resources": {
                                "requests": {"memory": "256Mi"},
                                "limits": {"memory": "256Mi"},
                            },
                            "ports": [
                                {"containerPort": 8080, "protocol": "TCP"},
                                {"containerPort": 8081, "protocol": "TCP"},
                            ],
                            "readinessProbe": {
                                "httpGet": {"path": "/healthz", "port": 8081},
                                "initialDelaySeconds": 10,
                                "periodSeconds": 10,
                            },
                            "livenessProbe": {
                                "httpGet": {"path": "/healthz", "port": 8081},
                                "initialDelaySeconds": 15,
                                "periodSeconds": 10,
                            },
                            "env": [
                                {
                                    "name": "TRAINING_PORTAL",
                                    "value": portal_name,
                                },
                                {
                                    "name": "PORTAL_UID",
                                    "value": uid,
                                },
                                {
                                    "name": "PORTAL_HOSTNAME",
                                    "value": portal_hostname,
                                },
                                {
                                    "name": "PORTAL_TITLE",
                                    "value": portal_title,
                                },
                                {
                                    "name": "PORTAL_PASSWORD",
                                    "value": portal_password,
                                },
                                {
                                    "name": "PORTAL_INDEX",
                                    "value": portal_index,
                                },
                                {
                                    "name": "THEME_NAME",
                                    "value": theme_name,
                                },
                                {
                                    "name": "FRAME_ANCESTORS",
                                    "value": frame_ancestors,
                                },
                                {
                                    "name": "ADMIN_USERNAME",
                                    "value": admin_username,
                                },
                                {
                                    "name": "ADMIN_PASSWORD",
                                    "value": admin_password,
                                },
                                {
                                    "name": "INGRESS_DOMAIN",
                                    "value": INGRESS_DOMAIN,
                                },
                                {
                                    "name": "SESSION_COOKIE_DOMAIN",
                                    "value": cookie_domain,
                                },
                                {
                                    "name": "REGISTRATION_TYPE",
                                    "value": registration_type,
                                },
                                {
                                    "name": "ENABLE_REGISTRATION",
                                    "value": enable_registration,
                                },
                                {
                                    "name": "CATALOG_VISIBILITY",
                                    "value": catalog_visibility,
                                },
                                {
                                    "name": "INGRESS_CLASS",
                                    "value": INGRESS_CLASS,
                                },
                                {
                                    "name": "INGRESS_PROTOCOL",
                                    "value": INGRESS_PROTOCOL,
                                },
                                {
                                    "name": "INGRESS_SECRET",
                                    "value": INGRESS_SECRET,
                                },
                                {
                                    "name": "GOOGLE_TRACKING_ID",
                                    "value": google_tracking_id,
                                },
                                {
                                    "name": "CLARITY_TRACKING_ID",
                                    "value": clarity_tracking_id,
                                },
                                {
                                    "name": "AMPLITUDE_TRACKING_ID",
                                    "value": amplitude_tracking_id,
                                },
                                {
                                    "name": "ANALYTICS_WEBHOOK_URL",
                                    "value": analytics_webhook_url,
                                },
                            ],
                            "volumeMounts": [
                                {"name": "data", "mountPath": "/opt/app-root/data"},
                                {"name": "config", "mountPath": "/opt/app-root/config"},
                                {
                                    "name": "theme",
                                    "mountPath": "/opt/app-root/static/theme",
                                },
                                {
                                    "name": "token",
                                    "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                                    "readOnly": True,
                                },
                            ],
                        }
                    ],
                    "volumes": [
                        {
                            "name": "data",
                            "persistentVolumeClaim": {"claimName": "training-portal"},
                        },
                        {
                            "name": "config",
                            "configMap": {"name": "training-portal"},
                        },
                        {
                            "name": "theme",
                            "secret": {"secretName": theme_name},
                        },
                        {
                            "name": "token",
                            "secret": {"secretName": "training-portal-token"},
                        },
                    ],
                },
            },
        },
    }

    if CLUSTER_STORAGE_USER:
        # This hack is to cope with Kubernetes clusters which don't properly set
        # up persistent volume ownership. IBM Kubernetes is one example. The
        # init container runs as root and sets permissions on the storage and
        # ensures it is group writable. Note that this will only work where pod
        # security policies are not enforced. Don't attempt to use it if they
        # are. If they are, this hack should not be required.

        storage_init_container = {
            "name": "storage-permissions-initialization",
            "image": TRAINING_PORTAL_IMAGE,
            "imagePullPolicy": image_pull_policy(TRAINING_PORTAL_IMAGE),
            "securityContext": {"runAsUser": 0},
            "command": ["/bin/sh", "-c"],
            "args": [
                f"chown {CLUSTER_STORAGE_USER}:{CLUSTER_STORAGE_GROUP} /mnt && chmod og+rwx /mnt"
            ],
            "resources": {
                "requests": {"memory": "256Mi"},
                "limits": {"memory": "256Mi"},
            },
            "volumeMounts": [{"name": "data", "mountPath": "/mnt"}],
        }

        deployment_body["spec"]["template"]["spec"]["initContainers"] = [
            storage_init_container
        ]

    if INGRESS_SECRET and INGRESS_CA_SECRET:
            deployment_body["spec"]["template"]["spec"]["volumes"].append(
                {
                    "name": "ingress-ca",
                    "secret": {"secretName": INGRESS_CA_SECRET},
                }
            )
            deployment_body["spec"]["template"]["spec"]["containers"][0]["volumeMounts"].append(
                {
                    "name": "ingress-ca",
                    "mountPath": "/opt/app-root/certs/ingress-ca",
                    "readOnly": True,
                }
            )

    service_body = {
        "apiVersion": "v1",
        "kind": "Service",
        "metadata": {
            "name": "training-portal",
            "namespace": portal_namespace,
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
            },
        },
        "spec": {
            "type": "ClusterIP",
            "ports": [{"port": 80, "protocol": "TCP", "targetPort": 8080}],
            "selector": {"deployment": "training-portal"},
        },
    }

    ingress_body = {
        "apiVersion": "networking.k8s.io/v1",
        "kind": "Ingress",
        "metadata": {
            "name": "training-portal",
            "namespace": portal_namespace,
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
            },
            "annotations": {},
        },
        "spec": {
            "rules": [
                {
                    "host": portal_hostname,
                    "http": {
                        "paths": [
                            {
                                "path": "/",
                                "pathType": "Prefix",
                                "backend": {
                                    "service": {
                                        "name": "training-portal",
                                        "port": {"number": 80},
                                    }
                                },
                            }
                        ]
                    },
                }
            ]
        },
    }

    if INGRESS_CLASS:
        ingress_body["spec"]["ingressClassName"] = INGRESS_CLASS

    if INGRESS_SECRET:
        ingress_body["metadata"]["annotations"].update(
            {
                "ingress.kubernetes.io/force-ssl-redirect": "true",
                "nginx.ingress.kubernetes.io/ssl-redirect": "true",
                "nginx.ingress.kubernetes.io/force-ssl-redirect": "true",
            }
        )

    ingress_secret_copier_body = ""

    if INGRESS_SECRET:
        ingress_secret_name = xget(spec, "portal.ingress.tlsCertificateRef.name")
        ingress_secret_namespace = xget(
            spec, "portal.ingress.tlsCertificateRef.namespace"
        )

        if ingress_secret_name:
            ingress_body["spec"]["tls"] = [
                {
                    "hosts": [portal_hostname],
                    "secretName": ingress_secret_name,
                }
            ]

            if (
                ingress_secret_namespace
                and ingress_secret_namespace != portal_namespace
            ):
                ingress_secret_copier_body = {
                    "apiVersion": "secrets.educates.dev/v1beta1",
                    "kind": "SecretCopier",
                    "metadata": {
                        "name": f"educates-ingress-secret-{portal_namespace}",
                        "labels": {
                            "training.educates.dev/component": "portal",
                            "training.educates.dev/portal.name": portal_name,
                            "training.educates.dev/portal.uid": portal_uid,
                        },
                    },
                    "spec": {
                        "rules": [
                            {
                                "sourceSecret": {
                                    "name": ingress_secret_name,
                                    "namespace": ingress_secret_namespace,
                                },
                                "targetNamespaces": {
                                    "nameSelector": {"matchNames": [portal_namespace]}
                                },
                            }
                        ]
                    },
                }

        else:
            ingress_body["spec"]["tls"] = [
                {
                    "hosts": [portal_hostname],
                    "secretName": INGRESS_SECRET,
                }
            ]

            ingress_secret_copier_body = {
                "apiVersion": "secrets.educates.dev/v1beta1",
                "kind": "SecretCopier",
                "metadata": {
                    "name": f"educates-ingress-secret-{portal_namespace}",
                    "labels": {
                        "training.educates.dev/component": "portal",
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                    },
                },
                "spec": {
                    "rules": [
                        {
                            "sourceSecret": {
                                "name": INGRESS_SECRET,
                                "namespace": OPERATOR_NAMESPACE,
                            },
                            "targetNamespaces": {
                                "nameSelector": {"matchNames": [portal_namespace]}
                            },
                        }
                    ]
                },
            }

        if INGRESS_CA_SECRET:
            xget(ingress_secret_copier_body, "spec.rules").append(
                {
                    "sourceSecret": {
                        "name": INGRESS_CA_SECRET,
                        "namespace": OPERATOR_NAMESPACE,
                    },
                    "targetNamespaces": {
                        "nameSelector": {"matchNames": [portal_namespace]}
                    },
                    "reclaimPolicy": "Delete",
                }
            )

        if ingress_secret_copier_body:
            kopf.adopt(ingress_secret_copier_body, namespace_instance.obj)

    theme_secret_copier_body = {
        "apiVersion": "secrets.educates.dev/v1beta1",
        "kind": "SecretCopier",
        "metadata": {
            "name": f"educates-website-theme-{portal_namespace}",
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
            },
        },
        "spec": {
            "rules": [
                {
                    "sourceSecret": {
                        "name": theme_name,
                        "namespace": OPERATOR_NAMESPACE,
                    },
                    "targetNamespaces": {
                        "nameSelector": {"matchNames": [portal_namespace]}
                    },
                }
            ]
        },
    }

    kopf.adopt(theme_secret_copier_body, namespace_instance.obj)

    # Create all the resources and if we fail on any then flag a transient error
    # and we will retry again later. Because of above checks in this case the
    # namespace will be deleted on a retry since it is owned by the training
    # portal, thus there will be an attempt to start over. Note that we create
    # the deployment last so no workload is created unless everything else
    # worked okay.

    try:
        if INGRESS_SECRET and ingress_secret_copier_body:
            SecretCopier(api, ingress_secret_copier_body).create()

        SecretCopier(api, theme_secret_copier_body).create()

        pykube.ServiceAccount(api, service_account_body).create()
        pykube.Secret(api, service_account_token_body).create()
        pykube.ClusterRoleBinding(api, cluster_role_binding_body).create()
        pykube.PersistentVolumeClaim(api, persistent_volume_claim_body).create()
        pykube.ConfigMap(api, config_map_body).create()
        pykube.Service(api, service_body).create()
        pykube.Ingress(api, ingress_body).create()
        pykube.Deployment(api, deployment_body).create()

    except pykube.exceptions.KubernetesError as exc:
        logger.exception("Unexpected error creating training portal %s.", portal_name)

        patch["status"] = {
            "educates": {
                "phase": "Retrying",
                "message": f"Unexpected error creating training portal {portal_name}.",
            }
        }

        report_analytics_event(
            "Resource/TemporaryError",
            {
                "kind": "TrainingPortal",
                "name": name,
                "uid": uid,
                "retry": retry,
                "message": f"Unexpected error creating training portal {portal_name}.",
            },
        )

        raise kopf.TemporaryError(
            f"Unexpected error creating training portal {portal_name}.", delay=30
        ) from exc

    # Report analytics event training portal should be ready.

    report_analytics_event(
        "Resource/Ready",
        {"kind": "TrainingPortal", "name": name, "uid": uid, "retry": retry},
    )

    logger.info(
        "Training portal %s has been deployed to namespace %s.",
        portal_name,
        portal_namespace,
    )

    # Save away the details of the portal which was created in status.

    patch["status"] = {}

    patch["status"]["educates"] = {
        "phase": "Running",
        "message": None,
        "namespace": portal_namespace,
        "url": portal_url,
        "credentials": {
            "admin": {"username": admin_username, "password": admin_password},
            "robot": {"username": robot_username, "password": robot_password},
        },
        "clients": {"robot": {"id": robot_client_id, "secret": robot_client_secret}},
    }


@kopf.on.delete(
    "training.educates.dev",
    "v1beta1",
    "trainingportals",
    optional=True,
)
def training_portal_delete(**_):
    """Nothing to do here at this point because the owner references will
    ensure that everything is cleaned up appropriately."""

    # NOTE: This doesn't actually get called because we as we marked it as
    # optional to avoid a finalizer being added to the custom resource, so we
    # use separate generic event handler below to log when the training portal
    # is deleted.


@kopf.on.event("training.educates.dev", "v1beta1", "trainingportals")
def training_portal_event(type, event, **_):  # pylint: disable=redefined-builtin
    """Log when a training portal is deleted."""

    if type == "DELETED":
        logger.info(
            "Training portal %s has been deleted.", event["object"]["metadata"]["name"]
        )


@kopf.on.event(
    "",
    "v1",
    "pods",
    labels={
        "training.educates.dev/component": "portal",
        "training.educates.dev/portal.services.dashboard": "true",
    },
)
def training_portal_pod_event(type, event, **_):  # pylint: disable=redefined-builtin
    """Log the status of deployment of any training portal pods."""

    pod_name = event["object"]["metadata"]["name"]
    pod_namespace = event["object"]["metadata"]["namespace"]

    pod_status = event["object"].get("status", {}).get("phase", "Unknown")

    if type in ("ADDED", "MODIFIED", "DELETED"):
        logger.info(
            "Training portal pod %s in namespace %s has been %s with current status of %s.",
            pod_name,
            pod_namespace,
            type.lower(),
            pod_status,
        )

    else:
        logger.info(
            "Training portal pod %s in namespace %s has been found with current status of %s.",
            pod_name,
            pod_namespace,
            pod_status,
        )
