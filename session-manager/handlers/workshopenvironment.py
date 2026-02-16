import os
import base64
import copy
import logging

import yaml

import kopf
import pykube

from .objects import create_from_dict, Workshop, SecretCopier
from .helpers import (
    xget,
    resource_owned_by,
    substitute_variables,
    smart_overlay_merge,
    image_pull_policy,
    Applications,
)
from .applications import environment_objects_list, workshop_spec_patches
from .kyverno_rules import kyverno_environment_rules
from .analytics import report_analytics_event

from .operator_config import (
    resolve_workshop_image,
    PLATFORM_ARCH,
    OPERATOR_NAMESPACE,
    IMAGE_REPOSITORY,
    CLUSTER_DOMAIN,
    INGRESS_DOMAIN,
    INGRESS_PROTOCOL,
    INGRESS_SECRET,
    INGRESS_CA_SECRET,
    INGRESS_CLASS,
    CLUSTER_STORAGE_CLASS,
    CLUSTER_STORAGE_USER,
    CLUSTER_STORAGE_GROUP,
    CLUSTER_SECURITY_POLICY_ENGINE,
    WORKSHOP_SECURITY_RULES_ENGINE,
    DOCKERD_MIRROR_REMOTE,
    DOCKERD_MIRROR_USERNAME,
    DOCKERD_MIRROR_PASSWORD,
    NETWORK_BLOCKCIDRS,
    DOCKER_REGISTRY_IMAGE,
    BASE_ENVIRONMENT_IMAGE,
    TUNNEL_MANAGER_IMAGE,
    IMAGE_CACHE_IMAGE,
    ASSETS_SERVER_IMAGE,
)

__all__ = ["workshop_environment_create", "workshop_environment_delete"]

logger = logging.getLogger("educates.workshopenvironment")

api = pykube.HTTPClient(pykube.KubeConfig.from_env())


@kopf.index("training.educates.dev", "v1beta1", "workshopenvironments")
def workshop_environment_index(name, meta, body, **_):
    """Keeps an index of the workshop environments. This is used to allow
    workshop environments to be found when processing a workshop allocation
    request."""

    generation = meta["generation"]

    logger.info(
        "Workshop environment %s with generation %s has been cached.", name, generation
    )

    return {(None, name): body}


@kopf.on.resume(
    "training.educates.dev",
    "v1beta1",
    "workshopenvironments",
)
def workshop_environment_resume(name, **_):
    """Used to acknowledge that there was an existing workshop environment
    resource found when the operator started up."""

    logger.info(
        "Workshop environment %s has been found but was previously processed.",
        name,
    )


