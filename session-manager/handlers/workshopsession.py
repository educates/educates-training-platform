import base64
import copy
import json
import logging
import random
import string
import time

import bcrypt
import cryptography.hazmat.primitives
import kopf
import pykube
import yaml
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric import rsa

from .analytics import report_analytics_event
from .applications import pod_template_spec_patches, session_objects_list
from .helpers import (Applications, image_pull_policy, smart_overlay_merge,
                      substitute_variables, xget)
from .namespace_budgets import namespace_budgets
from .objects import WorkshopEnvironment, create_from_dict
from .operator_config import (AMPLITUDE_TRACKING_ID, BASE_ENVIRONMENT_IMAGE,
                              CLARITY_TRACKING_ID, CLUSTER_DOMAIN,
                              CLUSTER_SECURITY_POLICY_ENGINE,
                              CLUSTER_STORAGE_CLASS, CLUSTER_STORAGE_GROUP,
                              CLUSTER_STORAGE_USER, DOCKER_IN_DOCKER_IMAGE,
                              DOCKER_REGISTRY_IMAGE, DOCKERD_MIRROR_REMOTE,
                              DOCKERD_MTU, GOOGLE_TRACKING_ID,
                              IMAGE_REPOSITORY, INGRESS_CA_SECRET,
                              INGRESS_CLASS, INGRESS_DOMAIN, INGRESS_PROTOCOL,
                              INGRESS_SECRET, NETWORK_BLOCKCIDRS,
                              PLATFORM_ARCH,
                              RUNTIME_CLASS, SESSION_COOKIE_DOMAIN,
                              resolve_workshop_image)

__all__ = ["workshop_session_create", "workshop_session_delete"]

logger = logging.getLogger("educates.workshopsession")

api = pykube.HTTPClient(pykube.KubeConfig.from_env())


@kopf.index("training.educates.dev", "v1beta1", "workshopsessions")
def workshop_session_index(name, meta, body, **_):
    """Keeps an index of the workshop session. This is used to allow
    workshop sessions to be found when processing a workshop allocation
    request."""

    generation = meta["generation"]

    logger.info(
        "Workshop session %s with generation %s has been cached.", name, generation
    )

    return {(None, name): body}


@kopf.on.resume(
    "training.educates.dev",
    "v1beta1",
    "workshopsessions",
)
def workshop_session_resume(name, **_):
    """Used to acknowledge that there was an existing workshop session
    resource found when the operator started up."""

    logger.info(
        "Workshop session %s has been found but was previously processed.",
        name,
    )


def _setup_session_namespace(
    primary_namespace_body,
    workshop_name,
    portal_name,
    portal_uid,
    environment_name,
    environment_uid,
    session_name,
    workshop_namespace,
    session_namespace,
    target_namespace,
    service_account,
    applications,
    role,
    budget,
    limits,
    security_policy,
):
    # When a namespace is created, it needs to be populated with the default
    # service account, as well as potentially resource quotas and limit ranges.
    # If those aren't created immediately, some of the following steps may fail.
    # At least try to wait for the default service account to be created as it
    # must always exist. Others are more problematic since they may or may not
    # exist.

    for _ in range(25):
        try:
            service_account_instance = pykube.ServiceAccount.objects(
                api, namespace=target_namespace
            ).get(name="default")

        except pykube.exceptions.ObjectDoesNotExist:
            time.sleep(0.1)

        else:
            break

    # Determine which limit ranges and resources quotas to be used.

    if budget != "custom":
        if budget not in namespace_budgets:
            budget = "default"
        elif not namespace_budgets[budget]:
            budget = "default"

    if budget not in ("default", "custom"):
        budget_item = namespace_budgets[budget]

        resource_limits_definition = budget_item["resource-limits"]
        compute_resources_definition = budget_item["compute-resources"]
        compute_resources_timebound_definition = budget_item[
            "compute-resources-timebound"
        ]
        object_counts_definition = budget_item["object-counts"]

        if limits:
            resource_limits_definition = copy.deepcopy(resource_limits_definition)

            container_limits_patch = {"type": "Container"}
            container_limits_patch.update(limits)

            smart_overlay_merge(
                resource_limits_definition["spec"]["limits"],
                [container_limits_patch],
                "type",
            )

    # Delete any limit ranges applied to the namespace that may conflict with
    # the limit range being applied. For the case of custom, we delete any being
    # applied but don't replace it. It is assumed that the session objects for
    # the workshop will define any limit ranges and resource quotas itself.

    if budget != "default":
        for limit_range in pykube.LimitRange.objects(
            api, namespace=target_namespace
        ).all():
            try:
                limit_range.delete()
            except pykube.exceptions.ObjectDoesNotExist:
                pass

    # Delete any resource quotas applied to the namespace that may conflict with
    # the resource quotas being applied.

    if budget != "default":
        for resource_quota in pykube.ResourceQuota.objects(
            api, namespace=target_namespace
        ).all():
            try:
                resource_quota.delete()
            except pykube.exceptions.ObjectDoesNotExist:
                pass

    # If there is a CIDR list of networks to block create a network policy in
    # the target session environment to restrict access from all pods. The
    # customised roles for "admin", "edit" and "view" used below ensure that the
    # network policy objects cannot be deleted from the session namespaces by a
    # user.

    if NETWORK_BLOCKCIDRS:
        network_policy_body = {
            "apiVersion": "networking.k8s.io/v1",
            "kind": "NetworkPolicy",
            "metadata": {
                "name": "educates-network-policy",
                "namespace": target_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
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

        kopf.adopt(network_policy_body, primary_namespace_body)

        NetworkPolicy = pykube.object_factory(
            api, "networking.k8s.io/v1", "NetworkPolicy"
        )

        NetworkPolicy(api, network_policy_body).create()

    # Create role binding in the namespace so the service account under which
    # the workshop environment runs can create resources in it. We only allow a
    # select set of roles which are "admin", "edit", "view" and "cluster-admin".
    # Except for "cluster-admin" these will be mapped to our own version of the
    # respective cluster roles which have any edit access to network policies
    # dropped. If a role is provided we don't know about, we will map it to
    # "view" which because it is the most restrictive will flag more easily that
    # something is wrong in what a workshop defines. If want no role at all to
    # be set up so you can define your own, then can set "custom".

    role_mappings = {
        "admin": "educates-admin-session-role",
        "edit": "educates-edit-session-role",
        "view": "educates-view-session-role",
        "cluster-admin": "cluster-admin",
        "custom": None,
    }

    role_resource_name = role_mappings.get(role)

    if role_resource_name is None and role != "custom":
        role_resource_name = "educates-view-session-role"

    if role_resource_name is not None:
        role_binding_body = {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "RoleBinding",
            "metadata": {
                "name": "educates-session-role",
                "namespace": target_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                },
            },
            "roleRef": {
                "apiGroup": "rbac.authorization.k8s.io",
                "kind": "ClusterRole",
                "name": role_resource_name,
            },
            "subjects": [
                {
                    "kind": "ServiceAccount",
                    "name": service_account,
                    "namespace": workshop_namespace,
                }
            ],
        }

        pykube.RoleBinding(api, role_binding_body).create()

    # Create rolebinding so that all service accounts in the namespace are bound
    # by the specified security policy.

    if CLUSTER_SECURITY_POLICY_ENGINE == "pod-security-policies":
        psp_role_binding_body = {
            "apiVersion": "rbac.authorization.k8s.io/v1",
            "kind": "RoleBinding",
            "metadata": {
                "name": "educates-security-policy",
                "namespace": target_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                },
            },
            "roleRef": {
                "apiGroup": "rbac.authorization.k8s.io",
                "kind": "ClusterRole",
                "name": f"educates-{security_policy}-psp",
            },
            "subjects": [
                {
                    "apiGroup": "rbac.authorization.k8s.io",
                    "kind": "Group",
                    "name": f"system:serviceaccounts:{target_namespace}",
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
                "namespace": target_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                },
            },
            "roleRef": {
                "apiGroup": "rbac.authorization.k8s.io",
                "kind": "ClusterRole",
                "name": f"educates-{security_policy}-scc",
            },
            "subjects": [
                {
                    "apiGroup": "rbac.authorization.k8s.io",
                    "kind": "Group",
                    "name": f"system:serviceaccounts:{target_namespace}",
                }
            ],
        }

        pykube.RoleBinding(api, scc_role_binding_body).create()

    # Create secret which holds image registry '.docker/config.json' and apply
    # it to the default service account in the target namespace so that any
    # deployment using that service account can pull images from the image
    # registry without needing to explicitly add their own image pull secret.

    if applications.is_enabled("registry"):
        registry_host = applications.property("registry", "host")
        registry_username = applications.property("registry", "username")
        registry_password = applications.property("registry", "password")
        registry_auth_token = applications.property("registry", "basic_auth_token")
        registry_secret = applications.property("registry", "secret")

        registry_config = {"auths": {registry_host: {"auth": f"{registry_auth_token}"}}}

        secret_body = {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {
                "name": registry_secret,
                "namespace": target_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                },
            },
            "type": "kubernetes.io/dockerconfigjson",
            "stringData": {".dockerconfigjson": json.dumps(registry_config, indent=4)},
        }

        pykube.Secret(api, secret_body).create()

    # Create limit ranges for the namespace so any deployments will have default
    # memory/cpu min and max values.

    if budget not in ("default", "custom"):
        resource_limits_body = copy.deepcopy(resource_limits_definition)

        resource_limits_body["metadata"].setdefault("labels", {}).update(
            {
                "training.educates.dev/component": "session",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            }
        )

        resource_limits_body["metadata"]["namespace"] = target_namespace

        pykube.LimitRange(api, resource_limits_body).create()

    # Create resource quotas for the namespace so there is a maximum for what
    # resources can be used.

    if budget not in ("default", "custom"):
        resource_quota_body = copy.deepcopy(compute_resources_definition)

        resource_quota_body["metadata"].setdefault("labels", {}).update(
            {
                "training.educates.dev/component": "session",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            }
        )

        resource_quota_body["metadata"]["namespace"] = target_namespace

        pykube.ResourceQuota(api, resource_quota_body).create()

        resource_quota_body = copy.deepcopy(compute_resources_timebound_definition)

        resource_quota_body["metadata"].setdefault("labels", {}).update(
            {
                "training.educates.dev/component": "session",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            }
        )

        resource_quota_body["metadata"]["namespace"] = target_namespace

        pykube.ResourceQuota(api, resource_quota_body).create()

        resource_quota_body = copy.deepcopy(object_counts_definition)

        resource_quota_body["metadata"].setdefault("labels", {}).update(
            {
                "training.educates.dev/component": "session",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            }
        )

        resource_quota_body["metadata"]["namespace"] = target_namespace

        pykube.ResourceQuota(api, resource_quota_body).create()

        # Verify that the status of the resource quotas have been updated. If we
        # don't do this, then the calculated hard limits may not be calculated
        # before we start creating resources in the namespace resulting in a
        # failure. If we can't manage to verify quotas after a period, give up.
        # This may result in a subsequent failure.

        for _ in range(25):
            resource_quotas = pykube.ResourceQuota.objects(
                api, namespace=target_namespace
            ).all()

            for resource_quota in resource_quotas:
                if (
                    not resource_quota.obj.get("status")
                    or not resource_quota.obj["status"].get("used")
                    or not resource_quota.obj["status"].get("hard")
                ):
                    time.sleep(0.1)
                    continue

            break


