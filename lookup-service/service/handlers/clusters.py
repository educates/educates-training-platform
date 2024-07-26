"""Operator handlers for cluster configuration resources."""

import base64
import logging

from typing import Any, Dict

import kopf
import yaml

from ..service import ServiceState
from ..caches.clusters import ClusterConfiguration
from ..helpers.objects import xgetattr
from ..helpers.kubeconfig import (
    create_kubeconfig_from_access_token_secret,
    verify_kubeconfig_format,
    extract_context_from_kubeconfig,
)


logger = logging.getLogger("educates")


@kopf.index("secrets", when=lambda body, **_: body.get("type") == "Opaque")
def secrets_index(namespace: str, name: str, body: kopf.Body, **_) -> dict:
    """Keeps an index of secret data by namespace and name. This is so we can
    easily retrieve the kubeconfig data for each cluster when starting the
    training platform operator."""

    # Note that under normal circumstances only a single namespace should be
    # monitored, thus we are not caching secrets from the whole cluster but
    # only where the operator is deployed. This is to avoid potential security
    # issues and memory bloat from caching secrets from the whole cluster.

    return {(namespace, name): xgetattr(body, "data", {})}


@kopf.on.resume("clusterconfigs.lookup.educates.dev")
@kopf.on.create("clusterconfigs.lookup.educates.dev")
@kopf.on.update("clusterconfigs.lookup.educates.dev")
def clusterconfigs_update(
    namespace: str,
    name: str,
    meta: kopf.Meta,
    spec: kopf.Spec,
    secrets_index: Dict[str, Any],
    memo: ServiceState,
    reason: str,
    retry: int,
    **_,
):  # pylint: disable=redefined-outer-name
    """Add the cluster configuration to the cluster database."""

    generation = meta.get("generation")

    # We need to cache the kubeconfig data. This can be provided in a separate
    # secret or it can be read from a mounted secret for the case of the local
    # cluster.

    secret_ref_name = xgetattr(spec, "credentials.kubeconfig.secretRef.name")

    if secret_ref_name is not None:
        config_key = xgetattr(spec, "credentials.kubeconfig.secretRef.key", "config")

        # Make sure the secret holding the kubeconfig has been seen already and
        # that the key for the kubeconfig file is present in the data.

        if (namespace, secret_ref_name) not in secrets_index:
            raise kopf.TemporaryError(
                f"Secret {secret_ref_name} required for cluster configuration {name} not found.",
                delay=5,
            )

        cluster_config_data, *_ = secrets_index[(namespace, secret_ref_name)]

        if config_key not in cluster_config_data:
            raise kopf.TemporaryError(
                f"Key {config_key} not found in secret {secret_ref_name} required for cluster configuration {name}.",  # pylint: disable=line-too-long
                delay=5 if not retry else 15,
            )

        # Decode the base64 encoded kubeconfig data and load it as a yaml
        # document.

        try:
            kubeconfig = yaml.safe_load(
                base64.b64decode(
                    xgetattr(cluster_config_data, config_key, "").encode("UTF-8")
                )
            )
        except yaml.YAMLError as exc:
            raise kopf.TemporaryError(
                f"Failed to load kubeconfig data from secret {secret_ref_name} required for cluster configuration {name}.",  # pylint: disable=line-too-long
                delay=5 if not retry else 15,
            ) from exc

        try:
            verify_kubeconfig_format(kubeconfig)
        except ValueError as exc:
            raise kopf.TemporaryError(
                f"Invalid kubeconfig data in secret {secret_ref_name} required for cluster configuration {name}.",  # pylint: disable=line-too-long
                delay=5 if not retry else 15,
            ) from exc

        # Extract only the context from the kubeconfig file that is required
        # for the cluster configuration.

        try:
            kubeconfig = extract_context_from_kubeconfig(
                kubeconfig, xgetattr(spec, "credentials.kubeconfig.context")
            )
        except ValueError as exc:
            raise kopf.TemporaryError(
                f"Failed to extract kubeconfig context from secret {secret_ref_name} required for cluster configuration {name}.",  # pylint: disable=line-too-long
                delay=5 if not retry else 15,
            ) from exc

    else:
        server = xgetattr(spec, "server", "https://kubernetes.default.svc")

        kubeconfig = create_kubeconfig_from_access_token_secret(
            "/opt/cluster-access-token", name, server
        )

    # Update the cluster configuration in the cluster database.

    logger.info(
        "%s cluster configuration %r with generation %s.",
        (reason == "update") and "Update" or "Register",
        name,
        generation,
    )

    cluster_database = memo.cluster_database

    cluster_database.update_cluster(
        ClusterConfiguration(
            name=name,
            labels=xgetattr(spec, "labels", {}),
            kubeconfig=kubeconfig,
        ),
    )


@kopf.on.delete("clusterconfigs.lookup.educates.dev")
def clusterconfigs_delete(name: str, memo: ServiceState, **_):
    """Remove the cluster configuration from the cluster database."""

    generation = memo.get("generation")

    cluster_database = memo.cluster_database

    logger.info("Delete cluster configuration %r with generation %s", name, generation)

    cluster_database.remove_cluster(name)