@kopf.on.create(
    "training.educates.dev",
    "v1beta1",
    "workshopenvironments",
)
def workshop_environment_create(
    name, uid, body, meta, spec, status, patch, runtime, retry, **_
):
    """Handle creation of a training portal resource. This involves creating a
    shared namespace for holding workshop session instances and any other
    resources associated with the workshop environment."""

    # Report analytics event indicating processing workshop environment.

    report_analytics_event(
        "Resource/Create",
        {"kind": "WorkshopEnvironment", "name": name, "uid": uid, "retry": retry},
    )

    if retry > 0:
        logger.info(
            "Workshop environment creation request for %s being retried, retries %d.",
            name,
            retry,
        )
    else:
        logger.info(
            "Workshop environment creation request for %s being processed.", name
        )

    # Use the name of the custom resource as the name of the namespace under
    # which the workshop environment is created and any workshop instances are
    # created.

    environment_name = name
    environment_uid = uid

    workshop_namespace = environment_name

    # Can optionally be passed name of the training portal via a label when the
    # workshop environment is created as a child to a training portal.

    portal_name = meta.get("labels", {}).get(
        "training.educates.dev/portal.name", ""
    )
    portal_uid = meta.get("labels", {}).get(
        "training.educates.dev/portal.uid", ""
    )

    # The name of the workshop to be deployed can differ and is taken from the
    # specification of the workshop environment. Lookup the workshop resource
    # definition and ensure it exists. Later we will stash a copy of this in the
    # status of the custom resource, and we will use this copy to avoid being
    # affected by changes in the original after the creation of the workshop
    # environment.

    workshop_name = spec["workshop"]["name"]

    try:
        workshop_instance = Workshop.objects(api).get(name=workshop_name)

    except pykube.exceptions.ObjectDoesNotExist as exc:
        if runtime.total_seconds() >= 300:
            patch["status"] = {
                "educates": {
                    "phase": "Failed",
                    "message": f"Workshop {workshop_name} is not available.",
                }
            }

            report_analytics_event(
                "Resource/PermanentError",
                {
                    "kind": "WorkshopEnvironment",
                    "name": name,
                    "uid": uid,
                    "retry": retry,
                    "message": f"Workshop {workshop_name} is not available.",
                },
            )

            raise kopf.PermanentError(
                f"Workshop {workshop_name} required by workshop environment {environment_name} is not available."
            ) from exc

        else:
            patch["status"] = {
                "educates": {
                    "phase": "Pending",
                    "message": f"Workshop {workshop_name} is not available.",
                }
            }

            report_analytics_event(
                "Resource/TemporaryError",
                {
                    "kind": "TrainingPortal",
                    "name": name,
                    "uid": uid,
                    "retry": retry,
                    "message": f"Workshop {workshop_name} is not available.",
                },
            )

            raise kopf.TemporaryError(
                f"Workshop {workshop_name} required by workshop environment {environment_name} is not available.",
                delay=30,
            )

    # Grab the details of the workshop instance so we can stash a copy of the
    # details in the status of the workshop environment.

    workshop_uid = workshop_instance.obj["metadata"]["uid"]
    workshop_generation = workshop_instance.obj["metadata"]["generation"]
    workshop_spec = workshop_instance.obj.get("spec", {})

    workshop_version = workshop_spec.get("version", "latest")

    # Remove the last applied configuration annotation from the workshop. This
    # is done to avoid storing a large amount of data.
    #
    # NOTE: This may actually not be required any longer as we are now storing
    # only the spec in the status of the workshop environment and not the whole
    # workshop resource definition.

    try:
        del workshop_instance.obj["metadata"]["annotations"][
            "kubectl.kubernetes.io/last-applied-configuration"
        ]
    except KeyError:
        pass

    # Create a wrapper for determining what applications are enabled and what
    # configuration they provide. This includes allowing applications to patch
    # the workshop config. As an application could enable another application
    # because it requires it, we calculate the list of applications again after
    # patching. It is the modified version of the config which gets saved in
    # the status so that it can be used later by the workshop session.

    applications = Applications(workshop_spec["session"].get("applications", {}))

    for application in applications:
        if applications.is_enabled(application):
            workshop_config_patch = workshop_spec_patches(
                application, workshop_spec, applications.properties(application)
            )
            smart_overlay_merge(workshop_spec, workshop_config_patch.get("spec", {}))

    applications = Applications(workshop_spec["session"].get("applications", {}))

    # Create the namespace for holding the workshop environment before we create
    # anything else. Before we attempt to create the namespace, we first see
    # whether it may already exist. This could be because a prior namespace
    # hadn't yet been deleted, or we failed on a prior attempt to create the
    # workshop environment some point after the namespace had been created but
    # before all other resources could be created.

    try:
        namespace_instance = pykube.Namespace.objects(api).get(name=workshop_namespace)

    except pykube.exceptions.ObjectDoesNotExist:
        # Namespace doesn't exist so we should be all okay to continue.

        pass

    except pykube.exceptions.KubernetesError as exc:
        logger.exception(
            "Unexpected error querying namespace %s required by workshop environment %s.",
            workshop_namespace,
            environment_name,
        )

        patch["status"] = {
            "educates": {
                "phase": "Unknown",
                "message": f"Unexpected error querying namespace {workshop_namespace}.",
            }
        }

        report_analytics_event(
            "Resource/TemporaryError",
            {
                "kind": "WorkshopEnvironment",
                "name": name,
                "uid": uid,
                "retry": retry,
                "message": f"Unexpected error querying namespace {workshop_namespace}.",
            },
        )

        raise kopf.TemporaryError(
            f"Unexpected error querying namespace {workshop_namespace} required by workshop environment {environment_name}.",
            delay=30,
        ) from exc

    else:
        # The namespace already exists. We need to check whether it is owned by
        # this workshop environment instance.

        if not resource_owned_by(namespace_instance.obj, body):
            # Namespace is owned by another party so we flag a transient error
            # and will check again later to give time for the namespace to be
            # deleted.

            if runtime.total_seconds() >= 300:
                patch["status"] = {
                    "educates": {
                        "phase": "Failed",
                        "message": f"Namespace {workshop_namespace} already exists.",
                    }
                }

                report_analytics_event(
                    "Resource/PermanentError",
                    {
                        "kind": "WorkshopEnvironment",
                        "name": name,
                        "uid": uid,
                        "retry": retry,
                        "message": f"Namespace {workshop_namespace} already exists.",
                    },
                )

                raise kopf.PermanentError(
                    f"Namespace {workshop_namespace} required by workshop environment {environment_name} already exists."
                )

            else:
                patch["status"] = {
                    "educates": {
                        "phase": "Pending",
                        "message": f"Namespace {workshop_namespace} already exists.",
                    }
                }

                report_analytics_event(
                    "Resource/TemporaryError",
                    {
                        "kind": "WorkshopEnvironment",
                        "name": name,
                        "uid": uid,
                        "retry": retry,
                        "message": f"Namespace {workshop_namespace} already exists.",
                    },
                )

                raise kopf.TemporaryError(
                    f"Namespace {workshop_namespace} required by workshop environment {environment_name} already exists.",
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
                            "message": f"Unable to setup workshop environment {environment_name}.",
                        }
                    }

                    report_analytics_event(
                        "Resource/PermanentError",
                        {
                            "kind": "WorkshopEnvironment",
                            "name": name,
                            "uid": uid,
                            "retry": retry,
                            "message": f"Unable to setup workshop environment {environment_name}.",
                        },
                    )

                    raise kopf.PermanentError(
                        f"Unable to setup workshop environment {environment_name}."
                    )

                else:
                    try:
                        namespace_instance.delete()

                    except pykube.exceptions.PyKubeError as exc:
                        logger.exception(
                            "Unexpected error deleting namespace %s required by workshop environment %s.",
                            workshop_namespace,
                            environment_name,
                        )

                        patch["status"] = {
                            "educates": {
                                "phase": "Retrying",
                                "message": f"Failed to delete namespace {workshop_namespace}.",
                            }
                        }

                        report_analytics_event(
                            "Resource/TemporaryError",
                            {
                                "kind": "WorkshopEnvironment",
                                "name": name,
                                "uid": uid,
                                "retry": retry,
                                "message": f"Failed to delete namespace {workshop_namespace}.",
                            },
                        )

                        raise kopf.TemporaryError(
                            f"Failed to delete namespace {workshop_namespace} required by workshop environment {environment_name}.",
                            delay=30,
                        ) from exc

                    patch["status"] = {
                        "educates": {
                            "phase": "Retrying",
                            "message": f"Deleting namespace {workshop_namespace} and retrying.",
                        }
                    }

                    report_analytics_event(
                        "Resource/TemporaryError",
                        {
                            "kind": "WorkshopEnvironment",
                            "name": name,
                            "uid": uid,
                            "retry": retry,
                            "message": f"Deleting namespace {workshop_namespace} and retrying.",
                        },
                    )

                    raise kopf.TemporaryError(
                        f"Deleting namespace {workshop_namespace} required by workshop environment {environment_name} and retrying.",
                        delay=30,
                    )

            else:
                patch["status"] = {
                    "educates": {
                        "phase": "Unknown",
                        "message": f"Workshop environment {environment_name} in unexpected state {phase}.",
                    }
                }

                report_analytics_event(
                    "Resource/TemporaryError",
                    {
                        "kind": "WorkshopEnvironment",
                        "name": name,
                        "uid": uid,
                        "retry": retry,
                        "message": f"Workshop environment {environment_name} in unexpected state {phase}.",
                    },
                )

                raise kopf.TemporaryError(
                    f"Workshop environment {environment_name} in unexpected state {phase}.",
                    delay=30,
                )

    # Namespace doesn't already exist so we need to create it. We set the owner
    # of the namespace to be the workshop environment resource, but set anything
    # created as part of the workshop environment as being owned by the
    # namespace so that the namespace will remain stuck in terminating state
    # until the child resources are deleted. Believe this makes clearer what is
    # going on as you may miss that workshop environment is stuck. It also
    # allows is to delete the namespace and have all resources deleted before
    # retrying in the case an error during setup was transient.

    namespace_body = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {
            "name": workshop_namespace,
            "labels": {
                "training.educates.dev/component": "environment",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/policy.engine": CLUSTER_SECURITY_POLICY_ENGINE,
                "training.educates.dev/policy.name": "privileged",
            },
            "annotations": {"secretgen.carvel.dev/excluded-from-wildcard-matching": ""},
        },
    }

    if CLUSTER_SECURITY_POLICY_ENGINE == "pod-security-standards":
        namespace_body["metadata"]["labels"][
            "pod-security.kubernetes.io/enforce"
        ] = "privileged"

    kopf.adopt(namespace_body)

    try:
        pykube.Namespace(api, namespace_body).create()

        namespace_instance = pykube.Namespace.objects(api).get(name=workshop_namespace)

    except pykube.exceptions.PyKubeError as exc:
        logger.exception(
            "Unexpected error creating namespace %s required by workshop environment %s.",
            workshop_namespace,
            environment_name,
        )

        patch["status"] = {
            "educates": {
                "phase": "Retrying",
                "message": f"Failed to create namespace {workshop_namespace}.",
            }
        }

        raise kopf.TemporaryError(
            f"Failed to create namespace {workshop_namespace} required by workshop environment {environment_name}.",
            delay=30,
        ) from exc

    # Set status as retrying in case there is a failure before everything is
    # completed with setup of workshop environment. We will clear this before
    # returning if everything is successful. Because kopf will automatically
    # keep retrying when there is an unexpected error, we can avoid having to
    # explicitly catch and raise a temporary error at every point beyond here.

    patch["status"] = {"educates": {"phase": "Retrying"}}

    # Apply security policies to whole namespace if enabled. We need to set the
    # whole namespace as requiring privilged as we need to run docker in docker
    # in this namespace.

    if CLUSTER_SECURITY_POLICY_ENGINE == "pod-security-policies":
        psp_role_binding_body = {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "RoleBinding",
            "metadata": {
                "name": "educates-security-policy",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "roleRef": {
                "apiGroup": "rbac.authorization.k8s.io",
                "kind": "ClusterRole",
                "name": "educates-privileged-psp",
            },
            "subjects": [
                {
                    "apiGroup": "rbac.authorization.k8s.io",
                    "kind": "Group",
                    "name": f"system:serviceaccounts:{workshop_namespace}",
                }
            ],
        }

        kopf.adopt(psp_role_binding_body, namespace_instance.obj)

        pykube.RoleBinding(api, psp_role_binding_body).create()

    if CLUSTER_SECURITY_POLICY_ENGINE == "security-context-constraints":
        scc_role_binding_body = {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "RoleBinding",
            "metadata": {
                "name": "educates-security-policy",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "roleRef": {
                "apiGroup": "rbac.authorization.k8s.io",
                "kind": "ClusterRole",
                "name": "educates-privileged-scc",
            },
            "subjects": [
                {
                    "apiGroup": "rbac.authorization.k8s.io",
                    "kind": "Group",
                    "name": f"system:serviceaccounts:{workshop_namespace}",
                }
            ],
        }

        kopf.adopt(scc_role_binding_body, namespace_instance.obj)

        pykube.RoleBinding(api, scc_role_binding_body).create()

    # Delete any limit ranges applied to the namespace so they don't cause
    # issues with workshop instance deployments or any workshop deployments.
    # This can be an issue where namespace/project templates apply them
    # automatically to a namespace. The problem is that we may do this query too
    # quickly and they may not have been created as yet.

    for limit_range in pykube.LimitRange.objects(
        api, namespace=workshop_namespace
    ).all():
        try:
            limit_range.delete()
        except pykube.exceptions.ObjectDoesNotExist:
            pass

    # Delete any resource quotas applied to the namespace so they don't cause
    # issues with workshop instance deploymemnts or any workshop resources. This
    # can be an issue where namespace/project templates apply them automatically
    # to a namespace. The problem is that we may do this query too quickly and
    # they may not have been created as yet.

    for resource_quota in pykube.ResourceQuota.objects(
        api, namespace=workshop_namespace
    ).all():
        try:
            resource_quota.delete()
        except pykube.exceptions.ObjectDoesNotExist:
            pass

    # If there is a CIDR list of networks to block create a network policy in
    # the workshop environment to restrict access from all pods. Pods here
    # include the workshop pods where each users terminal runs.

    if NETWORK_BLOCKCIDRS:
        network_policy_body = {
            "apiVersion": "networking.k8s.io/v1",
            "kind": "NetworkPolicy",
            "metadata": {
                "name": "educates-network-policy",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "spec": {
                "policyTypes": ["Egress"],
                "egress": [],
            },
        }

        egresses = []

        ipv4_blockcidrs = []
        ipv6_blockcidrs = []

        for block in list(NETWORK_BLOCKCIDRS):
            if ":" in block:
                ipv6_blockcidrs.append(block)
            else:
                ipv4_blockcidrs.append(block)

        if ipv4_blockcidrs:
            egresses.append(
                {"to": [{"ipBlock": {"cidr": "0.0.0.0/0", "except": ipv4_blockcidrs}}]}
            )

        if ipv6_blockcidrs:
            egresses.append(
                {"to": [{"ipBlock": {"cidr": "::/0", "except": ipv6_blockcidrs}}]}
            )

        network_policy_body["spec"]["egress"] = egresses

        kopf.adopt(network_policy_body, namespace_instance.obj)

        NetworkPolicy = pykube.object_factory(  # pylint: disable=invalid-name
            api, "networking.k8s.io/v1", "NetworkPolicy"
        )

        NetworkPolicy(api, network_policy_body).create()

    # Create a config map in the workshop namespace which contains the details
    # about the workshop. This will be mounted into workshop instances so they
    # can derive information to configure themselves. We need to make sure not
    # including potentially sensitive details such as lists of Kubernetes
    # resources or docker-compose config.

    applications_config = workshop_spec.get("session", {}).get("applications", {})
    applications_config = copy.deepcopy(applications_config)
    applications_config.get("docker", {}).pop("compose", None)
    applications_config.get("vcluster", {}).pop("objects", None)

    workshop_config = {
        "spec": {
            "title": workshop_spec.get("title", ""),
            "description": workshop_spec.get("description", ""),
            "version": workshop_spec.get("version", "latest"),
            "session": {
                "applications": applications_config,
                "ingresses": workshop_spec.get("session", {}).get("ingresses", []),
                "dashboards": workshop_spec.get("session", {}).get("dashboards", []),
            },
        }
    }

    config_secret_body = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": "workshop-config",
            "namespace": workshop_namespace,
            "labels": {
                "training.educates.dev/component": "environment",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
            },
        },
        "data": {
            "workshop.yaml": base64.b64encode(
                yaml.dump(workshop_config, Dumper=yaml.Dumper).encode("utf-8")
            ).decode("utf-8"),
        },
    }

    image_repository = IMAGE_REPOSITORY

    image_registry_host = xget(spec, "registry.host")
    image_registry_namespace = xget(spec, "registry.namespace")

    if image_registry_host:
        if image_registry_namespace:
            image_repository = f"{image_registry_host}/{image_registry_namespace}"
        else:
            image_repository = image_registry_host

    base_workshop_image = BASE_ENVIRONMENT_IMAGE
    base_workshop_image_pull_policy = image_pull_policy(base_workshop_image)

    workshop_image = resolve_workshop_image(
        workshop_spec.get("workshop", {}).get(
            "image", workshop_spec.get("content", {}).get("image", "base-environment:*")
        )
    )

    workshop_image_pull_policy = image_pull_policy(workshop_image)

    assets_repository = f"assets-server.{workshop_namespace}.svc.{CLUSTER_DOMAIN}"

    if xget(workshop_spec, "environment.assets.ingress.enabled", False):
        assets_repository = f"assets-{workshop_namespace}.{INGRESS_DOMAIN}"

    oci_image_cache = f"image-cache.{workshop_namespace}.svc.{CLUSTER_DOMAIN}"

    if xget(workshop_spec, "environment.images.ingress.enabled", False):
        oci_image_cache = f"images-{workshop_namespace}.{INGRESS_DOMAIN}"

    ingress_port = "80"

    if INGRESS_PROTOCOL == "https":
        ingress_port = "443"

    environment_downloads_variables = dict(
        platform_arch=PLATFORM_ARCH,
        image_repository=image_repository,
        oci_image_cache=oci_image_cache,
        assets_repository=assets_repository,
        workshop_name=workshop_name,
        workshop_version=workshop_version,
        environment_name=environment_name,
        workshop_namespace=workshop_namespace,
        cluster_domain=CLUSTER_DOMAIN,
        ingress_domain=INGRESS_DOMAIN,
        ingress_protocol=INGRESS_PROTOCOL,
        ingress_port=ingress_port,
        ingress_port_suffix="",
        training_portal=portal_name,
    )

    workshop_files = workshop_spec.get("workshop", {}).get("files", [])

    if workshop_files:
        vendir_count = 1

        for workshop_files_item in workshop_files:
            vendir_config = {
                "apiVersion": "vendir.k14s.io/v1alpha1",
                "kind": "Config",
                "directories": [],
            }

            directories_config = []

            workshop_files_item = substitute_variables(
                workshop_files_item, environment_downloads_variables
            )
            workshop_files_path = workshop_files_item.pop("path", ".")
            workshop_files_path = os.path.join("/opt/assets/files", workshop_files_path)
            workshop_files_path = os.path.normpath(workshop_files_path)
            workshop_files_item["path"] = "."

            directories_config.append(
                {"path": workshop_files_path, "contents": [workshop_files_item]}
            )

            vendir_config["directories"] = directories_config

            config_secret_body["data"]["vendir-assets-%02d.yaml" % vendir_count] = (
                base64.b64encode(
                    yaml.dump(vendir_config, Dumper=yaml.Dumper).encode("utf-8")
                ).decode("utf-8")
            )

            vendir_count += 1

    packages = workshop_spec.get("workshop", {}).get("packages", [])

    if packages:
        vendir_config = {
            "apiVersion": "vendir.k14s.io/v1alpha1",
            "kind": "Config",
            "directories": [],
        }

        directories_config = []

        for package in packages:
            package_name = package["name"]
            package_files = substitute_variables(
                package["files"], environment_downloads_variables
            )
            directories_config.append(
                {"path": f"/opt/packages/{package_name}", "contents": package_files}
            )

        vendir_config["directories"] = directories_config

        config_secret_body["data"]["vendir-packages.yaml"] = base64.b64encode(
            yaml.dump(vendir_config, Dumper=yaml.Dumper).encode("utf-8")
        ).decode("utf-8")

    kopf.adopt(config_secret_body, namespace_instance.obj)

    pykube.Secret(api, config_secret_body).create()

    # Because the Kubernetes web console is designed for working against
    # a whole cluster and we want to use it in scope of a single
    # namespace, we need to at least grant it roles to be able to list
    # and get namespaces. If we don't do this then the web console will
    # forever generate a stream of events complaining that it can't read
    # namespaces. This seems to hasten the web console hanging, but also
    # means can't easily switch to other namespaces the workshop has
    # access to as the list of namespaces cannot be generated. At this
    # point we therefore create a cluster role associated with the
    # name of the namespace create for the workshop environment. This
    # will later be bound to the service account the workshop
    # instances and web consoles runs as. As with the namespace we add
    # it as a child to the custom resource for the workshop.

    cluster_role_body = {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "ClusterRole",
        "metadata": {
            "name": f"educates-web-console-{workshop_namespace}",
            "labels": {
                "training.educates.dev/component": "environment",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
            },
        },
        "rules": [
            {
                "apiGroups": [""],
                "resources": ["namespaces"],
                "verbs": ["get", "list", "watch"],
            }
        ],
    }

    kopf.adopt(cluster_role_body, namespace_instance.obj)

    pykube.ClusterRole(api, cluster_role_body).create()

    # Setup rule for copying the ingress secret into the workshop namespace.

    if INGRESS_SECRET:
        secret_copier_body = {
            "apiVersion": "secrets.educates.dev/v1beta1",
            "kind": "SecretCopier",
            "metadata": {
                "name": f"educates-ingress-secret-{workshop_namespace}",
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
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
                            "nameSelector": {"matchNames": [workshop_namespace]}
                        },
                        "reclaimPolicy": "Delete",
                    }
                ],
            },
        }

        if INGRESS_CA_SECRET:
            xget(secret_copier_body, "spec.rules").append(
                {
                    "sourceSecret": {
                        "name": INGRESS_CA_SECRET,
                        "namespace": OPERATOR_NAMESPACE,
                    },
                    "targetNamespaces": {
                        "nameSelector": {"matchNames": [workshop_namespace]}
                    },
                    "reclaimPolicy": "Delete",
                }
            )

        kopf.adopt(secret_copier_body, namespace_instance.obj)

        SecretCopier(api, secret_copier_body).create()

    # Setup rules for copying any workshop secrets into the workshop namespace.

    if workshop_spec.get("environment", {}).get("secrets"):
        secrets = workshop_spec["environment"]["secrets"]

        secret_copier_body = {
            "apiVersion": "secrets.educates.dev/v1beta1",
            "kind": "SecretCopier",
            "metadata": {
                "name": f"educates-workshop-secrets-{workshop_namespace}",
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "spec": {
                "rules": [],
            },
        }

        for secret_value in secrets:
            secret_name = secret_value["name"]
            secret_namespace = secret_value["namespace"]
            secret_labels = secret_value.get("labels", {})

            secret_name = secret_name.replace("$(workshop_name)", workshop_name)

            secret_copier_body["spec"]["rules"].append(
                {
                    "sourceSecret": {
                        "name": secret_name,
                        "namespace": secret_namespace,
                    },
                    "targetNamespaces": {
                        "nameSelector": {"matchNames": [workshop_namespace]}
                    },
                    "targetSecret": {"labels": secret_labels},
                    "reclaimPolicy": "Delete",
                }
            )

        kopf.adopt(secret_copier_body, namespace_instance.obj)

        SecretCopier(api, secret_copier_body).create()

    if spec.get("environment", {}).get("secrets"):
        secrets = spec["environment"]["secrets"]

        secret_copier_body = {
            "apiVersion": "secrets.educates.dev/v1beta1",
            "kind": "SecretCopier",
            "metadata": {
                "name": f"educates-environment-secrets-{workshop_namespace}",
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "spec": {
                "rules": [],
            },
        }

        for secret_value in secrets:
            secret_name = secret_value["name"]
            secret_namespace = secret_value["namespace"]
            secret_labels = secret_value.get("labels", {})

            secret_name = secret_name.replace("$(workshop_name)", workshop_name)

            secret_copier_body["spec"]["rules"].append(
                {
                    "sourceSecret": {
                        "name": secret_name,
                        "namespace": secret_namespace,
                    },
                    "targetNamespaces": {
                        "nameSelector": {"matchNames": [workshop_namespace]}
                    },
                    "targetSecret": {"labels": secret_labels},
                    "reclaimPolicy": "Delete",
                }
            )

        kopf.adopt(secret_copier_body, namespace_instance.obj)

        SecretCopier(api, secret_copier_body).create()

    # Copy secret containing the website theme data files into the workshop
    # namespace.

    theme_name = xget(spec, "theme.name", "default-website-theme")

    theme_secret_copier_body = {
        "apiVersion": "secrets.educates.dev/v1beta1",
        "kind": "SecretCopier",
        "metadata": {
            "name": f"educates-website-theme-{workshop_namespace}",
            "labels": {
                "training.educates.dev/component": "environment",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
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
                        "nameSelector": {"matchNames": [workshop_namespace]}
                    },
                    "targetSecret": {"name": "workshop-theme"},
                }
            ]
        },
    }

    kopf.adopt(theme_secret_copier_body, namespace_instance.obj)

    SecretCopier(api, theme_secret_copier_body).create()

    # Calculate the set of variables for interpolation into the environment
    # objects and other resources being created. The set of variables includes
    # special variables dependent on the list of enabled applications.

    environment_token = spec.get("request", {}).get("token", "")

    environment_variables = dict(
        workshop_environment_uid=uid,
        platform_arch=PLATFORM_ARCH,
        image_repository=image_repository,
        oci_image_cache=oci_image_cache,
        assets_repository=assets_repository,
        service_account="educates-services",
        workshop_name=workshop_name,
        workshop_version=workshop_version,
        workshop_image=workshop_image,
        workshop_image_pull_policy=workshop_image_pull_policy,
        environment_name=environment_name,
        environment_token=environment_token,
        workshop_namespace=workshop_namespace,
        cluster_domain=CLUSTER_DOMAIN,
        ingress_domain=INGRESS_DOMAIN,
        ingress_protocol=INGRESS_PROTOCOL,
        ingress_port=ingress_port,
        ingress_port_suffix="",
        ingress_secret=INGRESS_SECRET,
        ingress_class=INGRESS_CLASS,
        storage_class=CLUSTER_STORAGE_CLASS,
        training_portal=portal_name,
    )

    application_variables_list = workshop_spec.get("environment", {}).get("variables", [])

    application_variables_list = substitute_variables(
        application_variables_list, environment_variables
    )

    for variable in application_variables_list:
        environment_variables[variable["name"]] = variable["value"]

    # Create a service account for running any services in the workshop
    # namespace such as session specific or mirror container registries.

    service_account_body = {
        "apiVersion": "v1",
        "kind": "ServiceAccount",
        "metadata": {
            "name": "educates-services",
            "namespace": workshop_namespace,
            "labels": {
                "training.educates.dev/component": "environment",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
            },
        },
    }

    pykube.ServiceAccount(api, service_account_body).create()

    # Potentially use base workshop image later on to initialize storage
    # permissions.

    base_workshop_image = BASE_ENVIRONMENT_IMAGE
    base_workshop_image_pull_policy = image_pull_policy(base_workshop_image)

    # If docker is enabled and system profile indicates that a registry
    # mirror should be used, we deploy a registry mirror in the workshop
    # namespace. We only need to expose it via a service, not an ingress
    # as the dockerd is in the same namespace and can talk to it direct.

    if applications.is_enabled("docker"):
        if DOCKERD_MIRROR_REMOTE:
            mirror_objects = []

            mirror_memory = applications.property("registry", "memory", "768Mi")
            mirror_storage = applications.property("docker", "storage", "5Gi")

            mirror_persistent_volume_claim_body = {
                "apiVersion": "v1",
                "kind": "PersistentVolumeClaim",
                "metadata": {
                    "namespace": workshop_namespace,
                    "name": f"{workshop_namespace}-mirror",
                    "labels": {
                        "training.educates.dev/component": "environment",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                    },
                },
                "spec": {
                    "accessModes": ["ReadWriteOnce"],
                    "resources": {"requests": {"storage": mirror_storage}},
                },
            }

            if CLUSTER_STORAGE_CLASS:
                mirror_persistent_volume_claim_body["spec"][
                    "storageClassName"
                ] = CLUSTER_STORAGE_CLASS

            registry_image = DOCKER_REGISTRY_IMAGE
            registry_image_pull_policy = image_pull_policy(registry_image)

            mirror_deployment_body = {
                "apiVersion": "apps/v1",
                "kind": "Deployment",
                "metadata": {
                    "namespace": workshop_namespace,
                    "name": f"{workshop_namespace}-mirror",
                    "labels": {
                        "training.educates.dev/component": "environment",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                        "training.educates.dev/environment.services.mirror": "true",
                    },
                },
                "spec": {
                    "replicas": 1,
                    "selector": {
                        "matchLabels": {"deployment": f"{workshop_namespace}-mirror"}
                    },
                    "strategy": {"type": "Recreate"},
                    "template": {
                        "metadata": {
                            "labels": {
                                "deployment": f"{workshop_namespace}-mirror",
                                "training.educates.dev/component": "environment",
                                "training.educates.dev/workshop.name": workshop_name,
                                "training.educates.dev/portal.name": portal_name,
                                "training.educates.dev/portal.uid": portal_uid,
                                "training.educates.dev/environment.name": environment_name,
                                "training.educates.dev/environment.uid": environment_uid,
                                "training.educates.dev/environment.services.mirror": "true",
                            },
                        },
                        "spec": {
                            "serviceAccountName": "educates-services",
                            "initContainers": [],
                            "containers": [
                                {
                                    "name": "registry",
                                    "image": registry_image,
                                    "imagePullPolicy": registry_image_pull_policy,
                                    "securityContext": {
                                        "allowPrivilegeEscalation": False,
                                        "capabilities": {"drop": ["ALL"]},
                                        "runAsNonRoot": True,
                                        # "seccompProfile": {"type": "RuntimeDefault"},
                                    },
                                    "resources": {
                                        "limits": {"memory": mirror_memory},
                                        "requests": {"memory": mirror_memory},
                                    },
                                    "ports": [
                                        {"containerPort": 5000, "protocol": "TCP"}
                                    ],
                                    "env": [
                                        {
                                            "name": "REGISTRY_STORAGE_DELETE_ENABLED",
                                            "value": "true",
                                        },
                                        {
                                            "name": "REGISTRY_PROXY_REMOTEURL",
                                            "value": DOCKERD_MIRROR_REMOTE,
                                        },
                                        {
                                            "name": "REGISTRY_PROXY_USERNAME",
                                            "value": DOCKERD_MIRROR_USERNAME,
                                        },
                                        {
                                            "name": "REGISTRY_PROXY_PASSWORD",
                                            "value": DOCKERD_MIRROR_PASSWORD,
                                        },
                                    ],
                                    "volumeMounts": [
                                        {
                                            "name": "data",
                                            "mountPath": "/var/lib/registry",
                                        },
                                    ],
                                }
                            ],
                            "securityContext": {
                                "runAsUser": 1000,
                                "fsGroup": CLUSTER_STORAGE_GROUP,
                                "supplementalGroups": [CLUSTER_STORAGE_GROUP],
                            },
                            "volumes": [
                                {
                                    "name": "data",
                                    "persistentVolumeClaim": {
                                        "claimName": f"{workshop_namespace}-mirror"
                                    },
                                },
                            ],
                        },
                    },
                },
            }

            if CLUSTER_STORAGE_USER:
                # This hack is to cope with Kubernetes clusters which don't
                # properly set up persistent volume ownership. IBM Kubernetes is
                # one example. The init container runs as root and sets
                # permissions on the storage and ensures it is group writable.
                # Note that this will only work where pod security policies are
                # not enforced. Don't attempt to use it if they are. If they
                # are, this hack should not be required.

                storage_init_container = {
                    "name": "storage-permissions-initialization",
                    "image": registry_image,
                    "imagePullPolicy": registry_image_pull_policy,
                    "securityContext": {
                        "allowPrivilegeEscalation": False,
                        "capabilities": {"drop": ["ALL"]},
                        "runAsNonRoot": False,
                        "runAsUser": 0,
                        # "seccompProfile": {"type": "RuntimeDefault"},
                    },
                    "command": ["/bin/sh", "-c"],
                    "args": [
                        f"chown {CLUSTER_STORAGE_USER}:{CLUSTER_STORAGE_GROUP} /mnt && chmod og+rwx /mnt"
                    ],
                    "volumeMounts": [{"name": "data", "mountPath": "/mnt"}],
                }

                mirror_deployment_body["spec"]["template"]["spec"]["initContainers"] = [
                    storage_init_container
                ]

            mirror_service_body = {
                "apiVersion": "v1",
                "kind": "Service",
                "metadata": {
                    "namespace": workshop_namespace,
                    "name": f"{workshop_namespace}-mirror",
                    "labels": {
                        "training.educates.dev/component": "environment",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                    },
                },
                "spec": {
                    "type": "ClusterIP",
                    "ports": [{"port": 5000, "targetPort": 5000}],
                    "selector": {"deployment": f"{workshop_namespace}-mirror"},
                },
            }

            mirror_objects.extend(
                [
                    mirror_persistent_volume_claim_body,
                    mirror_deployment_body,
                    mirror_service_body,
                ]
            )

            for object_body in mirror_objects:
                object_body = substitute_variables(object_body, environment_variables)
                kopf.adopt(object_body, namespace_instance.obj)
                create_from_dict(object_body)

    # If list of workshop image dependencies are defined, and any are configured
    # to be cached, deploy and artifact registry and configure it to mirror the
    # required workshop images.

    artifacts_objects = []

    artifacts_storage = xget(workshop_spec, "environment.images.storage", "")
    artifacts_memory = xget(workshop_spec, "environment.images.memory", "512Mi")
    artifacts_ingress_enabled = xget(
        workshop_spec, "environment.images.ingress.enabled", False
    )

    artifacts_registries = xget(workshop_spec, "environment.images.registries", [])
    artifacts_registries = substitute_variables(
        artifacts_registries, environment_downloads_variables
    )

    if artifacts_registries:
        artifacts_cache_image = IMAGE_CACHE_IMAGE
        artifacts_cache_image_pull_policy = image_pull_policy(artifacts_cache_image)

        artifacts_deployment_body = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "namespace": workshop_namespace,
                "name": "image-cache",
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/environment.services.images": "true",
                },
            },
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {"deployment": "image-cache"}},
                "strategy": {"type": "Recreate"},
                "template": {
                    "metadata": {
                        "labels": {
                            "deployment": "image-cache",
                            "training.educates.dev/component": "environment",
                            "training.educates.dev/workshop.name": workshop_name,
                            "training.educates.dev/portal.name": portal_name,
                            "training.educates.dev/portal.uid": portal_uid,
                            "training.educates.dev/environment.name": environment_name,
                            "training.educates.dev/environment.uid": environment_uid,
                            "training.educates.dev/environment.services.images": "true",
                        },
                    },
                    "spec": {
                        "serviceAccountName": "educates-services",
                        "containers": [
                            {
                                "name": "image-cache",
                                "image": artifacts_cache_image,
                                "imagePullPolicy": artifacts_cache_image_pull_policy,
                                "securityContext": {
                                    "allowPrivilegeEscalation": False,
                                    "capabilities": {"drop": ["ALL"]},
                                    "runAsNonRoot": True,
                                    # "seccompProfile": {"type": "RuntimeDefault"},
                                },
                                "resources": {
                                    "limits": {"memory": artifacts_memory},
                                    "requests": {"memory": artifacts_memory},
                                },
                                "ports": [{"containerPort": 5000, "protocol": "TCP"}],
                                "volumeMounts": [
                                    {
                                        "name": "config",
                                        "mountPath": "/etc/zot",
                                    },
                                    {
                                        "name": "data",
                                        "mountPath": "/var/lib/registry",
                                        "subPath": "files",
                                    },
                                ],
                            }
                        ],
                        "securityContext": {
                            "runAsUser": 1001,
                            "fsGroup": CLUSTER_STORAGE_GROUP,
                            "supplementalGroups": [CLUSTER_STORAGE_GROUP],
                        },
                        "volumes": [
                            {
                                "name": "config",
                                "configMap": {"name": "image-cache"},
                            },
                        ],
                    },
                },
            },
        }

        if artifacts_storage:
            artifacts_persistent_volume_claim_body = {
                "apiVersion": "v1",
                "kind": "PersistentVolumeClaim",
                "metadata": {
                    "namespace": workshop_namespace,
                    "name": "image-cache",
                    "labels": {
                        "training.educates.dev/component": "environment",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                    },
                },
                "spec": {
                    "accessModes": ["ReadWriteOnce"],
                    "resources": {"requests": {"storage": artifacts_storage}},
                },
            }

            if CLUSTER_STORAGE_CLASS:
                artifacts_persistent_volume_claim_body["spec"][
                    "storageClassName"
                ] = CLUSTER_STORAGE_CLASS

            if CLUSTER_STORAGE_USER:
                # This hack is to cope with Kubernetes clusters which don't
                # properly set up persistent volume ownership. IBM Kubernetes is
                # one example. The init container runs as root and sets
                # permissions on the storage and ensures it is group writable.
                # Note that this will only work where pod security policies are
                # not enforced. Don't attempt to use it if they are. If they
                # are, this hack should not be required.

                storage_init_container = {
                    "name": "storage-permissions-initialization",
                    "image": base_workshop_image,
                    "imagePullPolicy": base_workshop_image_pull_policy,
                    "securityContext": {
                        "allowPrivilegeEscalation": False,
                        "capabilities": {"drop": ["ALL"]},
                        "runAsNonRoot": False,
                        "runAsUser": 0,
                        # "seccompProfile": {"type": "RuntimeDefault"},
                    },
                    "command": ["/bin/sh", "-c"],
                    "args": [
                        f"chown {CLUSTER_STORAGE_USER}:{CLUSTER_STORAGE_GROUP} /mnt && chmod og+rwx /mnt"
                    ],
                    "volumeMounts": [{"name": "data", "mountPath": "/mnt"}],
                }

                artifacts_deployment_body["spec"]["template"]["spec"][
                    "initContainers"
                ].insert(0, storage_init_container)

            artifacts_deployment_body["spec"]["template"]["spec"]["volumes"].append(
                {
                    "name": "data",
                    "persistentVolumeClaim": {"claimName": "image-cache"},
                }
            )

            artifacts_objects.extend([artifacts_persistent_volume_claim_body])

        else:
            artifacts_deployment_body["spec"]["template"]["spec"]["volumes"].append(
                {
                    "name": "data",
                    "emptyDir": {},
                }
            )

        artifacts_service_body = {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "namespace": workshop_namespace,
                "name": "image-cache",
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "spec": {
                "type": "ClusterIP",
                "ports": [{"port": 80, "targetPort": 5000}],
                "selector": {"deployment": "image-cache"},
            },
        }

        artifacts_config = {
            "distSpecVersion": "1.0.1",
            "storage": {
                "rootDirectory": "/var/lib/registry",
                "gc": True,
            },
            "http": {
                "address": "0.0.0.0",
                "port": "5000",
                "realm": "zot",
                "auth": {
                    "htpasswd": {"path": "/dev/null"},
                },
                "accessControl": {
                    "**": {"anonymousPolicy": ["read"]},
                },
            },
            "log": {"level": "debug"},
            "extensions": {"sync": {"enable": True, "registries": []}},
        }

        artifacts_config["extensions"]["sync"]["registries"] = artifacts_registries

        artifacts_config_map_body = {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
                "name": "image-cache",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "data": {"config.yaml": yaml.dump(artifacts_config, Dumper=yaml.Dumper)},
        }

        artifacts_objects.extend(
            [
                artifacts_deployment_body,
                artifacts_service_body,
                artifacts_config_map_body,
            ]
        )

        if artifacts_ingress_enabled:
            artifacts_host = f"images-{workshop_namespace}.{INGRESS_DOMAIN}"

            artifacts_ingress_body = {
                "apiVersion": "networking.k8s.io/v1",
                "kind": "Ingress",
                "metadata": {
                    "name": "image-cache",
                    "namespace": workshop_namespace,
                    "annotations": {},
                    "labels": {
                        "training.educates.dev/component": "session",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                    },
                },
                "spec": {
                    "rules": [
                        {
                            "host": artifacts_host,
                            "http": {
                                "paths": [
                                    {
                                        "path": "/",
                                        "pathType": "Prefix",
                                        "backend": {
                                            "service": {
                                                "name": "image-cache",
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

            if INGRESS_SECRET:
                artifacts_ingress_body["metadata"]["annotations"].update(
                    {
                        "ingress.kubernetes.io/force-ssl-redirect": "true",
                        "nginx.ingress.kubernetes.io/ssl-redirect": "true",
                        "nginx.ingress.kubernetes.io/force-ssl-redirect": "true",
                    }
                )

            if INGRESS_SECRET:
                artifacts_ingress_body["spec"]["tls"] = [
                    {
                        "hosts": [artifacts_host],
                        "secretName": INGRESS_SECRET,
                    }
                ]

            artifacts_objects.extend(
                [
                    artifacts_ingress_body,
                ]
            )

        for object_body in artifacts_objects:
            object_body = substitute_variables(object_body, environment_variables)
            kopf.adopt(object_body, namespace_instance.obj)
            create_from_dict(object_body)

    # If any assets are required for the workshop environment, deploy the
    # assets-server and pre-load it with the assets.

    assets_server_objects = []

    assets_files = xget(workshop_spec, "environment.assets.files", [])
    assets_storage = xget(workshop_spec, "environment.assets.storage", "")
    assets_memory = xget(workshop_spec, "environment.assets.memory", "128Mi")
    assets_ingress_enabled = xget(
        workshop_spec, "environment.assets.ingress.enabled", False
    )

    if assets_files:
        assets_server_image = ASSETS_SERVER_IMAGE
        assets_server_image_pull_policy = image_pull_policy(assets_server_image)

        assets_server_deployment_body = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "namespace": workshop_namespace,
                "name": "assets-server",
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/environment.services.assets": "true",
                },
            },
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {"deployment": "assets-server"}},
                "strategy": {"type": "Recreate"},
                "template": {
                    "metadata": {
                        "labels": {
                            "deployment": "assets-server",
                            "training.educates.dev/component": "environment",
                            "training.educates.dev/workshop.name": workshop_name,
                            "training.educates.dev/portal.name": portal_name,
                            "training.educates.dev/portal.uid": portal_uid,
                            "training.educates.dev/environment.name": environment_name,
                            "training.educates.dev/environment.uid": environment_uid,
                            "training.educates.dev/environment.services.assets": "true",
                        },
                    },
                    "spec": {
                        "serviceAccountName": "educates-services",
                        "initContainers": [
                            {
                                "name": "download-assets",
                                "image": base_workshop_image,
                                "imagePullPolicy": base_workshop_image_pull_policy,
                                "securityContext": {
                                    "allowPrivilegeEscalation": False,
                                    "capabilities": {"drop": ["ALL"]},
                                    "runAsNonRoot": False,
                                    "runAsUser": 1001,
                                    # "seccompProfile": {"type": "RuntimeDefault"},
                                },
                                "command": ["download-assets"],
                                "volumeMounts": [
                                    {
                                        "name": "data",
                                        "mountPath": "/opt/assets",
                                    },
                                    {
                                        "name": "assets-config",
                                        "mountPath": "/opt/eduk8s/config",
                                    },
                                ],
                            }
                        ],
                        "containers": [
                            {
                                "name": "assets-server",
                                "image": assets_server_image,
                                "imagePullPolicy": assets_server_image_pull_policy,
                                "securityContext": {
                                    "allowPrivilegeEscalation": False,
                                    "capabilities": {"drop": ["ALL"]},
                                    "runAsNonRoot": True,
                                    # "seccompProfile": {"type": "RuntimeDefault"},
                                },
                                "resources": {
                                    "limits": {"memory": assets_memory},
                                    "requests": {"memory": assets_memory},
                                },
                                "ports": [{"containerPort": 8080, "protocol": "TCP"}],
                                "volumeMounts": [
                                    {
                                        "name": "data",
                                        "mountPath": "/opt/app-root/data",
                                        "subPath": "files",
                                    },
                                ],
                            }
                        ],
                        "securityContext": {
                            "runAsUser": 1001,
                            "fsGroup": CLUSTER_STORAGE_GROUP,
                            "supplementalGroups": [CLUSTER_STORAGE_GROUP],
                        },
                        "volumes": [
                            {
                                "name": "assets-config",
                                "configMap": {"name": "assets-server"},
                            },
                        ],
                    },
                },
            },
        }

        if assets_storage:
            assets_server_persistent_volume_claim_body = {
                "apiVersion": "v1",
                "kind": "PersistentVolumeClaim",
                "metadata": {
                    "namespace": workshop_namespace,
                    "name": "assets-server",
                    "labels": {
                        "training.educates.dev/component": "environment",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                    },
                },
                "spec": {
                    "accessModes": ["ReadWriteOnce"],
                    "resources": {"requests": {"storage": assets_storage}},
                },
            }

            if CLUSTER_STORAGE_CLASS:
                assets_server_persistent_volume_claim_body["spec"][
                    "storageClassName"
                ] = CLUSTER_STORAGE_CLASS

            if CLUSTER_STORAGE_USER:
                # This hack is to cope with Kubernetes clusters which don't
                # properly set up persistent volume ownership. IBM Kubernetes is
                # one example. The init container runs as root and sets
                # permissions on the storage and ensures it is group writable.
                # Note that this will only work where pod security policies are
                # not enforced. Don't attempt to use it if they are. If they
                # are, this hack should not be required.

                storage_init_container = {
                    "name": "storage-permissions-initialization",
                    "image": base_workshop_image,
                    "imagePullPolicy": base_workshop_image_pull_policy,
                    "securityContext": {
                        "allowPrivilegeEscalation": False,
                        "capabilities": {"drop": ["ALL"]},
                        "runAsNonRoot": False,
                        "runAsUser": 0,
                        # "seccompProfile": {"type": "RuntimeDefault"},
                    },
                    "command": ["/bin/sh", "-c"],
                    "args": [
                        f"chown {CLUSTER_STORAGE_USER}:{CLUSTER_STORAGE_GROUP} /mnt && chmod og+rwx /mnt"
                    ],
                    "volumeMounts": [{"name": "data", "mountPath": "/mnt"}],
                }

                assets_server_deployment_body["spec"]["template"]["spec"][
                    "initContainers"
                ].insert(0, storage_init_container)

            assets_server_deployment_body["spec"]["template"]["spec"]["volumes"].append(
                {
                    "name": "data",
                    "persistentVolumeClaim": {"claimName": "assets-server"},
                }
            )

            assets_server_objects.extend([assets_server_persistent_volume_claim_body])

        else:
            assets_server_deployment_body["spec"]["template"]["spec"]["volumes"].append(
                {
                    "name": "data",
                    "emptyDir": {},
                }
            )

        assets_server_service_body = {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "namespace": workshop_namespace,
                "name": "assets-server",
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "spec": {
                "type": "ClusterIP",
                "ports": [{"port": 80, "targetPort": 8080}],
                "selector": {"deployment": "assets-server"},
            },
        }

        assets_server_config_map_body = {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
                "name": "assets-server",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "data": {},
        }

        vendir_count = 1

        for assets_files_item in assets_files:
            vendir_config = {
                "apiVersion": "vendir.k14s.io/v1alpha1",
                "kind": "Config",
                "directories": [],
            }

            directories_config = []

            assets_files_item = substitute_variables(
                assets_files_item, environment_downloads_variables
            )
            assets_files_path = assets_files_item.pop("path", ".")
            assets_files_path = os.path.join("/opt/assets/files", assets_files_path)
            assets_files_path = os.path.normpath(assets_files_path)
            assets_files_item["path"] = "."

            directories_config.append(
                {"path": assets_files_path, "contents": [assets_files_item]}
            )

            vendir_config["directories"] = directories_config

            assets_server_config_map_body["data"][
                "vendir-assets-%02d.yaml" % vendir_count
            ] = yaml.dump(vendir_config, Dumper=yaml.Dumper)

            vendir_count += 1

        assets_server_objects.extend(
            [
                assets_server_deployment_body,
                assets_server_service_body,
                assets_server_config_map_body,
            ]
        )

        if assets_ingress_enabled:
            assets_server_host = f"assets-{workshop_namespace}.{INGRESS_DOMAIN}"

            assets_server_ingress_body = {
                "apiVersion": "networking.k8s.io/v1",
                "kind": "Ingress",
                "metadata": {
                    "name": "assets-server",
                    "namespace": workshop_namespace,
                    "annotations": {},
                    "labels": {
                        "training.educates.dev/component": "session",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                    },
                },
                "spec": {
                    "rules": [
                        {
                            "host": assets_server_host,
                            "http": {
                                "paths": [
                                    {
                                        "path": "/",
                                        "pathType": "Prefix",
                                        "backend": {
                                            "service": {
                                                "name": "assets-server",
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

            if INGRESS_SECRET:
                assets_server_ingress_body["metadata"]["annotations"].update(
                    {
                        "ingress.kubernetes.io/force-ssl-redirect": "true",
                        "nginx.ingress.kubernetes.io/ssl-redirect": "true",
                        "nginx.ingress.kubernetes.io/force-ssl-redirect": "true",
                    }
                )

            if INGRESS_SECRET:
                assets_server_ingress_body["spec"]["tls"] = [
                    {
                        "hosts": [assets_server_host],
                        "secretName": INGRESS_SECRET,
                    }
                ]

            assets_server_objects.extend(
                [
                    assets_server_ingress_body,
                ]
            )

        for object_body in assets_server_objects:
            object_body = substitute_variables(object_body, environment_variables)
            kopf.adopt(object_body, namespace_instance.obj)
            create_from_dict(object_body)

    # If sshd access is enabled for the workshop, deploy a ssh tunneling proxy
    # if it also is enabled.

    if applications.is_enabled("sshd") and applications.property(
        "sshd", "tunnel.enabled", False
    ):
        tunnel_objects = []

        tunnel_image = TUNNEL_MANAGER_IMAGE
        tunnel_image_pull_policy = image_pull_policy(tunnel_image)

        tunnel_memory = applications.property("sshd", "tunnel.memory", "128Mi")

        tunnel_service_account_body = {
            "apiVersion": "v1",
            "kind": "ServiceAccount",
            "metadata": {
                "name": "tunnel-manager",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
        }

        tunnel_cluster_role_binding_body = {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "ClusterRoleBinding",
            "metadata": {
                "name": f"educates-tunnel-manager-{workshop_namespace}",
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "roleRef": {
                "apiGroup": "rbac.authorization.k8s.io",
                "kind": "ClusterRole",
                "name": "educates-tunnel-manager",
            },
            "subjects": [
                {
                    "kind": "ServiceAccount",
                    "namespace": workshop_namespace,
                    "name": "tunnel-manager",
                }
            ],
        }

        tunnel_deployment_body = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "namespace": workshop_namespace,
                "name": "tunnel-manager",
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "spec": {
                "replicas": 1,
                "selector": {"matchLabels": {"deployment": "tunnel-manager"}},
                "strategy": {"type": "Recreate"},
                "template": {
                    "metadata": {
                        "labels": {
                            "deployment": "tunnel-manager",
                            "training.educates.dev/component": "environment",
                            "training.educates.dev/workshop.name": workshop_name,
                            "training.educates.dev/portal.name": portal_name,
                            "training.educates.dev/portal.uid": portal_uid,
                            "training.educates.dev/environment.name": environment_name,
                            "training.educates.dev/environment.uid": environment_uid,
                        },
                    },
                    "spec": {
                        "serviceAccountName": "tunnel-manager",
                        "containers": [
                            {
                                "name": "tunnel",
                                "image": tunnel_image,
                                "imagePullPolicy": tunnel_image_pull_policy,
                                "securityContext": {
                                    "allowPrivilegeEscalation": False,
                                    "capabilities": {"drop": ["ALL"]},
                                    "runAsNonRoot": True,
                                    # "seccompProfile": {"type": "RuntimeDefault"},
                                },
                                "resources": {
                                    "limits": {"memory": tunnel_memory},
                                    "requests": {"memory": tunnel_memory},
                                },
                                "ports": [{"containerPort": 8080, "protocol": "TCP"}],
                                "env": [
                                    {"name": "INGRESS_DOMAIN", "value": INGRESS_DOMAIN},
                                    {
                                        "name": "ENVIRONMENT_NAME",
                                        "value": environment_name,
                                    },
                                ],
                            }
                        ],
                        "securityContext": {
                            "runAsUser": 1001,
                            "fsGroup": CLUSTER_STORAGE_GROUP,
                            "supplementalGroups": [CLUSTER_STORAGE_GROUP],
                        },
                    },
                },
            },
        }

        tunnel_service_body = {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "namespace": workshop_namespace,
                "name": "tunnel-manager",
                "labels": {
                    "training.educates.dev/component": "environment",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                },
            },
            "spec": {
                "type": "ClusterIP",
                "ports": [{"port": 8080, "targetPort": 8080}],
                "selector": {"deployment": "tunnel-manager"},
            },
        }

        tunnel_objects.extend(
            [
                tunnel_service_account_body,
                tunnel_cluster_role_binding_body,
                tunnel_deployment_body,
                tunnel_service_body,
            ]
        )

        for object_body in tunnel_objects:
            object_body = substitute_variables(object_body, environment_variables)
            kopf.adopt(object_body, namespace_instance.obj)
            create_from_dict(object_body)

    # If kyverno is being used as the workshop security rules engine then create
    # a policy encapsulating all the restrictions on session namespaces for a
    # workshop.

    if WORKSHOP_SECURITY_RULES_ENGINE == "kyverno":
        for object_body in kyverno_environment_rules(workshop_spec, environment_name):
            kopf.adopt(object_body, namespace_instance.obj)
            create_from_dict(object_body)

    # Create any additional resources required for the workshop, as defined by
    # the workshop resource definition and extras from the workshop environment
    # itself. Where a namespace isn't defined for a namespaced resource type,
    # the resource will be created in the workshop namespace. Any error from
    # this point on will result in the workshop environment being marked as
    # failed.
    #
    # XXX For now make the workshop environment namespace the parent of all
    # objects. Technically should only do so for non namespaced objects, or
    # objects created in namespaces that already existed. How to work out if a
    # resource type is namespaced or not with the Python Kubernetes client
    # appears to be a bit of a hack.

    objects = []

    if workshop_spec.get("environment", {}).get("objects"):
        for application in applications:
            if applications.is_enabled(application):
                objects.extend(
                    environment_objects_list(
                        application, workshop_spec, applications.properties(application)
                    )
                )

        objects.extend(workshop_spec["environment"]["objects"])

    if spec.get("environment", {}).get("objects"):
        objects.extend(spec["environment"]["objects"])

    for object_body in objects:
        # Substitute any data variables in the request objects.

        object_body = substitute_variables(object_body, environment_variables)

        # Set the namespace on the request object to the workshop namespace if
        # it is not already set.

        if not object_body["metadata"].get("namespace"):
            object_body["metadata"]["namespace"] = workshop_namespace

        # Set the labels on the request object to indicate that it was created
        # for this specific workshop environment.

        object_body["metadata"].setdefault("labels", {}).update(
            {
                "training.educates.dev/component": "environment",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/environment.objects": "true",
            }
        )

        # Adopt the request objects so that they are owned by the workshop
        # namespace. This way they will be deleted automatically when the
        # workshop environment and workshop namespace are deleted.

        kopf.adopt(object_body, namespace_instance.obj)

        object_name = object_body["metadata"]["name"]
        object_namespace = object_body["metadata"]["namespace"]
        object_type = object_body["kind"]
        object_api_version = object_body["apiVersion"]

        if object_api_version == "v1" and object_type.lower() == "namespace":
            annotations = object_body["metadata"].setdefault("annotations", {})
            annotations["secretgen.carvel.dev/excluded-from-wildcard-matching"] = ""

        try:
            logger.info(
                "Creating workshop environment object %s of type %s in namespace %s for workshop environment %s.",
                object_name,
                object_type,
                object_namespace,
                environment_name,
            )

            create_from_dict(object_body)

        except Exception as exc:
            logger.exception(
                "Unable to create workshop environments objects, failed creating object %s of type %s in namespace %s for workshop environment %s.",
                object_name,
                object_type,
                object_namespace,
                environment_name,
            )

            report_analytics_event(
                "Resource/PermanentError",
                {
                    "kind": "WorkshopEnvironment",
                    "name": name,
                    "uid": uid,
                    "retry": retry,
                    "message": f"Unable to create workshop environment objects, failed creating object {object_name} of type {object_type} in namespace {object_namespace} for workshop environment {environment_name}.",
                },
            )

            patch["status"] = {
                "educates": {
                    "phase": "Failed",
                    "message": f"Unable to create workshop environment objects, failed creating object {object_name} of type {object_type} in namespace {object_namespace} for workshop environment {environment_name}.",
                }
            }

            raise kopf.PermanentError(
                f"Unable to create workshop environment objects, failed creating object {object_name} of type {object_type} in namespace {object_namespace} for workshop environment {environment_name}."
            ) from exc

    # Report analytics event workshop environment should be ready.

    report_analytics_event(
        "Resource/Ready",
        {"kind": "WorkshopEnvironment", "name": name, "uid": uid, "retry": retry},
    )

    logger.info(
        "Workshop environment %s has been created in namespace %s.",
        environment_name,
        workshop_namespace,
    )

    # Save away the specification of the workshop in the status for the custom
    # resourcse. We will use this later when creating any workshop instances so
    # always working with the same version. Note that we clear the provisional
    # status set in case there was a failure.

    patch["status"] = {}

    patch["status"]["educates"] = {
        "phase": "Running",
        "message": None,
        "namespace": workshop_namespace,
        "workshop": {
            "name": workshop_name,
            "uid": workshop_uid,
            "generation": workshop_generation,
            "spec": workshop_spec,
        },
    }


@kopf.on.delete(
    "training.educates.dev",
    "v1beta1",
    "workshopenvironments",
    optional=True,
)
def workshop_environment_delete(**_):
    """Nothing to do here at this point because the owner references will
    ensure that everything is cleaned up appropriately."""

    # NOTE: This doesn't actually get called because we as we marked it as
    # optional to avoid a finalizer being added to the custom resource, so we
    # use separate generic event handler below to log when the workshop
    # environment is deleted.


@kopf.on.event("training.educates.dev", "v1beta1", "workshopenvironments")
def workshop_environment_event(type, event, **_):  # pylint: disable=redefined-builtin
    """Log when a workshop environment is deleted."""

    if type == "DELETED":
        logger.info(
            "Workshop environment %s has been deleted.",
            event["object"]["metadata"]["name"],
        )