@kopf.on.create(
    "training.educates.dev",
    "v1beta1",
    "workshopsessions",
)
def workshop_session_create(name, body, meta, uid, spec, status, patch, retry, **_):
    # Report analytics event indicating processing workshop session.

    report_analytics_event(
        "Resource/Create",
        {"kind": "WorkshopSession", "name": name, "uid": uid, "retry": retry},
    )

    if retry > 0:
        logger.info(
            "Workshop session creation request for %s being retried, retries %d.",
            name,
            retry,
        )
    else:
        logger.info("Workshop session creation request for %s being processed.", name)

    # Make sure that if any unexpected error occurs prior to session namespace
    # being created that status is set to Pending indicating the that successful
    # creation based on the custom resource still needs to be done. For any
    # errors occurring after the session namespace has been created we will set
    # a Failed status instead.

    patch["status"] = {"educates": {"phase": "Pending"}}

    # The namespace created for the session is the name of the workshop
    # namespace suffixed by the session ID. By convention this should be
    # the same as what would be used for the name of the session
    # resource definition, but we can't rely on that being the case, as
    # may be different during development and testing, so we construct
    # the name ourself.

    environment_name = spec["environment"]["name"]

    workshop_namespace = environment_name

    session_name = name

    try:
        environment_instance = WorkshopEnvironment.objects(api).get(
            name=workshop_namespace
        )

    except pykube.exceptions.ObjectDoesNotExist as exc:
        patch["status"] = {"educates": {"phase": "Pending"}}
        raise kopf.TemporaryError(
            f"Environment {workshop_namespace} does not exist."
        ) from exc

    session_id = spec["session"]["id"]
    session_namespace = f"{workshop_namespace}-{session_id}"

    environment_uid = environment_instance.obj["metadata"]["uid"]

    # Can optionally be passed name of the training portal via a label
    # when the workshop environment is created as a child to a training
    # portal.

    portal_name = meta.get("labels", {}).get(
        "training.educates.dev/portal.name", ""
    )
    portal_uid = meta.get("labels", {}).get(
        "training.educates.dev/portal.uid", ""
    )

    # We pull details of the workshop to be deployed from the status of the
    # environment custom resource. This is a copy of the specification from the
    # custom resource for the workshop. We use a copy so we aren't affected by
    # changes in the original workshop made after the environment was created.

    if not environment_instance.obj.get("status") or not environment_instance.obj[
        "status"
    ].get("educates"):
        patch["status"] = {"educates": {"phase": "Pending"}}
        raise kopf.TemporaryError(f"Environment {workshop_namespace} is not ready.")

    workshop_name = environment_instance.obj["status"]["educates"]["workshop"][
        "name"
    ]
    workshop_spec = environment_instance.obj["status"]["educates"]["workshop"][
        "spec"
    ]

    workshop_version = workshop_spec.get("version", "latest")

    # Create a wrapper for determining if applications enabled and what
    # configuration they provide. Apply any patches to the workshop config
    # required by enabled applications.

    applications = Applications(workshop_spec["session"].get("applications", {}))

    # Calculate the hostname to be used for this workshop session.

    session_hostname = f"{session_namespace}.{INGRESS_DOMAIN}"
    session_url = f"{INGRESS_PROTOCOL}://{session_hostname}"

    # Calculate session cookie domain to use.

    cookie_domain = environment_instance.obj["spec"].get("cookies", {}).get("domain")

    if not cookie_domain:
        cookie_domain = SESSION_COOKIE_DOMAIN

    # Calculate role, security policy and quota details for primary namespace.

    role = "admin"
    budget = "default"
    limits = {}

    namespace_security_policy = "restricted"

    security_policy_mapping = {
        "restricted": "restricted",
        "baseline": "baseline",
        "privileged": "privileged",
        # Following are obsolete and should not be used.
        "nonroot": "restricted",
        "anyuid": "baseline",
        "custom": "privileged",
    }

    def resolve_security_policy(name):
        return security_policy_mapping.get(name, "restricted")

    if workshop_spec.get("session"):
        role = workshop_spec["session"].get("namespaces", {}).get("role", role)
        budget = workshop_spec["session"].get("namespaces", {}).get("budget", budget)
        limits = workshop_spec["session"].get("namespaces", {}).get("limits", limits)

        namespace_security_policy = resolve_security_policy(
            workshop_spec["session"]
            .get("namespaces", {})
            .get("security", {})
            .get("policy", namespace_security_policy)
        )

    # Generate a random password for the image registry if required.

    characters = string.ascii_letters + string.digits

    if applications.is_enabled("registry"):
        registry_host = f"registry-{session_namespace}.{INGRESS_DOMAIN}"
        registry_username = session_namespace
        registry_password = "".join(random.sample(characters, 32))

        registry_auth_token = (
            base64.b64encode(f"{registry_username}:{registry_password}".encode("utf-8"))
            .decode("ascii")
            .strip()
        )

        registry_secret = "educates-registry-credentials"

        applications.properties("registry")["host"] = registry_host
        applications.properties("registry")["username"] = registry_username
        applications.properties("registry")["password"] = registry_password

        applications.properties("registry")["basic_auth_token"] = registry_auth_token

        applications.properties("registry")["secret"] = registry_secret

    # Generate a random password to be used for any services or applications
    # deployed for a workshop, as well as one specifically for accessing the
    # workshop configuration.

    services_password = "".join(random.sample(characters, 32))

    config_password = spec["session"].get("config", {}).get("password", "")

    if not config_password:
        config_password = "".join(random.sample(characters, 32))

    # Validate that any secrets to be copied into the workshop environment
    # namespace exist. This is done before creating the session namespace so we
    # can fail with a transient error and try again later. Note that we don't
    # check whether any secrets required for workshop downloads are included
    # in this list and will instead let the download of workshop content fail
    # in the running container if any fail so users can know there was a
    # problem.

    environment_secrets = {}

    for secret_item in workshop_spec.get("environment", {}).get("secrets", []):
        try:
            secret = pykube.Secret.objects(api, namespace=workshop_namespace).get(
                name=secret_item["name"]
            )

        except pykube.exceptions.ObjectDoesNotExist as exc:
            patch["status"] = {"educates": {"phase": "Pending"}}
            raise kopf.TemporaryError(
                f"Secret {secret_item['name']} not yet available in {workshop_namespace}.",
                delay=15,
            ) from exc

        except pykube.exceptions.KubernetesError as exc:
            logger.exception(
                "Unexpected error querying secrets in %s.", workshop_namespace
            )
            patch["status"] = {"educates": {"phase": "Pending"}}
            raise kopf.TemporaryError(
                f"Unexpected error querying secrets in {workshop_namespace}."
            ) from exc

        # This will go into a secret later, so we base64 encode values and set
        # keys to be file names to be used when mounted into the container
        # filesystem via a projected volume. Drop the manage fields property
        # so not so much noise.

        secret_obj = copy.deepcopy(secret.obj)
        secret_obj["metadata"].pop("managedFields", None)
        environment_secrets[f"{secret_item['name']}.yaml"] = base64.b64encode(
            yaml.dump(secret_obj, Dumper=yaml.Dumper).encode("utf-8")
        ).decode("utf-8")

    # Create the namespace for everything related to this session. We set the
    # owner of the namespace to be the workshop session resource, but set
    # anything created as part of the workshop session as being owned by
    # the namespace so that the namespace will remain stuck in terminating
    # state until the child resources are deleted. Believe this makes clearer
    # what is going on as you may miss that workshop environment is stuck.

    namespace_body = {
        "apiVersion": "v1",
        "kind": "Namespace",
        "metadata": {
            "name": session_namespace,
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
                "training.educates.dev/policy.engine": CLUSTER_SECURITY_POLICY_ENGINE,
                "training.educates.dev/policy.name": namespace_security_policy,
            },
            "annotations": {"secretgen.carvel.dev/excluded-from-wildcard-matching": ""},
        },
    }

    if CLUSTER_SECURITY_POLICY_ENGINE == "pod-security-standards":
        namespace_body["metadata"]["labels"][
            "pod-security.kubernetes.io/enforce"
        ] = namespace_security_policy

    kopf.adopt(namespace_body)

    try:
        pykube.Namespace(api, namespace_body).create()

    except pykube.exceptions.PyKubeError as exc:
        if exc.code == 409:
            patch["status"] = {"educates": {"phase": "Pending"}}
            raise kopf.TemporaryError(
                f"Namespace {session_namespace} already exists."
            ) from exc
        raise

    try:
        namespace_instance = pykube.Namespace.objects(api).get(name=session_namespace)

    # To get resource uuid for the namespace so can make it the parent of all
    # other resources created, we need to query it back. If this fails something
    # drastic would have had to happen so raise a permanent error.

    except pykube.exceptions.KubernetesError as exc:
        logger.exception("Unexpected error fetching namespace %s.", session_namespace)
        patch["status"] = {"educates": {"phase": "Failed"}}
        raise kopf.PermanentError(
            f"Failed to fetch namespace {session_namespace}."
        ) from exc

    # Generate a SSH key pair for injection into workshop container and any
    # potential services that need it.

    private_key = rsa.generate_private_key(
        public_exponent=65537, key_size=2048
    )

    unencrypted_pem_private_key = private_key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.TraditionalOpenSSL,
        encryption_algorithm=serialization.NoEncryption(),
    )

    rsa_public_key = private_key.public_key().public_bytes(
        encoding=serialization.Encoding.OpenSSH,
        format=serialization.PublicFormat.OpenSSH,
    )

    # pem_public_key = private_key.public_key().public_bytes(
    #     encoding=serialization.Encoding.PEM,
    #     format=serialization.PublicFormat.SubjectPublicKeyInfo,
    # )

    ssh_private_key = unencrypted_pem_private_key.decode("utf-8")
    ssh_public_key = rsa_public_key.decode("utf-8")

    # For unexpected errors beyond this point we will set the status to say
    # things Failed since we can't really recover.

    patch["status"] = {"educates": {"phase": "Failed"}}

    # Create the service account under which the workshop session
    # instance will run. This is created in the workshop namespace. As
    # with the separate namespace, make the session custom resource the
    # parent. We will do this for all objects created for the session as
    # we go along. Name the service account the same as the session
    # namespace now even though this includes the workshop environment
    # name and is contained in the workshop environment.

    service_account = session_namespace

    service_account_body = {
        "apiVersion": "v1",
        "kind": "ServiceAccount",
        "metadata": {
            "name": service_account,
            "namespace": workshop_namespace,
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            },
        },
    }

    kopf.adopt(service_account_body, namespace_instance.obj)

    try:
        pykube.ServiceAccount(api, service_account_body).create()

    except pykube.exceptions.PyKubeError as exc:
        logger.exception(
            "Unexpected error creating service account %s.", service_account
        )
        patch["status"] = {
            "educates": {
                "phase": "Failed",
                "message": f"Failed to create service account {service_account}: {exc}",
            }
        }
        raise kopf.PermanentError(
            f"Failed to create service account {service_account}: {exc}"
        ) from exc

    service_account_token_body = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": f"{service_account}-token",
            "namespace": workshop_namespace,
            "annotations": {"kubernetes.io/service-account.name": service_account},
            "labels": {
                "training.educates.dev/component": "portal",
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
            },
        },
        "type": "kubernetes.io/service-account-token",
    }

    kopf.adopt(service_account_token_body, namespace_instance.obj)

    try:
        pykube.Secret(api, service_account_token_body).create()

    except pykube.exceptions.PyKubeError as exc:
        logger.exception(
            "Unexpected error creating access token %s-token.", service_account
        )
        patch["status"] = {
            "educates": {
                "phase": "Failed",
                "message": f"Failed to create access token {service_account}-token: {exc}",
            }
        }
        raise kopf.PermanentError(
            f"Failed to create access token {service_account}-token: {exc}"
        )

    # Create the rolebinding for this service account to add access to
    # the additional roles that the Kubernetes web console requires.

    cluster_role_binding_body = {
        "apiVersion": "rbac.authorization.k8s.io/v1",
        "kind": "ClusterRoleBinding",
        "metadata": {
            "name": f"educates-web-console-{session_namespace}",
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            },
        },
        "roleRef": {
            "apiGroup": "rbac.authorization.k8s.io",
            "kind": "ClusterRole",
            "name": f"educates-web-console-{workshop_namespace}",
        },
        "subjects": [
            {
                "kind": "ServiceAccount",
                "namespace": workshop_namespace,
                "name": service_account,
            }
        ],
    }

    kopf.adopt(cluster_role_binding_body, namespace_instance.obj)

    try:
        pykube.ClusterRoleBinding(api, cluster_role_binding_body).create()

    except pykube.exceptions.PyKubeError as exc:
        logger.exception(
            "Unexpected error creating cluster role binding %s-web-console-%s.",
            "educates",
            session_namespace,
        )
        patch["status"] = {
            "educates": {
                "phase": "Failed",
                "message": f"Failed to create cluster role binding educates-web-console-{session_namespace}: {exc}",
            }
        }
        raise kopf.PermanentError(
            f"Failed to create cluster role binding educates-web-console-{session_namespace}: {exc}"
        )

    # Setup configuration on the primary session namespace.

    _setup_session_namespace(
        namespace_instance.obj,
        workshop_name,
        portal_name,
        portal_uid,
        environment_name,
        environment_uid,
        session_name,
        workshop_namespace,
        session_namespace,
        session_namespace,
        service_account,
        applications,
        role,
        budget,
        limits,
        namespace_security_policy,
    )

    # List of variables that can be replaced in session objects etc. For those
    # set by applications they are passed through from when the workshop
    # environment was processed. We need to substitute and session variables
    # in those before add them to the final set of session variables.

    image_repository = IMAGE_REPOSITORY

    assets_repository = f"assets-server.{workshop_namespace}.svc.{CLUSTER_DOMAIN}"

    if xget(workshop_spec, "environment.assets.ingress.enabled", False):
        assets_repository = f"assets-{workshop_namespace}.{INGRESS_DOMAIN}"

    oci_image_cache = f"image-cache.{workshop_namespace}.svc.{CLUSTER_DOMAIN}"

    if xget(workshop_spec, "environment.images.ingress.enabled", False):
        oci_image_cache = f"images-{workshop_namespace}.{INGRESS_DOMAIN}"

    image_registry_host = xget(environment_instance.obj, "spec.registry.host")
    image_registry_namespace = xget(environment_instance.obj, "spec.registry.namespace")

    if image_registry_host:
        if image_registry_namespace:
            image_repository = f"{image_registry_host}/{image_registry_namespace}"
        else:
            image_repository = image_registry_host

    ingress_port = "80"

    if INGRESS_PROTOCOL == "https":
        ingress_port = "443"

    session_variables = dict(
        workshop_session_uid=uid,
        platform_arch=PLATFORM_ARCH,
        image_repository=image_repository,
        oci_image_cache=oci_image_cache,
        assets_repository=assets_repository,
        session_id=session_id,
        session_name=session_name,
        session_namespace=session_namespace,
        service_account=service_account,
        workshop_name=workshop_name,
        workshop_version=workshop_version,
        environment_name=environment_name,
        workshop_namespace=workshop_namespace,
        training_portal=portal_name,
        session_url=session_url,
        session_hostname=session_hostname,
        cluster_domain=CLUSTER_DOMAIN,
        ingress_domain=INGRESS_DOMAIN,
        ingress_protocol=INGRESS_PROTOCOL,
        ingress_port=ingress_port,
        ingress_port_suffix="",
        ingress_secret=INGRESS_SECRET,
        ingress_class=INGRESS_CLASS,
        storage_class=CLUSTER_STORAGE_CLASS,
        ssh_private_key=ssh_private_key,
        ssh_public_key=ssh_public_key,
        ssh_keys_secret=f"{session_namespace}-ssh-keys",
        services_password=services_password,
        config_password=config_password,
    )

    base_workshop_image = BASE_ENVIRONMENT_IMAGE
    base_workshop_image_pull_policy = image_pull_policy(base_workshop_image)

    workshop_image = workshop_spec.get("workshop", {}).get(
        "image", workshop_spec.get("content", {}).get("image", "base-environment:*")
    )
    workshop_image = substitute_variables(workshop_image, session_variables)
    workshop_image = resolve_workshop_image(workshop_image)

    workshop_image_pull_policy = image_pull_policy(workshop_image)

    session_variables["workshop_image"] = workshop_image
    session_variables["workshop_image_pull_policy"] = workshop_image_pull_policy

    application_variables_list = workshop_spec.get("session").get("variables", [])

    for variable in application_variables_list:
        session_variables[variable["name"]] = substitute_variables(
            variable["value"], session_variables
        )

    if applications.is_enabled("registry"):
        session_variables.update(
            dict(
                registry_host=registry_host,
                registry_username=registry_username,
                registry_password=registry_password,
                registry_auth_token=registry_auth_token,
                registry_secret=registry_secret,
            )
        )

    # Claim a persistent volume for the workshop session if requested.

    storage = workshop_spec.get("session", {}).get("resources", {}).get("storage")

    storage_volume_name = substitute_variables(
        workshop_spec.get("session", {})
        .get("resources", {})
        .get("volume", {})
        .get("name", ""),
        session_variables,
    )

    storage_volume_subpath = ""

    if storage_volume_name:
        storage = None

        storage_volume_subpath = substitute_variables(
            workshop_spec.get("session", {})
            .get("resources", {})
            .get("volume", {})
            .get("subPath", ""),
            session_variables,
        )

    if storage:
        persistent_volume_claim_body = {
            "apiVersion": "v1",
            "kind": "PersistentVolumeClaim",
            "metadata": {
                "name": session_namespace,
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                },
            },
            "spec": {
                "accessModes": [
                    "ReadWriteOnce",
                ],
                "resources": {
                    "requests": {
                        "storage": storage,
                    }
                },
            },
        }

        if CLUSTER_STORAGE_CLASS:
            persistent_volume_claim_body["spec"][
                "storageClassName"
            ] = CLUSTER_STORAGE_CLASS

        kopf.adopt(persistent_volume_claim_body, namespace_instance.obj)

        pykube.PersistentVolumeClaim(api, persistent_volume_claim_body).create()

    # Create secret containing session variables for later use when allocating
    # a user to a workshop session.

    variables_secret_body = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": f"{session_namespace}-session",
            "namespace": workshop_namespace,
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/component.group": "variables",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            },
        },
        "data": {},
    }

    variables_data = {}

    for key, value in session_variables.items():
        variables_data[key] = base64.b64encode(value.encode("UTF-8")).decode("UTF-8")

    variables_secret_body["data"] = variables_data

    kopf.adopt(variables_secret_body, namespace_instance.obj)

    try:
        pykube.Secret(api, variables_secret_body).create()

    except pykube.exceptions.PyKubeError as exc:
        if exc.code == 409:
            patch["status"] = {"educates": {"phase": "Failed"}}
            raise kopf.TemporaryError(
                f"Session variables secret {session_namespace}-session already exists."
            )
        raise

    # Create any secondary namespaces required for the session.

    namespaces = []

    if workshop_spec.get("session"):
        namespaces = workshop_spec["session"].get("namespaces", {}).get("secondary", [])
        for namespaces_item in namespaces:
            target_namespace = substitute_variables(
                namespaces_item["name"], session_variables
            )

            target_role = namespaces_item.get("role", role)
            target_budget = namespaces_item.get("budget", budget)
            target_limits = namespaces_item.get("limits", {})

            target_security_policy = resolve_security_policy(
                namespaces_item.get("security", {}).get(
                    "policy", namespace_security_policy
                )
            )

            namespace_body = {
                "apiVersion": "v1",
                "kind": "Namespace",
                "metadata": {
                    "name": target_namespace,
                    "labels": {
                        "training.educates.dev/component": "session",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                        "training.educates.dev/session.name": session_name,
                        "training.educates.dev/policy.engine": CLUSTER_SECURITY_POLICY_ENGINE,
                        "training.educates.dev/policy.name": target_security_policy,
                    },
                    "annotations": {
                        "secretgen.carvel.dev/excluded-from-wildcard-matching": ""
                    },
                },
            }

            if CLUSTER_SECURITY_POLICY_ENGINE == "pod-security-standards":
                namespace_body["metadata"]["labels"][
                    "pod-security.kubernetes.io/enforce"
                ] = target_security_policy

            kopf.adopt(namespace_body, namespace_instance.obj)

            try:
                pykube.Namespace(api, namespace_body).create()

            except pykube.exceptions.PyKubeError as exc:
                if exc.code == 409:
                    patch["status"] = {"educates": {"phase": "Failed"}}
                    raise kopf.TemporaryError(
                        f"Secondary namespace {target_namespace} already exists."
                    )
                raise

            _setup_session_namespace(
                namespace_instance.obj,
                workshop_name,
                portal_name,
                portal_uid,
                environment_name,
                environment_uid,
                session_name,
                workshop_namespace,
                session_namespace,
                target_namespace,
                service_account,
                applications,
                target_role,
                target_budget,
                target_limits,
                target_security_policy,
            )

    # Create any additional resource objects required for the session.
    #
    # XXX For now make the session resource definition the parent of
    # all objects. Technically should only do so for non namespaced
    # objects, or objects created in namespaces that already existed.
    # How to work out if a resource type is namespaced or not with the
    # Python Kubernetes client appears to be a bit of a hack.

    objects = []

    for application in applications:
        if applications.is_enabled(application):
            objects.extend(
                session_objects_list(
                    application, workshop_spec, applications.properties(application)
                )
            )

    if workshop_spec.get("session"):
        objects.extend(workshop_spec["session"].get("objects", []))

    for object_body in objects:
        object_body = substitute_variables(object_body, session_variables)

        if not object_body["metadata"].get("namespace"):
            object_body["metadata"]["namespace"] = session_namespace

        object_name = object_body["metadata"]["name"]
        object_namespace = object_body["metadata"]["namespace"]
        object_type = object_body["kind"]
        object_api_version = object_body["apiVersion"]

        object_body["metadata"].setdefault("labels", {}).update(
            {
                "training.educates.dev/component": "session",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
                "training.educates.dev/session.objects": "true",
            }
        )

        kopf.adopt(object_body, namespace_instance.obj)

        if object_api_version == "v1" and object_type.lower() == "namespace":
            annotations = object_body["metadata"].setdefault("annotations", {})

            annotations["secretgen.carvel.dev/excluded-from-wildcard-matching"] = ""

            target_role = annotations.get(
                "training.educates.dev/session.role", role
            )

            target_security_policy = resolve_security_policy(
                annotations.get(
                    "training.educates.dev/session.policy",
                    namespace_security_policy,
                )
            )

            object_body["metadata"].setdefault("labels", {}).update(
                {
                    "training.educates.dev/policy.engine": CLUSTER_SECURITY_POLICY_ENGINE,
                    "training.educates.dev/policy.name": target_security_policy,
                }
            )

            if CLUSTER_SECURITY_POLICY_ENGINE == "pod-security-standards":
                object_body["metadata"]["labels"][
                    "pod-security.kubernetes.io/enforce"
                ] = target_security_policy

            target_budget = annotations.get(
                "training.educates.dev/session.budget", budget
            )

            target_limits = {}

            if annotations.get("training.educates.dev/session.limits.min.cpu"):
                target_limits.setdefault("min", {})["cpu"] = annotations[
                    "training.educates.dev/session.limits.min.cpu"
                ]
            if annotations.get(
                "training.educates.dev/session.limits.min.memory"
            ):
                target_limits.setdefault("min", {})["memory"] = annotations[
                    "training.educates.dev/session.limits.min.memory"
                ]

            if annotations.get("training.educates.dev/session.limits.max.cpu"):
                target_limits.setdefault("max", {})["cpu"] = annotations[
                    "training.educates.dev/session.limits.max.cpu"
                ]
            if annotations.get(
                "training.educates.dev/session.limits.max.memory"
            ):
                target_limits.setdefault("max", {})["memory"] = annotations[
                    "training.educates.dev/session.limits.max.memory"
                ]

            if annotations.get(
                "training.educates.dev/session.limits.defaultrequest.cpu"
            ):
                target_limits.setdefault("defaultRequest", {})["cpu"] = annotations[
                    "training.educates.dev/session.limits.defaultrequest.cpu"
                ]
            if annotations.get(
                "training.educates.dev/session.limits.defaultrequest.memory"
            ):
                target_limits.setdefault("defaultRequest", {})["memory"] = annotations[
                    "training.educates.dev/session.limits.defaultrequest.memory"
                ]

            if annotations.get(
                "training.educates.dev/session.limits.default.cpu"
            ):
                target_limits.setdefault("default", {})["cpu"] = annotations[
                    "training.educates.dev/session.limits.default.cpu"
                ]
            if annotations.get(
                "training.educates.dev/session.limits.default.memory"
            ):
                target_limits.setdefault("default", {})["memory"] = annotations[
                    "training.educates.dev/session.limits.default.memory"
                ]

            logger.info(
                "Creating workshop session object %s of type %s in namespace %s for workshop session %s.",
                object_name,
                object_type,
                object_namespace,
                session_name,
            )

            try:
                create_from_dict(object_body)

            except Exception as exc:
                logger.exception(
                    "Unable to create workshop session objects, failed creating object %s of type %s in namespace %s for workshop session %s.",
                    object_name,
                    object_type,
                    object_namespace,
                    session_name,
                )

                report_analytics_event(
                    "Resource/PermanentError",
                    {
                        "kind": "WorkshopSession",
                        "name": name,
                        "uid": uid,
                        "retry": retry,
                        "message": f"Unable to create workshop session objects, failed creating object {object_name} of type {object_type} in namespace {object_namespace} for workshop session {session_name}.",
                    },
                )

                patch["status"] = {
                    "educates": {
                        "phase": "Failed",
                        "message": f"Unable to create workshop session objects, failed creating object {object_name} of type {object_type} in namespace {object_namespace} for workshop session {session_name}.",
                    }
                }

                raise kopf.PermanentError(
                    f"Unable to create workshop session objects, failed creating object {object_name} of type {object_type} in namespace {object_namespace} for workshop session {session_name}."
                ) from exc

            target_namespace = object_body["metadata"]["name"]

            _setup_session_namespace(
                namespace_instance.obj,
                workshop_name,
                portal_name,
                portal_uid,
                environment_name,
                environment_uid,
                session_name,
                workshop_namespace,
                session_namespace,
                target_namespace,
                service_account,
                applications,
                target_role,
                target_budget,
                target_limits,
                target_security_policy,
            )

        else:
            logger.info(
                "Creating workshop session object %s of type %s in namespace %s for workshop session %s.",
                object_name,
                object_type,
                object_namespace,
                session_name,
            )

            try:
                create_from_dict(object_body)

            except Exception as exc:
                logger.exception(
                    "Unable to create workshop session objects, failed creating object %s of type %s in namespace %s for workshop session %s.",
                    object_name,
                    object_type,
                    object_namespace,
                    session_name,
                )

                report_analytics_event(
                    "Resource/PermanentError",
                    {
                        "kind": "WorkshopSession",
                        "name": name,
                        "uid": uid,
                        "retry": retry,
                        "message": f"Unable to create workshop session objects, failed creating object {object_name} of type {object_type} in namespace {object_namespace} for workshop session {session_name}.",
                    },
                )

                patch["status"] = {
                    "educates": {
                        "phase": "Failed",
                        "message": f"Unable to create workshop session objects, failed creating object {object_name} of type {object_type} in namespace {object_namespace} for workshop session {session_name}.",
                    }
                }

                raise kopf.PermanentError(
                    f"Unable to create workshop session objects, failed creating object {object_name} of type {object_type} in namespace {object_namespace} for workshop session {session_name}."
                ) from exc

        if object_api_version == "v1" and object_type.lower() == "resourcequota":
            # Verify that the status of the resource quota has been
            # updated. If we don't do this, then the calculated hard
            # limits may not be calculated before we start creating
            # resources in the namespace resulting in a failure. If we
            # can't manage to verify quotas after a period, give up.
            # This may result in a subsequent failure.

            for _ in range(25):
                resource_quota = pykube.ResourceQuota.objects(
                    api, namespace=object_body["metadata"]["namespace"]
                ).get(name=object_body["metadata"]["name"])

                if (
                    not resource_quota.obj.get("status")
                    or not resource_quota.obj["status"].get("used")
                    or not resource_quota.obj["status"].get("hard")
                ):
                    time.sleep(0.1)
                    continue

    # Work out the name of the workshop config secret to use for a session. This
    # will usually be the common workshop-config secret created with the
    # workshop environment, but if the request.objects contains a secret with
    # name same as $(session_name)-config, then use that instead.

    workshop_config_secret_name = "workshop-config"

    request_objects = workshop_spec.get("request", {}).get("objects", [])

    for object_body in request_objects:
        if object_body["kind"] == "Secret":
            object_name = substitute_variables(
                object_body["metadata"]["name"], session_variables
            )
            if object_name == f"{session_name}-config":
                workshop_config_secret_name = f"{session_name}-config"
                break

    # Next setup the deployment resource for the workshop dashboard. Note that
    # spec.content.image is deprecated and should use spec.workshop.image. We
    # will check both.

    username = spec["session"].get("username", "")
    password = spec["session"].get("password", "")

    default_memory = "512Mi"

    if applications.is_enabled("editor"):
        default_memory = "1Gi"

    workshop_memory = (
        workshop_spec.get("session", {})
        .get("resources", {})
        .get("memory", default_memory)
    )

    google_tracking_id = (
        spec.get("analytics", {})
        .get("google", {})
        .get("trackingId", GOOGLE_TRACKING_ID)
    )

    clarity_tracking_id = (
        spec.get("analytics", {})
        .get("clarity", {})
        .get("trackingId", CLARITY_TRACKING_ID)
    )

    amplitude_tracking_id = (
        spec.get("analytics", {})
        .get("amplitude", {})
        .get("trackingId", AMPLITUDE_TRACKING_ID)
    )

    workshop_env_from = workshop_spec.get("session", {}).get("envFrom", [])
    workshop_env_from = substitute_variables(workshop_env_from, session_variables)

    deployment_body = {
        "apiVersion": "apps/v1",
        "kind": "Deployment",
        "metadata": {
            "name": session_namespace,
            "namespace": workshop_namespace,
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/application": "workshop",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
                "training.educates.dev/session.services.workshop": "true",
            },
        },
        "spec": {
            "replicas": 1,
            "selector": {"matchLabels": {"deployment": session_namespace}},
            "strategy": {"type": "Recreate"},
            "template": {
                "metadata": {
                    "labels": {
                        "deployment": session_namespace,
                        "training.educates.dev/component": "session",
                        "training.educates.dev/application": "workshop",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                        "training.educates.dev/session.name": session_name,
                        "training.educates.dev/session.services.workshop": "true",
                    },
                },
                "spec": {
                    "serviceAccountName": service_account,
                    "securityContext": {
                        "fsGroup": CLUSTER_STORAGE_GROUP,
                        "supplementalGroups": [CLUSTER_STORAGE_GROUP],
                    },
                    "initContainers": [],
                    "containers": [
                        {
                            "name": "workshop",
                            "image": workshop_image,
                            "imagePullPolicy": workshop_image_pull_policy,
                            "securityContext": {
                                "allowPrivilegeEscalation": False,
                                "capabilities": {"drop": ["ALL"]},
                                "runAsNonRoot": True,
                                # "seccompProfile": {"type": "RuntimeDefault"},
                                "privileged": False,
                            },
                            "resources": {
                                "requests": {"memory": workshop_memory},
                                "limits": {"memory": workshop_memory},
                            },
                            "ports": [
                                {
                                    "name": "10080-tcp",
                                    "containerPort": 10080,
                                    "protocol": "TCP",
                                }
                            ],
                            "envFrom": workshop_env_from,
                            "env": [
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
                                    "name": "ENVIRONMENT_NAME",
                                    "value": environment_name,
                                },
                                {
                                    "name": "WORKSHOP_NAME",
                                    "value": workshop_name,
                                },
                                {
                                    "name": "WORKSHOP_VERSION",
                                    "value": workshop_version,
                                },
                                {
                                    "name": "WORKSHOP_NAMESPACE",
                                    "value": workshop_namespace,
                                },
                                {
                                    "name": "SESSION_NAMESPACE",
                                    "value": session_namespace,
                                },
                                {
                                    "name": "SESSION_NAME",
                                    "value": session_name,
                                },
                                {
                                    "name": "SESSION_ID",
                                    "value": session_id,
                                },
                                {
                                    "name": "SESSION_URL",
                                    "value": session_url,
                                },
                                {
                                    "name": "SESSION_HOSTNAME",
                                    "value": session_hostname,
                                },
                                {
                                    "name": "AUTH_USERNAME",
                                    "value": username,
                                },
                                {
                                    "name": "AUTH_PASSWORD",
                                    "value": password,
                                },
                                {
                                    "name": "GATEWAY_PORT",
                                    "value": "10080",
                                },
                                {
                                    "name": "CLUSTER_DOMAIN",
                                    "value": CLUSTER_DOMAIN,
                                },
                                {
                                    "name": "INGRESS_DOMAIN",
                                    "value": INGRESS_DOMAIN,
                                },
                                {
                                    "name": "INGRESS_PORT_SUFFIX",
                                    "value": "",
                                },
                                {"name": "INGRESS_PROTOCOL", "value": INGRESS_PROTOCOL},
                                {
                                    "name": "SESSION_COOKIE_DOMAIN",
                                    "value": cookie_domain,
                                },
                                {
                                    "name": "IMAGE_REPOSITORY",
                                    "value": image_repository,
                                },
                                {
                                    "name": "OCI_IMAGE_CACHE",
                                    "value": oci_image_cache,
                                },
                                {
                                    "name": "ASSETS_REPOSITORY",
                                    "value": assets_repository,
                                },
                                {"name": "INGRESS_CLASS", "value": INGRESS_CLASS},
                                {
                                    "name": "STORAGE_CLASS",
                                    "value": CLUSTER_STORAGE_CLASS,
                                },
                                {
                                    "name": "POLICY_ENGINE",
                                    "value": CLUSTER_SECURITY_POLICY_ENGINE,
                                },
                                {
                                    "name": "POLICY_NAME",
                                    "value": namespace_security_policy,
                                },
                                {
                                    "name": "SERVICES_PASSWORD",
                                    "value": services_password,
                                },
                                {
                                    "name": "CONFIG_PASSWORD",
                                    "value": config_password,
                                },
                            ],
                            "volumeMounts": [
                                {
                                    "name": "workshop-config",
                                    "mountPath": "/opt/eduk8s/config",
                                },
                                {
                                    "name": "workshop-theme",
                                    "mountPath": "/opt/eduk8s/theme",
                                },
                            ],
                        },
                    ],
                    "volumes": [
                        {
                            "name": "workshop-config",
                            "secret": {"secretName": workshop_config_secret_name},
                        },
                        {
                            "name": "workshop-theme",
                            "secret": {"secretName": "workshop-theme"},
                        },
                    ],
                    "hostAliases": [],
                },
            },
        },
    }

    deployment_pod_template_spec = deployment_body["spec"]["template"]["spec"]

    if RUNTIME_CLASS:
        deployment_pod_template_spec["runtimeClassName"] = RUNTIME_CLASS

    token_enabled = (
        workshop_spec["session"]
        .get("namespaces", {})
        .get("security", {})
        .get("token", {})
        .get("enabled", True)
    )

    deployment_pod_template_spec["automountServiceAccountToken"] = False

    deployment_pod_template_spec["volumes"].append(
        {
            "name": "cluster-token",
            "secret": {"secretName": f"{session_namespace}-token"},
        },
    )

    if token_enabled:
        deployment_pod_template_spec["containers"][0]["volumeMounts"].append(
            {
                "name": "cluster-token",
                "mountPath": "/var/run/secrets/kubernetes.io/serviceaccount",
                "readOnly": True,
            },
        )

    if INGRESS_CA_SECRET:
        deployment_pod_template_spec["volumes"].extend(
            [
                {
                    "name": "workshop-ca",
                    "secret": {
                        "secretName": INGRESS_CA_SECRET,
                    },
                },
                {
                    "name": "workshop-ca-trust",
                    "emptyDir": {},
                },
            ]
        )

        certificates_init_container = {
            "name": "ca-trust-store-initialization",
            "image": base_workshop_image,
            "imagePullPolicy": base_workshop_image_pull_policy,
            "securityContext": {
                "allowPrivilegeEscalation": False,
                # Not sure why can't drop all capabilities here.
                # "capabilities": {"drop": ["ALL"]},
                "runAsNonRoot": False,
                "runAsUser": 0,
                # "seccompProfile": {"type": "RuntimeDefault"},
            },
            "command": ["/opt/eduk8s/sbin/setup-certificates"],
            "resources": {
                "requests": {"memory": workshop_memory},
                "limits": {"memory": workshop_memory},
            },
            "volumeMounts": [
                {
                    "name": "workshop-ca",
                    "mountPath": "/etc/pki/ca-trust/source/anchors/Cluster_Ingress_CA.pem",
                    # "readOnly": True,
                    "subPath": "ca.crt",
                },
                {"name": "workshop-ca-trust", "mountPath": "/mnt"},
            ],
        }

        deployment_pod_template_spec["initContainers"].append(
            certificates_init_container
        )

        deployment_pod_template_spec["containers"][0]["volumeMounts"].append(
            {
                "name": "workshop-ca-trust",
                "mountPath": "/etc/pki/ca-trust",
                "readOnly": True,
            },
        )

    workshop_volume_subpath = "home/eduk8s"

    if storage_volume_subpath:
        workshop_volume_subpath = f"{storage_volume_subpath}/{workshop_volume_subpath}"

    assets_volume_subpath = "opt/assets"

    if storage_volume_subpath:
        assets_volume_subpath = f"{storage_volume_subpath}/{assets_volume_subpath}"

    packages_volume_subpath = "opt/packages"

    if storage_volume_subpath:
        packages_volume_subpath = f"{storage_volume_subpath}/{packages_volume_subpath}"

    git_repos_volume_subpath = "opt/git/repositories"

    if storage_volume_subpath:
        git_repos_volume_subpath = (
            f"{storage_volume_subpath}/{git_repos_volume_subpath}"
        )

    if storage_volume_name:
        deployment_pod_template_spec["volumes"].append(
            {
                "name": "workshop-data",
                "persistentVolumeClaim": {"claimName": storage_volume_name},
            }
        )

    elif storage:
        deployment_pod_template_spec["volumes"].append(
            {
                "name": "workshop-data",
                "persistentVolumeClaim": {"claimName": session_namespace},
            }
        )

    # elif applications.is_enabled("docker"):
    #     if storage_volume_name:
    #         deployment_pod_template_spec["volumes"].append(
    #             {
    #                 "name": "workshop-data",
    #                 "persistentVolumeClaim": {"claimName": storage_volume_name},
    #             }
    #         )
    #     else:
    #         deployment_pod_template_spec["volumes"].append(
    #             {"name": "workshop-data", "emptyDir": {}}
    #         )

    else:
        deployment_pod_template_spec["volumes"].append(
            {"name": "workshop-data", "emptyDir": {}}
        )

    deployment_pod_template_spec["containers"][0]["volumeMounts"].extend(
        [
            {
                "name": "workshop-data",
                "mountPath": "/home/eduk8s",
                "subPath": workshop_volume_subpath,
            },
            {
                "name": "workshop-data",
                "mountPath": "/opt/assets",
                "subPath": assets_volume_subpath,
            },
            {
                "name": "workshop-data",
                "mountPath": "/opt/packages",
                "subPath": packages_volume_subpath,
            },
            {
                "name": "workshop-data",
                "mountPath": "/opt/git/repositories",
                "subPath": git_repos_volume_subpath,
            },
        ]
    )

    # Since using at least an emptyDir for workshop user home directory we
    # must always use an init container to copy the home directory from the
    # workshop image to the volume.

    if CLUSTER_STORAGE_USER:
        # This hack is to cope with Kubernetes clusters which don't properly
        # set up persistent volume ownership. IBM Kubernetes is one example.
        # The init container runs as root and sets permissions on the
        # storage and ensures it is group writable. Note that this will only
        # work where pod security policies are not enforced. Don't attempt
        # to use it if they are. If they are, this hack should not be
        # required.

        volume_init_container = {
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
            "resources": {
                "requests": {"memory": workshop_memory},
                "limits": {"memory": workshop_memory},
            },
            "volumeMounts": [{"name": "workshop-data", "mountPath": "/mnt"}],
        }

        deployment_pod_template_spec["initContainers"].append(volume_init_container)

    workshop_init_container = {
        "name": "workshop-volume-initialization",
        "image": workshop_image,
        "imagePullPolicy": workshop_image_pull_policy,
        "securityContext": {
            "allowPrivilegeEscalation": False,
            "capabilities": {"drop": ["ALL"]},
            "runAsNonRoot": True,
            # "seccompProfile": {"type": "RuntimeDefault"},
        },
        "command": [
            "/opt/eduk8s/sbin/setup-volume",
            "/home/eduk8s",
            f"/mnt/{workshop_volume_subpath}",
        ],
        "resources": {
            "requests": {"memory": workshop_memory},
            "limits": {"memory": workshop_memory},
        },
        "volumeMounts": [{"name": "workshop-data", "mountPath": "/mnt"}],
    }

    deployment_pod_template_spec["initContainers"].append(workshop_init_container)

    # Work out whether workshop downloads require any secrets and if so we
    # create an init container and perform workshop downloads from that rather
    # than the main container so we don't need to expose secrets to the workshop
    # user.

    def vendir_secrets_required(contents):
        for content in contents:
            if content.get("git", {}).get("secretRef"):
                return True
            if (
                content.get("git", {})
                .get("verification", {})
                .get("publicKeysSecretRef")
            ):
                return True
            elif content.get("hg", {}).get("secretRef"):
                return True
            elif content.get("http", {}).get("secretRef"):
                return True
            elif content.get("image", {}).get("secretRef"):
                return True
            elif content.get("imgpkgBundle", {}).get("secretRef"):
                return True
            elif content.get("githubRelease", {}).get("secretRef"):
                return True
            elif content.get("helmChart", {}).get("repository", {}).get("secretRef"):
                return True
            elif content.get("inline", {}).get("pathsFrom", []):
                return True

    workshop_files = workshop_spec.get("workshop", {}).get("files", [])

    need_secrets = vendir_secrets_required(workshop_files)

    if not need_secrets:
        for package in workshop_spec.get("workshop", {}).get("packages", []):
            package_files = package.get("files", [])
            need_secrets = vendir_secrets_required(package_files)
            if need_secrets:
                break

    if need_secrets and environment_secrets:
        # Need download secrets, so we need create a new secret which is a
        # composite of all the other secrets so we can mount the full Kubernetes
        # resource definitions for the secrets into the init container.

        secret_body = {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {
                "name": f"{session_namespace}-vendir-secrets",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                },
            },
            "data": environment_secrets,
        }

        kopf.adopt(secret_body, namespace_instance.obj)

        pykube.Secret(api, secret_body).create()

        deployment_pod_template_spec["volumes"].append(
            {
                "name": "vendir-secrets",
                "secret": {"secretName": f"{session_namespace}-vendir-secrets"},
            }
        )

        downloads_init_container = {
            "name": "workshop-downloads-initialization",
            "image": base_workshop_image,
            "imagePullPolicy": base_workshop_image_pull_policy,
            "securityContext": {
                "allowPrivilegeEscalation": False,
                "capabilities": {"drop": ["ALL"]},
                "runAsNonRoot": True,
                # "seccompProfile": {"type": "RuntimeDefault"},
            },
            "command": ["/opt/eduk8s/sbin/setup-downloads"],
            "resources": {
                "requests": {"memory": workshop_memory},
                "limits": {"memory": workshop_memory},
            },
            "volumeMounts": [
                {
                    "name": "workshop-data",
                    "mountPath": "/opt/assets",
                    "subPath": assets_volume_subpath,
                },
                {
                    "name": "workshop-data",
                    "mountPath": "/opt/packages",
                    "subPath": packages_volume_subpath,
                },
                {"name": "vendir-secrets", "mountPath": "/opt/secrets"},
                {"name": "workshop-config", "mountPath": "/opt/eduk8s/config"},
            ],
        }

        if INGRESS_CA_SECRET:
            downloads_init_container["volumeMounts"].append(
                {
                    "name": "workshop-ca-trust",
                    "mountPath": "/etc/pki/ca-trust",
                    "readOnly": True,
                },
            )

        deployment_pod_template_spec["initContainers"].append(downloads_init_container)

    # Append any init containers specified in the workshop definition.

    session_init_containers = workshop_spec["session"].get("initContainers", [])

    session_init_containers = substitute_variables(
        session_init_containers, session_variables
    )

    deployment_pod_template_spec["initContainers"].extend(session_init_containers)

    # Apply any patches for the pod specification for the deployment which
    # are specified in the workshop resource definition. This would be used
    # to set resources and setup volumes. If the target item is a list, look
    # for items within that which have a name field that matches a named item
    # in the patch and attempt to merge that with one in the target, but
    # don't do this if the item in the target was added by the patch as
    # that is likely an attempt to deliberately add two named items, such
    # as in the case of volume mounts.

    for application in applications:
        if applications.is_enabled(application):
            deployment_patch = pod_template_spec_patches(
                application, workshop_spec, applications.properties(application)
            )
            deployment_patch = substitute_variables(deployment_patch, session_variables)
            smart_overlay_merge(deployment_pod_template_spec, deployment_patch)

    if workshop_spec.get("session"):
        deployment_patch = workshop_spec["session"].get("patches", {})
        deployment_patch = substitute_variables(deployment_patch, session_variables)
        smart_overlay_merge(deployment_pod_template_spec, deployment_patch)

    # Apply any environment variable overrides for the workshop/environment.

    def _apply_environment_patch(patch):
        if not patch:
            return

        patch = substitute_variables(patch, session_variables)

        if deployment_pod_template_spec["containers"][0].get("env") is None:
            deployment_pod_template_spec["containers"][0]["env"] = patch
        else:
            smart_overlay_merge(
                deployment_pod_template_spec["containers"][0]["env"],
                patch,
            )

    if workshop_spec.get("session"):
        _apply_environment_patch(workshop_spec["session"].get("env", []))

    _apply_environment_patch(spec["session"].get("env", []))

    # Add additional labels for any applications which have been enabled.

    additional_env = []
    additional_labels = {}

    for application in applications:
        if applications.is_enabled(application):
            additional_labels[
                "training.educates.dev/session.applications.{application.lower()}"
            ] = "true"

    # Add in extra configuation for web console.

    if applications.is_enabled("console"):
        if applications.property("console", "vendor", "kubernetes") == "kubernetes":
            secret_body = {
                "apiVersion": "v1",
                "kind": "Secret",
                "metadata": {
                    "name": "kubernetes-dashboard-csrf",
                    "namespace": session_namespace,
                    "labels": {
                        "training.educates.dev/component": "session",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                        "training.educates.dev/session.name": session_name,
                    },
                },
            }

            pykube.Secret(api, secret_body).create()

    # Add in extra configuration for special cases, as well as bind policy.

    resource_objects = []

    if applications.is_enabled("docker"):
        docker_volumes = [{"name": "docker-socket", "emptyDir": {}}]

        docker_volume_subpath = "var/lib/docker"

        if storage_volume_subpath:
            docker_volume_subpath = f"{storage_volume_subpath}/{docker_volume_subpath}"

        if not storage_volume_name:
            docker_volumes.append(
                {
                    "name": "docker-data",
                    "persistentVolumeClaim": {
                        "claimName": f"{session_namespace}-docker"
                    },
                }
            )

        deployment_pod_template_spec["volumes"].extend(docker_volumes)

        docker_compose = applications.property("docker", "compose", {})
        docker_socket = applications.property("docker", "socket.enabled", None)

        if docker_socket or (docker_socket is None and not docker_compose):
            docker_workshop_volume_mounts = [
                {
                    "name": "docker-socket",
                    "mountPath": "/var/run/docker",
                    "readOnly": True,
                },
            ]

            deployment_pod_template_spec["containers"][0]["volumeMounts"].extend(
                docker_workshop_volume_mounts
            )

        docker_memory = applications.property("docker", "memory", "768Mi")
        docker_storage = applications.property("docker", "storage", "5Gi")

        dockerd_image = DOCKER_IN_DOCKER_IMAGE
        dockerd_image_pull_policy = image_pull_policy(dockerd_image)

        # Build args for dockerd daemon ahead of inection into the pod's args list

        dockerd_daemon_args = ["--host=unix:///var/run/workshop/docker.sock", f"--mtu={DOCKERD_MTU}"]

        if applications.is_enabled("registry"):
            if not INGRESS_SECRET:
                dockerd_daemon_args.append(f"--insecure-registry={registry_host}")

        if DOCKERD_MIRROR_REMOTE:
            dockerd_daemon_args.extend(
                [
                    f"--insecure-registry={workshop_namespace}-mirror",
                    f"--registry-mirror=http://{workshop_namespace}-mirror:5000",
                ]
            )

        dockerd_args = [
            "/bin/sh",
            "-c",
            f"mkdir -p /var/run/workshop && ln -s /var/run/workshop/docker.sock /var/run/docker.sock && (test -f /usr/local/share/ca-certificates/Cluster_Ingress_CA.crt && /usr/sbin/update-ca-certificates || true) && dockerd {' '.join(dockerd_daemon_args)}",
        ]

        docker_container = {
            "name": "docker",
            "image": dockerd_image,
            "imagePullPolicy": dockerd_image_pull_policy,
            "args": dockerd_args,
            "securityContext": {
                "allowPrivilegeEscalation": True,
                "privileged": True,
                "runAsUser": 0,
                "capabilities": {"drop": ["KILL", "MKNOD", "SETUID", "SETGID"]},
                # "seccompProfile": {"type": "RuntimeDefault"},
            },
            "resources": {
                "limits": {"memory": docker_memory},
                "requests": {"memory": docker_memory},
            },
            "volumeMounts": [
                {
                    "name": "docker-socket",
                    "mountPath": "/var/run/workshop",
                },
                {
                    "name": "workshop-data",
                    "mountPath": "/home/eduk8s",
                    "subPath": workshop_volume_subpath,
                },
            ],
        }

        if storage_volume_name:
            docker_container["volumeMounts"].append(
                {
                    "name": "workshop-data",
                    "mountPath": "/var/lib/docker",
                    "subPath": docker_volume_subpath,
                }
            )
        else:
            docker_container["volumeMounts"].append(
                {
                    "name": "docker-data",
                    "mountPath": "/var/lib/docker",
                    "subPath": docker_volume_subpath,
                }
            )

        if INGRESS_CA_SECRET:
            docker_container["volumeMounts"].append(
                {
                    "name": "workshop-ca",
                    "mountPath": "/usr/local/share/ca-certificates/Cluster_Ingress_CA.crt",
                    # "readOnly": True,
                    "subPath": "ca.crt",
                },
            )

        deployment_pod_template_spec["containers"].append(docker_container)

        deployment_body["metadata"]["labels"].update(
            {"training.educates.dev/session.services.docker": "true"}
        )
        deployment_body["spec"]["template"]["metadata"]["labels"].update(
            {"training.educates.dev/session.services.docker": "true"}
        )

        if not storage_volume_name:
            docker_persistent_volume_claim = {
                "apiVersion": "v1",
                "kind": "PersistentVolumeClaim",
                "metadata": {
                    "name": f"{session_namespace}-docker",
                    "namespace": workshop_namespace,
                    "labels": {
                        "training.educates.dev/component": "session",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                        "training.educates.dev/session.name": session_name,
                    },
                },
                "spec": {
                    "accessModes": [
                        "ReadWriteOnce",
                    ],
                    "resources": {
                        "requests": {
                            "storage": docker_storage,
                        }
                    },
                },
            }

            if CLUSTER_STORAGE_CLASS:
                docker_persistent_volume_claim["spec"][
                    "storageClassName"
                ] = CLUSTER_STORAGE_CLASS

            resource_objects = [docker_persistent_volume_claim]

        if docker_compose:
            # Where a volume mount references the named volume "workshop"
            # convert that to a bind mount of workshop home directory. We
            # should probably block certain types of mounts but allow
            # everything for now.

            docker_compose_services = xget(docker_compose, "services", {})

            for docker_compose_service in docker_compose_services.values():
                docker_compose_service_volumes = []

                for volume_details in xget(docker_compose_service, "volumes", []):
                    if xget(volume_details, "type") == "volume":
                        if xget(volume_details, "source") == "workshop":
                            docker_compose_service_volumes.append(
                                {
                                    "type": "bind",
                                    "source": "/home/eduk8s",
                                    "target": xget(volume_details, "target"),
                                }
                            )
                        else:
                            docker_compose_service_volumes.append(volume_details)
                    else:
                        docker_compose_service_volumes.append(volume_details)

                docker_compose_service["volumes"] = docker_compose_service_volumes

            docker_compose_config_map_body = {
                "apiVersion": "v1",
                "kind": "ConfigMap",
                "metadata": {
                    "name": f"{session_namespace}-docker-compose",
                    "namespace": workshop_namespace,
                    "labels": {
                        "training.educates.dev/component": "session",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                        "training.educates.dev/session.name": session_name,
                    },
                },
                "data": {
                    "compose-dev.yaml": yaml.dump(
                        substitute_variables(docker_compose, session_variables),
                        Dumper=yaml.Dumper,
                    )
                },
            }

            resource_objects.append(docker_compose_config_map_body)

            docker_compose_container = {
                "name": "docker-compose",
                "image": dockerd_image,
                "imagePullPolicy": dockerd_image_pull_policy,
                "command": [
                    "docker",
                    "--host=unix:///var/run/docker/docker.sock",
                    "compose",
                    "--file=/opt/eduk8s/config/compose-dev.yaml",
                    "--project-directory=/home/eduk8s",
                    f"--project-name={session_namespace}",
                    "up",
                ],
                "securityContext": {
                    "allowPrivilegeEscalation": False,
                    "capabilities": {"drop": ["ALL"]},
                    "runAsNonRoot": True,
                    "runAsUser": 1001,
                    "runAsGroup": 2375,
                    # "seccompProfile": {"type": "RuntimeDefault"},
                },
                "resources": {
                    "limits": {"memory": "256Mi"},
                    "requests": {"memory": "32Mi"},
                },
                "env": [{"name": "HOME", "value": "/home/eduk8s"}],
                "volumeMounts": [
                    {
                        "name": "docker-socket",
                        "mountPath": "/var/run/docker",
                        "readOnly": True,
                    },
                    {
                        "name": "compose-config",
                        "mountPath": "/opt/eduk8s/config",
                    },
                    {
                        "name": "workshop-data",
                        "mountPath": "/home/eduk8s",
                        "subPath": workshop_volume_subpath,
                    },
                ],
            }

            docker_compose_volumes = [
                {
                    "name": "compose-config",
                    "configMap": {
                        "name": f"{session_namespace}-docker-compose",
                    },
                },
            ]

            deployment_pod_template_spec["volumes"].extend(docker_compose_volumes)

            deployment_pod_template_spec["containers"].append(docker_compose_container)

    for object_body in resource_objects:
        object_body = substitute_variables(object_body, session_variables)
        kopf.adopt(object_body, namespace_instance.obj)
        create_from_dict(object_body)

    # Add in extra configuration for registry and create session objects.

    if applications.is_enabled("registry"):
        registry_htpasswd_hash = bcrypt.hashpw(
            bytes(registry_password, "ascii"), bcrypt.gensalt(prefix=b"2a")
        ).decode("ascii")

        registry_htpasswd = f"{registry_username}:{registry_htpasswd_hash}\n"

        additional_env.append(
            {
                "name": "REGISTRY_HOST",
                "value": registry_host,
            }
        )
        additional_env.append(
            {
                "name": "REGISTRY_USERNAME",
                "value": registry_username,
            }
        )
        additional_env.append(
            {
                "name": "REGISTRY_PASSWORD",
                "value": registry_password,
            }
        )
        additional_env.append(
            {
                "name": "REGISTRY_AUTH_TOKEN",
                "value": registry_auth_token,
            }
        )
        additional_env.append(
            {
                "name": "REGISTRY_SECRET",
                "value": registry_secret,
            }
        )

        registry_volumes = [
            {
                "name": "registry",
                "configMap": {
                    "name": f"{session_namespace}-registry",
                    "items": [{"key": "config.json", "path": "config.json"}],
                },
            },
        ]

        deployment_body["spec"]["template"]["spec"]["volumes"].extend(registry_volumes)

        registry_workshop_volume_mounts = [
            {
                "name": "registry",
                "mountPath": "/var/run/registry",
            },
        ]

        deployment_pod_template_spec["containers"][0]["volumeMounts"].extend(
            registry_workshop_volume_mounts
        )

        registry_memory = applications.property("registry", "memory", "768Mi")
        registry_storage = applications.property("registry", "storage", "5Gi")

        registry_volume_name = substitute_variables(
            applications.property("registry", "volume.name", ""),
            session_variables,
        )

        if registry_volume_name:
            registry_storage = None

            registry_volume_subpath = substitute_variables(
                applications.property("registry", "volume.subPath", ""),
                session_variables,
            )

            if registry_volume_subpath:
                registry_volume_subpath = f"{registry_volume_subpath}/var/lib/registry"
            else:
                registry_volume_subpath = "var/lib/registry"

        else:
            registry_volume_subpath = "var/lib/registry"

        if not registry_volume_name:
            registry_volume_name = f"{session_namespace}-registry"

        if registry_storage:
            registry_persistent_volume_claim_body = {
                "apiVersion": "v1",
                "kind": "PersistentVolumeClaim",
                "metadata": {
                    "name": f"{session_namespace}-registry",
                    "namespace": workshop_namespace,
                    "labels": {
                        "training.educates.dev/component": "session",
                        "training.educates.dev/workshop.name": workshop_name,
                        "training.educates.dev/portal.name": portal_name,
                        "training.educates.dev/portal.uid": portal_uid,
                        "training.educates.dev/environment.name": environment_name,
                        "training.educates.dev/environment.uid": environment_uid,
                        "training.educates.dev/session.name": session_name,
                    },
                },
                "spec": {
                    "accessModes": ["ReadWriteOnce"],
                    "resources": {"requests": {"storage": registry_storage}},
                },
            }

            if CLUSTER_STORAGE_CLASS:
                registry_persistent_volume_claim_body["spec"][
                    "storageClassName"
                ] = CLUSTER_STORAGE_CLASS

        registry_config = {"auths": {registry_host: {"auth": f"{registry_auth_token}"}}}

        registry_config_map_body = {
            "apiVersion": "v1",
            "kind": "ConfigMap",
            "metadata": {
                "name": f"{session_namespace}-registry",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                },
            },
            "data": {
                "htpasswd": registry_htpasswd,
                "config.json": json.dumps(registry_config, indent=4),
            },
        }

        registry_image = DOCKER_REGISTRY_IMAGE
        registry_image_pull_policy = image_pull_policy(registry_image)

        registry_deployment_body = {
            "apiVersion": "apps/v1",
            "kind": "Deployment",
            "metadata": {
                "name": f"{session_namespace}-registry",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/application": "registry",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                    "training.educates.dev/session.services.registry": "true",
                },
            },
            "spec": {
                "replicas": 1,
                "selector": {
                    "matchLabels": {"deployment": f"{session_namespace}-registry"}
                },
                "strategy": {"type": "Recreate"},
                "template": {
                    "metadata": {
                        "labels": {
                            "deployment": f"{session_namespace}-registry",
                            "training.educates.dev/component": "session",
                            "training.educates.dev/application": "registry",
                            "training.educates.dev/workshop.name": workshop_name,
                            "training.educates.dev/portal.name": portal_name,
                            "training.educates.dev/portal.uid": portal_uid,
                            "training.educates.dev/environment.name": environment_name,
                            "training.educates.dev/environment.uid": environment_uid,
                            "training.educates.dev/session.name": session_name,
                            "training.educates.dev/session.services.registry": "true",
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
                                    "limits": {"memory": registry_memory},
                                    "requests": {"memory": registry_memory},
                                },
                                "ports": [{"containerPort": 5000, "protocol": "TCP"}],
                                "env": [
                                    {
                                        "name": "REGISTRY_STORAGE_DELETE_ENABLED",
                                        "value": "true",
                                    },
                                    {"name": "REGISTRY_AUTH", "value": "htpasswd"},
                                    {
                                        "name": "REGISTRY_AUTH_HTPASSWD_REALM",
                                        "value": "Image Registry",
                                    },
                                    {
                                        "name": "REGISTRY_AUTH_HTPASSWD_PATH",
                                        "value": "/auth/htpasswd",
                                    },
                                ],
                                "volumeMounts": [
                                    {
                                        "name": "data",
                                        "mountPath": "/var/lib/registry",
                                        "subPath": registry_volume_subpath,
                                    },
                                    {"name": "auth", "mountPath": "/auth"},
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
                                    "claimName": registry_volume_name
                                },
                            },
                            {
                                "name": "auth",
                                "configMap": {
                                    "name": f"{session_namespace}-registry",
                                    "items": [{"key": "htpasswd", "path": "htpasswd"}],
                                },
                            },
                        ],
                    },
                },
            },
        }

        if CLUSTER_STORAGE_USER:
            # This hack is to cope with Kubernetes clusters which don't
            # properly set up persistent volume ownership. IBM
            # Kubernetes is one example. The init container runs as root
            # and sets permissions on the storage and ensures it is
            # group writable. Note that this will only work where pod
            # security policies are not enforced. Don't attempt to use
            # it if they are. If they are, this hack should not be
            # required.

            workshop_init_container = {
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
                "resources": {
                    "limits": {"memory": registry_memory},
                    "requests": {"memory": registry_memory},
                },
                "volumeMounts": [{"name": "data", "mountPath": "/mnt"}],
            }

            registry_deployment_body["spec"]["template"]["spec"][
                "initContainers"
            ].append(workshop_init_container)

        registry_service_body = {
            "apiVersion": "v1",
            "kind": "Service",
            "metadata": {
                "name": f"{session_namespace}-registry",
                "namespace": workshop_namespace,
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/application": "registry",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                },
            },
            "spec": {
                "type": "ClusterIP",
                "ports": [{"port": 5000, "targetPort": 5000}],
                "selector": {"deployment": f"{session_namespace}-registry"},
            },
        }

        registry_ingress_body = {
            "apiVersion": "networking.k8s.io/v1",
            "kind": "Ingress",
            "metadata": {
                "name": f"{session_namespace}-registry",
                "namespace": workshop_namespace,
                "annotations": {"nginx.ingress.kubernetes.io/proxy-body-size": "512m"},
                "labels": {
                    "training.educates.dev/component": "session",
                    "training.educates.dev/application": "registry",
                    "training.educates.dev/workshop.name": workshop_name,
                    "training.educates.dev/portal.name": portal_name,
                    "training.educates.dev/portal.uid": portal_uid,
                    "training.educates.dev/environment.name": environment_name,
                    "training.educates.dev/environment.uid": environment_uid,
                    "training.educates.dev/session.name": session_name,
                },
            },
            "spec": {
                "rules": [
                    {
                        "host": registry_host,
                        "http": {
                            "paths": [
                                {
                                    "path": "/",
                                    "pathType": "Prefix",
                                    "backend": {
                                        "service": {
                                            "name": f"{session_namespace}-registry",
                                            "port": {"number": 5000},
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
            registry_ingress_body["metadata"]["annotations"].update(
                {
                    "ingress.kubernetes.io/force-ssl-redirect": "true",
                    "nginx.ingress.kubernetes.io/ssl-redirect": "true",
                    "nginx.ingress.kubernetes.io/force-ssl-redirect": "true",
                }
            )

        if INGRESS_SECRET:
            registry_ingress_body["spec"]["tls"] = [
                {
                    "hosts": [registry_host],
                    "secretName": INGRESS_SECRET,
                }
            ]

        registry_objects = [
            registry_config_map_body,
            registry_deployment_body,
            registry_service_body,
            registry_ingress_body,
        ]

        if registry_storage:
            registry_objects.insert(0, registry_persistent_volume_claim_body)

        for object_body in registry_objects:
            object_body = substitute_variables(object_body, session_variables)
            kopf.adopt(object_body, namespace_instance.obj)
            create_from_dict(object_body)

    # Apply any additional environment variables to the deployment.

    _apply_environment_patch(additional_env)

    # Overlay any additional labels to the deployment.

    deployment_body["metadata"]["labels"].update(additional_labels)
    deployment_body["spec"]["template"]["metadata"]["labels"].update(additional_labels)

    # Create a secret which contains the SSH key pair so that it can be
    # mounted into the workshop container.

    ssh_keys_secret_body = {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {
            "name": f"{session_namespace}-ssh-keys",
            "namespace": workshop_namespace,
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            },
        },
        "data": {
            "id_rsa": base64.b64encode(ssh_private_key.encode("utf-8")).decode("utf-8"),
            "id_rsa.pub": base64.b64encode(ssh_public_key.encode("utf-8")).decode(
                "utf-8"
            ),
        },
    }

    deployment_pod_template_spec["volumes"].append(
        {
            "name": "ssh-keys",
            "secret": {
                "secretName": f"{session_namespace}-ssh-keys",
                "defaultMode": 0o600,
            },
        },
    )

    deployment_pod_template_spec["containers"][0]["volumeMounts"].append(
        {
            "name": "ssh-keys",
            "mountPath": "/opt/ssh-keys",
            "readOnly": True,
        },
    )

    # Append any volume definitions and corresponding volume mounts. The volume
    # mounts are only applied to the workshop container. If any extra special
    # mounts are required for side car containers, a patch would need to be used
    # instead.

    extra_volumes_list = workshop_spec["session"].get("volumes", [])
    extra_volume_mounts_list = workshop_spec["session"].get("volumeMounts", [])

    if extra_volumes_list:
        deployment_pod_template_spec["volumes"].extend(
            substitute_variables(extra_volumes_list, session_variables)
        )

    if extra_volume_mounts_list:
        deployment_pod_template_spec["containers"][0]["volumeMounts"].extend(
            substitute_variables(extra_volume_mounts_list, session_variables)
        )

    # Create a service so that the workshop environment can be accessed.
    # This is only internal to the cluster, so port forwarding or an
    # ingress is still needed to access it from outside of the cluster.

    service_body = {
        "apiVersion": "v1",
        "kind": "Service",
        "metadata": {
            "name": session_namespace,
            "namespace": workshop_namespace,
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/application": "workshop",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            },
        },
        "spec": {
            "type": "ClusterIP",
            "ports": [
                {
                    "name": "80-tcp",
                    "port": 80,
                    "protocol": "TCP",
                    "targetPort": 10080,
                }
            ],
            "selector": {"deployment": session_namespace},
        },
    }

    if applications.is_enabled("sshd"):
        service_body["spec"]["ports"].append(
            {
                "name": "22-tcp",
                "port": 22,
                "protocol": "TCP",
                "targetPort": 2022,
            }
        )

    # Create the ingress for the workshop, including any for extra named
    # named ingresses.

    ingress_rules = [
        {
            "host": session_hostname,
            "http": {
                "paths": [
                    {
                        "path": "/",
                        "pathType": "Prefix",
                        "backend": {
                            "service": {
                                "name": session_namespace,
                                "port": {"number": 80},
                            }
                        },
                    }
                ]
            },
        }
    ]

    websocket_routes = ["/"]

    if applications.is_enabled("sshd") and applications.property(
        "sshd", "tunnel.enabled", False
    ):
        ingress_rules.insert(
            0,
            {
                "host": session_hostname,
                "http": {
                    "paths": [
                        {
                            "path": "/tunnel/",
                            "pathType": "Prefix",
                            "backend": {
                                "service": {
                                    "name": "tunnel-manager",
                                    "port": {"number": 8080},
                                }
                            },
                        }
                    ]
                },
            },
        )

        websocket_routes.append("/tunnel/")

    ingresses = []
    ingress_hostnames = []

    if workshop_spec.get("session"):
        ingresses = workshop_spec["session"].get("ingresses", [])

    if applications.is_enabled("console"):
        ingress_hostnames.append(f"console-{session_namespace}.{INGRESS_DOMAIN}")
        # Suffix use is deprecated. See prior note.
        ingress_hostnames.append(f"{session_namespace}-console.{INGRESS_DOMAIN}")
    if applications.is_enabled("editor"):
        ingress_hostnames.append(f"editor-{session_namespace}.{INGRESS_DOMAIN}")
        # Suffix use is deprecated. See prior note.
        ingress_hostnames.append(f"{session_namespace}-editor.{INGRESS_DOMAIN}")

    for ingress in ingresses:
        ingress_hostnames.append(
            f"{ingress['name']}-{session_namespace}.{INGRESS_DOMAIN}"
        )
        # Suffix use is deprecated. See prior note.
        ingress_hostnames.append(
            f"{session_namespace}-{ingress['name']}.{INGRESS_DOMAIN}"
        )

    for ingress_hostname in ingress_hostnames:
        ingress_rules.append(
            {
                "host": ingress_hostname,
                "http": {
                    "paths": [
                        {
                            "path": "/",
                            "pathType": "Prefix",
                            "backend": {
                                "service": {
                                    "name": session_namespace,
                                    "port": {"number": 80},
                                }
                            },
                        }
                    ]
                },
            }
        )

    ingress_body = {
        "apiVersion": "networking.k8s.io/v1",
        "kind": "Ingress",
        "metadata": {
            "name": session_namespace,
            "namespace": workshop_namespace,
            "annotations": {
                "nginx.ingress.kubernetes.io/enable-cors": "true",
                "nginx.ingress.kubernetes.io/proxy-send-timeout": "3600",
                "nginx.ingress.kubernetes.io/proxy-read-timeout": "3600",
                "projectcontour.io/websocket-routes": ",".join(websocket_routes),
                "projectcontour.io/response-timeout": "3600s",
            },
            "labels": {
                "training.educates.dev/component": "session",
                "training.educates.dev/application": "workshop",
                "training.educates.dev/workshop.name": workshop_name,
                "training.educates.dev/portal.name": portal_name,
                "training.educates.dev/portal.uid": portal_uid,
                "training.educates.dev/environment.name": environment_name,
                "training.educates.dev/environment.uid": environment_uid,
                "training.educates.dev/session.name": session_name,
            },
        },
        "spec": {
            "rules": ingress_rules,
        },
    }

    if INGRESS_SECRET:
        ingress_body["metadata"]["annotations"].update(
            {
                "ingress.kubernetes.io/force-ssl-redirect": "true",
                "nginx.ingress.kubernetes.io/ssl-redirect": "true",
                "nginx.ingress.kubernetes.io/force-ssl-redirect": "true",
            }
        )

    if INGRESS_SECRET:
        ingress_body["spec"]["tls"] = [
            {
                "hosts": [session_hostname] + ingress_hostnames,
                "secretName": INGRESS_SECRET,
            }
        ]

    if INGRESS_CLASS:
        ingress_body["spec"]["ingressClassName"] = INGRESS_CLASS

    # Update deployment with host aliases for the ports which ingresses are
    # targeting. This is so thay can be accessed by hostname rather than by
    # localhost. If follow convention of accessing by hostname then can be
    # compatible if workshop deployed with docker-compose. Note that originally
    # was using suffixes for the ingress name but switched to a prefix as DNS
    # resolvers like nip.io support a prefix on a hostname consisting of an IP
    # address which could also be useful when using docker-compose.
    #
    # Note this probably isn't needed now as when host is defined for ingress
    # implicitly proxy to localhost anyway and so don't need these special host
    # names for embedded components.

    hostnames = []

    hostnames.extend(
        [
            f"console-{session_namespace}",
            f"editor-{session_namespace}",
        ]
    )

    # Suffix use is deprecated. See prior note.

    hostnames.extend(
        [
            f"{session_namespace}-console",
            f"{session_namespace}-editor",
        ]
    )

    # if session_namespace != "workshop":
    #     hostnames.extend(
    #         [
    #             f"console-workshop",
    #             f"editor-workshop",
    #         ]
    #     )

    for ingress in ingresses:
        hostnames.append(f"{ingress['name']}-{session_namespace}")

        # if session_namespace != "workshop":
        #     hostnames.append(f"{ingress['name']}-workshop")

        # Suffix use is deprecated. See prior note.

        hostnames.append(f"{session_namespace}-{ingress['name']}")

    host_aliases = [
        {
            "ip": "127.0.0.1",
            "hostnames": hostnames,
        }
    ]

    deployment_pod_template_spec["hostAliases"].extend(host_aliases)

    # Finally create the deployment, service and ingress for the workshop
    # session.

    kopf.adopt(ssh_keys_secret_body, namespace_instance.obj)

    pykube.Secret(api, ssh_keys_secret_body).create()

    kopf.adopt(deployment_body, namespace_instance.obj)

    pykube.Deployment(api, deployment_body).create()

    kopf.adopt(service_body, namespace_instance.obj)

    pykube.Service(api, service_body).create()

    kopf.adopt(ingress_body, namespace_instance.obj)

    pykube.Ingress(api, ingress_body).create()

    # Report analytics event workshop session should be ready.

    report_analytics_event(
        "Resource/Ready",
        {"kind": "WorkshopSession", "name": name, "uid": uid, "retry": retry},
    )

    logger.info("Workshop session %s has been created.", session_name)

    # Set the URL for accessing the workshop session directly in the
    # status. This would only be used if directly creating workshop
    # session and not when using training portal. Set phase to Running
    # if standalone workshop environment. Where created by a training
    # portal it will set the status itself appropriately.

    url = f"{INGRESS_PROTOCOL}://{session_hostname}"

    phase = "Running"

    changes = {
        "message": None,
        "url": url,
        "sshd": {
            "enabled": applications.is_enabled("sshd"),
            "tunnel": {
                "enabled": applications.property("sshd", "tunnel.enabled", False)
            },
        },
    }

    if not portal_name:
        changes["phase"] = phase

    patch["status"] = {
        "educates": changes,
    }


@kopf.on.delete(
    "training.educates.dev",
    "v1beta1",
    "workshopsessions",
    optional=True,
)
def workshop_session_delete(**_):
    """Nothing to do here at this point because the owner references will
    ensure that everything is cleaned up appropriately."""

    # NOTE: This doesn't actually get called because we as we marked it as
    # optional to avoid a finalizer being added to the custom resource, so we
    # use separate generic event handler below to log when the workshop
    # session is deleted.


@kopf.on.event("training.educates.dev", "v1beta1", "workshopsessions")
def workshop_session_event(type, event, **_):  # pylint: disable=redefined-builtin
    """Log when a workshop session is deleted."""

    if type == "DELETED":
        logger.info(
            "Workshop session %s has been deleted.",
            event["object"]["metadata"]["name"],
        )


@kopf.on.event(
    "",
    "v1",
    "pods",
    labels={
        "training.educates.dev/component": "session",
        "training.educates.dev/application": "workshop",
        "training.educates.dev/portal.name": kopf.PRESENT,
        "training.educates.dev/environment.name": kopf.PRESENT,
        "training.educates.dev/session.name": kopf.PRESENT,
    },
)
def workshop_session_pod_event(type, event, **_):  # pylint: disable=redefined-builtin
    """Log the status of deployment of any workshop session pods."""

    pod_name = event["object"]["metadata"]["name"]
    pod_namespace = event["object"]["metadata"]["namespace"]

    pod_status = event["object"].get("status", {}).get("phase", "Unknown")

    if type in ("ADDED", "MODIFIED", "DELETED"):
        logger.info(
            "Workshop session pod %s in namespace %s has been %s with current status of %s.",
            pod_name,
            pod_namespace,
            type.lower(),
            pod_status,
        )

    else:
        logger.info(
            "Workshop session pod %s in namespace %s has been found with current status of %s.",
            pod_name,
            pod_namespace,
            pod_status,
        )
