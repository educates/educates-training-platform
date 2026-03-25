"""Operator handlers for cluster configuration resources."""

import asyncio
import base64
import logging
from typing import Any, Dict

import kopf
import yaml
from wrapt import synchronized

from ..caches.clusters import ClusterConfig
from ..caches.environments import WorkshopEnvironment
from ..caches.portals import PortalCredentials, TrainingPortal
from ..caches.sessions import WorkshopSession
from ..helpers.kubeconfig import (
    create_kubeconfig_from_access_token_secret,
    extract_context_from_kubeconfig,
    verify_kubeconfig_format,
)
from ..helpers.objects import xgetattr
from ..helpers.operator import GenericOperator
from ..service import ServiceState

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
    uid: str,
    meta: kopf.Meta,
    spec: kopf.Spec,
    secrets_index: Dict[str, Any],
    memo: ServiceState,
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
        # For the local cluster, we access credentials for accessing the cluster
        # from a mounted Kubernetes access token secret. Note that we do not
        # know the external URL of the local cluster, so we use the internal
        # Kubernetes service URL. This will need to be replaced if the
        # kubeconfig is used for accessing the cluster from outside the cluster.

        server = "https://kubernetes.default.svc"

        # TODO: Make the path to the access token secret configurable.

        kubeconfig = create_kubeconfig_from_access_token_secret(
            "/opt/cluster-access-token", name, server
        )

    # Update the cluster configuration in the cluster database.

    cluster_database = memo.cluster_database

    with synchronized(cluster_database):
        cluster_config = cluster_database.get_cluster(name)

        if not cluster_config:
            logger.info(
                "Registering cluster configuration %r with generation %s.",
                name,
                generation,
            )

            cluster_database.add_cluster(
                ClusterConfig(
                    name=name,
                    uid=uid,
                    labels=xgetattr(spec, "labels", []),
                    kubeconfig=kubeconfig,
                )
            )

        else:
            logger.info(
                "Updating cluster configuration %r with generation %s.",
                name,
                generation,
            )

            cluster_config.labels = xgetattr(spec, "labels", [])
            cluster_config.kubeconfig = kubeconfig


@kopf.on.delete("clusterconfigs.lookup.educates.dev")
def clusterconfigs_delete(name: str, meta: kopf.Meta, memo: ServiceState, **_):
    """Remove the cluster configuration from the cluster database."""

    generation = meta.get("generation")

    cluster_database = memo.cluster_database

    logger.info("Delete cluster configuration %r with generation %s", name, generation)

    cluster_database.remove_cluster(name)


