"""Base class and helper functions for kopf based operator."""

import asyncio
import contextlib
import logging
import threading
import time

import aiohttp
import kopf

from ..caches.clusters import ClusterConfig
from ..service import ServiceState
from .kubeconfig import create_connection_info_from_kubeconfig

logger = logging.getLogger("educates")


class GenericOperator(threading.Thread):
    """Base class for kopf based operator."""

    def __init__(
        self,
        cluster_config: ClusterConfig,
        *,
        namespaces: str = None,
        service_state: ServiceState
    ) -> None:
        """Initializes the operator."""

        super().__init__()

        # Set the name of the operator and the namespaces to watch for
        # resources. When the list of namespaces is empty, the operator will
        # watch for resources cluster wide.

        self.cluster_config = cluster_config
        self.namespaces = namespaces or []

        # Set the state object for the operator. This is used to store the state
        # of the operator across invocations.

        self.service_state = service_state

        # Create an operator registry to store the handlers for the operator.
        # We need a distinct registry for each operator since we need to be able
        # to run multiple operators in the same process with separate handlers.

        self.operator_registry = kopf.OperatorRegistry()

        # Create a stop flag to signal the operator to stop running. This is
        # used to bridge between the kopf variable for stopping the operator
        # and event required to stop the event loop for the operator.

        self._stop_flag = threading.Event()

    @property
    def cluster_name(self):
        """Return the name of the cluster the operator is managing."""

        return self.cluster_config.name

    @property
    def kubeconfig(self):
        """Return the kubeconfig for the cluster the operator is managing."""

        return self.cluster_config.kubeconfig

    def register_handlers(self) -> None:
        """Register the handlers for the operator."""

        raise NotImplementedError("Subclasses must implement this method.")

    def run(self) -> None:
        """Starts the kopf operator in a separate event loop."""

        # Register the login function for the operator.

        @kopf.on.login(registry=self.operator_registry)
        def login_fn(**_) -> dict:
            """Returns login credentials for the cluster calculated from the
            configuration currently held in the cluster configuration cache."""

            return create_connection_info_from_kubeconfig(self.kubeconfig)

        @kopf.on.cleanup()
        async def cleanup_fn(**_) -> None:
            """Cleanup function for operator."""

            # Workaround for possible kopf bug, set stop flag.

            self._stop_flag.set()

        # Register the kopf handlers for this operator.

        self.register_handlers()

        # Determine if the operator should be run clusterwide or in specific
        # namespaces.

        clusterwide = False

        if not self.namespaces:
            clusterwide = True

        # Run the operator in a separate event loop, waiting for the stop flag
        # to be set, at which point the operator will be stopped and this thread
        # will exit.

        while not self._stop_flag.is_set():
            event_loop = asyncio.new_event_loop()

            asyncio.set_event_loop(event_loop)

            logger.info("Starting managed cluster operator for %s.", self.cluster_name)

            # Serve only the resources found at startup. Otherwise kopf lists
            # and watches CustomResourceDefinitions in the managed cluster,
            # which its credentials need not grant, and logs each failed retry
            # as an error. This also stops namespace observation, so any
            # namespaces given must be exact names, not patterns. The registry
            # has no startup handler, so the settings are passed in here.

            settings = kopf.OperatorSettings()
            settings.scanning.disabled = True

            with contextlib.closing(event_loop):
                try:
                    event_loop.run_until_complete(
                        kopf.operator(
                            registry=self.operator_registry,
                            settings=settings,
                            clusterwide=clusterwide,
                            namespaces=self.namespaces,
                            memo=self.service_state,
                            stop_flag=self._stop_flag,
                        )
                    )

                except (
                    aiohttp.client_exceptions.ClientConnectorError,
                    aiohttp.client_exceptions.ClientConnectorCertificateError,
                ):
                    # If the operator exits due to a connection error it means it
                    # could not connect to the cluster on initial startup. After
                    # a short delay, the operator will be restarted. Note that
                    # this only applied to the initial connecttion. If the operator
                    # loses connection to the cluster while running, it will not
                    # be restarted and what instead happens is that kopf will
                    # continually attempt to reconnect to the cluster.
                    #
                    # TODO: Need to find a way to get from kopf a notification
                    # that the watchers are failing so can try to reconnect
                    # or tale some other action.

                    logger.exception(
                        "Connection error, restarting operator after delay."
                    )

                    time.sleep(5.0)

    def cancel(self) -> None:
        """Flags the kopf operator to stop."""

        # Set the stop flag to stop the operator. This will cause the event loop
        # to stop running and the operator thread to exit.

        self._stop_flag.set()

    def run_until_stopped(self, stopped: kopf.DaemonStopped) -> None:
        """Run the operator until stopped."""

        self.start()

        while not stopped:
            # We should be called from a traditional thread so it is safe to use
            # blocking sleep call.

            time.sleep(1.0)

        self.cancel()

        self.join()