class ClusterOperator(GenericOperator):
    """Operator for interacting with training platform on separate cluster."""

    def __init__(self, cluster_name: str, service_state: ServiceState) -> None:
        """Initializes the operator."""

        super().__init__(cluster_name, service_state=service_state)

    def register_handlers(self) -> None:
        """Register the handlers for the training platform operator."""

        @kopf.on.event(
            "trainingportals.training.educates.dev",
            registry=self.operator_registry,
        )
        async def trainingportals_event(event: kopf.RawEvent, **_):
            """Handles events for training portals."""

            body = xgetattr(event, "object", {})
            metadata = xgetattr(body, "metadata", {})
            spec = xgetattr(body, "spec", {})
            status = xgetattr(body, "status", {})

            portal_name = xgetattr(metadata, "name")
            portal_uid = xgetattr(metadata, "uid")

            with synchronized(self.cluster_config):
                if xgetattr(event, "type") == "DELETED":
                    logger.info(
                        "Discard training portal %s with uid %s of cluster %s",
                        portal_name,
                        portal_uid,
                        self.cluster_name,
                    )

                    portal_state = self.cluster_config.get_portal(portal_name)

                    if portal_state:
                        self.cluster_config.remove_portal(portal_name)

                        # Mark as stopped in case any workshop environments
                        # which reference it still haven't been cleaned up.

                        portal_state.phase = "Stopped"

                else:
                    credentials = PortalCredentials(
                        client_id=xgetattr(status, "educates.clients.robot.id"),
                        client_secret=xgetattr(status, "educates.clients.robot.secret"),
                        username=xgetattr(status, "educates.credentials.robot.username"),
                        password=xgetattr(status, "educates.credentials.robot.password"),
                    )

                    portal_state = self.cluster_config.get_portal(portal_name)

                    if not portal_state:
                        logger.info(
                            "Registering training portal %s with uid %s of cluster %s",
                            portal_name,
                            portal_uid,
                            self.cluster_name,
                        )

                        self.cluster_config.add_portal(
                            TrainingPortal(
                                cluster=self.cluster_config,
                                name=portal_name,
                                uid=portal_uid,
                                generation=xgetattr(metadata, "generation"),
                                labels=xgetattr(spec, "portal.labels", []),
                                url=xgetattr(status, "educates.url"),
                                phase=xgetattr(status, "educates.phase"),
                                credentials=credentials,
                                capacity=xgetattr(spec, "portal.sessions.maximum", 0),
                                allocated=0,
                            )
                        )

                        portal_state = self.cluster_config.get_portal(portal_name)

                    else:
                        logger.info(
                            "Updating training portal %s with uid %s of cluster %s",
                            portal_name,
                            portal_uid,
                            self.cluster_name,
                        )

                        portal_state.uid = portal_uid
                        portal_state.generation = xgetattr(metadata, "generation")
                        portal_state.labels = xgetattr(spec, "portal.labels", [])
                        portal_state.url = xgetattr(status, "educates.url")
                        portal_state.phase = xgetattr(status, "educates.phase")
                        portal_state.credentials = credentials
                        portal_state.capacity = xgetattr(
                            spec, "portal.sessions.maximum", 0
                        )

                    portal_state.recalculate_capacity()

        @kopf.on.event(
            "workshopenvironments.training.educates.dev",
            labels={"training.educates.dev/portal.name": kopf.PRESENT},
            registry=self.operator_registry,
        )
        async def workshopenvironments_event(event: kopf.RawEvent, **_):
            """Handles events for workshop environments."""

            body = xgetattr(event, "object", {})
            metadata = xgetattr(body, "metadata", {})
            spec = xgetattr(body, "spec", {})
            status = xgetattr(body, "status", {})

            portal_name = xgetattr(metadata, "labels", {}).get(
                "training.educates.dev/portal.name"
            )
            portal_uid = xgetattr(metadata, "labels", {}).get(
                "training.educates.dev/portal.uid"
            )

            environment_name = xgetattr(metadata, "name")
            environment_uid = xgetattr(metadata, "uid")

            workshop_name = xgetattr(metadata, "labels", {}).get(
                "training.educates.dev/portal.workshop", ""
            ) or xgetattr(spec, "workshop.name")

            workshop_generation = xgetattr(status, "educates.workshop.generation", 0)
            workshop_spec = xgetattr(status, "educates.workshop.spec", {})

            with synchronized(self.cluster_config):
                portal = self.cluster_config.get_portal(portal_name)

                if xgetattr(event, "type") == "DELETED":
                    if portal:
                        logger.info(
                            "Discard workshop environment %s for workshop %s from portal %s of cluster %s",  # pylint: disable=line-too-long
                            environment_name,
                            workshop_name,
                            portal_name,
                            self.cluster_name,
                        )

                        portal.remove_environment(environment_name)
                        portal.recalculate_capacity()

                        if portal.phase == "Unknown" and not portal.get_environments():
                            logger.info(
                                "Discard unknown training portal %s with uid %s of cluster %s",
                                portal_name,
                                portal_uid,
                                self.cluster_name,
                            )

                            self.cluster_config.remove_portal(portal_name)

                    else:
                        logger.info(
                            "Discard workshop environment %s for workshop %s from portal %s of cluster %s as portal not found",  # pylint: disable=line-too-long
                            environment_name,
                            workshop_name,
                            portal_name,
                            self.cluster_name,
                        )

                else:
                    if not portal:
                        logger.info(
                            "Registering unknown training portal %s with uid %s of cluster %s",
                            portal_name,
                            portal_uid,
                            self.cluster_name,
                        )

                        portal = TrainingPortal(
                            cluster=self.cluster_config,
                            name=portal_name,
                            uid=portal_uid,
                            generation=0,
                            labels=[],
                            url="",
                            phase="Unknown",
                            credentials=PortalCredentials(
                                client_id="",
                                client_secret="",
                                username="",
                                password="",
                            ),
                            capacity=0,
                            allocated=0,
                        )

                        self.cluster_config.add_portal(portal)

                    environment_state = portal.get_environment(environment_name)

                    if not environment_state:
                        logger.info(
                            "Registering workshop environment %s for workshop %s from portal %s of cluster %s",  # pylint: disable=line-too-long
                            environment_name,
                            workshop_name,
                            portal_name,
                            self.cluster_name,
                        )

                        portal.add_environment(
                            WorkshopEnvironment(
                                portal=portal,
                                name=environment_name,
                                uid=environment_uid,
                                generation=workshop_generation,
                                workshop=workshop_name,
                                title=xgetattr(workshop_spec, "title"),
                                description=xgetattr(workshop_spec, "description"),
                                labels=xgetattr(workshop_spec, "labels", []),
                                capacity=xgetattr(status, "educates.capacity", 0),
                                reserved=xgetattr(status, "educates.reserved", 0),
                                allocated=0,
                                available=0,
                                phase=xgetattr(status, "educates.phase"),
                            )
                        )

                    else:
                        logger.info(
                            "Updating workshop environment %s for workshop %s from portal %s of cluster %s",  # pylint: disable=line-too-long
                            environment_name,
                            workshop_name,
                            portal_name,
                            self.cluster_name,
                        )

                        environment_state.generation = workshop_generation
                        environment_state.title = xgetattr(workshop_spec, "title")
                        environment_state.description = xgetattr(
                            workshop_spec, "description"
                        )
                        environment_state.labels = xgetattr(workshop_spec, "labels", [])

                        environment_state.phase = xgetattr(status, "educates.phase")

                        environment_state.capacity = xgetattr(
                            status, "educates.capacity", 0
                        )
                        environment_state.reserved = xgetattr(
                            status, "educates.reserved", 0
                        )

                    portal.recalculate_capacity()

        @kopf.on.event(
            "workshopsessions.training.educates.dev",
            labels={
                "training.educates.dev/portal.name": kopf.PRESENT,
                "training.educates.dev/environment.name": kopf.PRESENT,
            },
            registry=self.operator_registry,
        )
        async def workshopsessions_event(event: kopf.RawEvent, **_):
            """Handles events for workshop sessions."""

            body = xgetattr(event, "object", {})
            metadata = xgetattr(body, "metadata", {})
            spec = xgetattr(body, "spec", {})
            status = xgetattr(body, "status", {})

            portal_name = xgetattr(metadata, "labels", {}).get(
                "training.educates.dev/portal.name"
            )
            portal_uid = xgetattr(metadata, "labels", {}).get(
                "training.educates.dev/portal.uid"
            )

            environment_name = xgetattr(metadata, "labels", {}).get(
                "training.educates.dev/environment.name"
            )
            environment_uid = xgetattr(metadata, "labels", {}).get(
                "training.educates.dev/environment.uid"
            )

            workshop_name = xgetattr(spec, "workshop.name")

            session_name = xgetattr(metadata, "name")

            with synchronized(self.cluster_config):
                portal = self.cluster_config.get_portal(portal_name)

                if xgetattr(event, "type") == "DELETED":
                    if portal:
                        environment = portal.get_environment(environment_name)

                        if environment:
                            logger.info(
                                "Discard workshop session %s for environment %s from portal %s of cluster %s",  # pylint: disable=line-too-long
                                session_name,
                                environment_name,
                                portal_name,
                                self.cluster_name,
                            )

                            environment.remove_session(session_name)
                            portal.recalculate_capacity()

                            if environment.phase == "Unknown" and not environment.get_sessions():
                                logger.info(
                                    "Discard unknown workshop environment %s from portal %s of cluster %s",  # pylint: disable=line-too-long
                                    environment_name,
                                    portal_name,
                                    self.cluster_name,
                                )

                                portal.remove_environment(environment_name)

                                if portal.phase == "Unknown" and not portal.get_environments():
                                    logger.info(
                                        "Discard unknown training portal %s with uid %s of cluster %s",
                                        portal_name,
                                        portal_uid,
                                        self.cluster_name,
                                    )

                                    self.cluster_config.remove_portal(portal_name)

                        else:
                            logger.info(
                                "Discard workshop session %s for environment %s from portal %s of cluster %s as environment not found",  # pylint: disable=line-too-long
                                session_name,
                                environment_name,
                                portal_name,
                                self.cluster_name,
                            )

                    else:
                        logger.info(
                            "Discard workshop session %s for environment %s from portal %s of cluster %s as portal not found",  # pylint: disable=line-too-long
                            session_name,
                            environment_name,
                            portal_name,
                            self.cluster_name,
                        )

                else:
                    if not portal:
                        logger.info(
                            "Registering unknown training portal %s with uid %s of cluster %s",
                            portal_name,
                            portal_uid,
                            self.cluster_name,
                        )

                        portal = TrainingPortal(
                            cluster=self.cluster_config,
                            name=portal_name,
                            uid=portal_uid,
                            generation=0,
                            labels=[],
                            url="",
                            phase="Unknown",
                            credentials=PortalCredentials(
                                client_id="",
                                client_secret="",
                                username="",
                                password="",
                            ),
                            capacity=0,
                            allocated=0,
                        )

                        self.cluster_config.add_portal(portal)

                    environment = portal.get_environment(environment_name)

                    if not environment:
                        logger.info(
                            "Registering unknown workshop environment %s from portal %s of cluster %s",
                            environment_name,
                            portal_name,
                            self.cluster_name,
                        )

                        environment = WorkshopEnvironment(
                            portal=portal,
                            name=environment_name,
                            uid=environment_uid,
                            generation=0,
                            workshop=workshop_name,
                            title="",
                            description="",
                            labels=[],
                            capacity=0,
                            reserved=0,
                            allocated=0,
                            available=0,
                            phase="Unknown",
                        )

                        portal.add_environment(environment)

                    session_state = environment.get_session(session_name)

                    if not session_state:
                        logger.info(
                            "Registering workshop session %s for environment %s from portal %s of cluster %s, where user is %r",  # pylint: disable=line-too-long
                            session_name,
                            environment_name,
                            portal_name,
                            self.cluster_name,
                            xgetattr(status, "educates.user"),
                        )

                        environment.add_session(
                            WorkshopSession(
                                environment=environment,
                                name=session_name,
                                generation=xgetattr(metadata, "generation"),
                                phase=xgetattr(status, "educates.phase"),
                                user=xgetattr(status, "educates.user"),
                            )
                        )

                    else:
                        logger.info(
                            "Updating workshop session %s for environment %s from portal %s of cluster %s, where user is %r",  # pylint: disable=line-too-long
                            session_name,
                            environment_name,
                            portal_name,
                            self.cluster_name,
                            xgetattr(status, "educates.user"),
                        )

                        session_state.generation = xgetattr(metadata, "generation")
                        session_state.phase = xgetattr(status, "educates.phase")
                        session_state.user = xgetattr(status, "educates.user")

                    portal.recalculate_capacity()


@kopf.daemon(
    "clusterconfigs.lookup.educates.dev",
    cancellation_backoff=5.0,
    cancellation_polling=5.0,
)
def clusterconfigs_daemon(
    stopped: kopf.DaemonStopped,
    name: str,
    uid: str,
    retry: int,
    memo: ServiceState,
    **_,
) -> None:
    """Starts an instance of the cluster operator for each registered cluster
    and waits for it to complete."""

    # Make sure we have separately processed the cluster config resource so
    # that an item exists for it in the cache and it has the same uid.

    cache = memo.cluster_database

    cluster_config = cache.get_cluster(name)

    if not cluster_config or cluster_config.uid != uid:
        raise kopf.TemporaryError(
            f"Cluster {name} with uid {uid} not found.",
            delay=5 if not retry else 15,
        )

    # Start the cluster operator and wait for it to complete. An infinite loop
    # is used to keep the daemon thread running until the daemon is stopped as
    # kopf framework expects this daemon thread to be running indefinitely until
    # it is stopped.

    operator = ClusterOperator(cluster_config, memo)

    operator.run_until_stopped(stopped)
