"""Main entry point for the lookup service. This module starts the kopf
operator framework and the aiohttp server for handling REST API requests."""

import asyncio
import contextlib
import logging
import os
import signal
import threading

import aiohttp
import kopf
import pykube

from .caches.databases import client_database, cluster_database, tenant_database
from .handlers import clients as _  # pylint: disable=unused-import
from .handlers import clusters as _  # pylint: disable=unused-import
from .handlers import tenants as _  # pylint: disable=unused-import
from .routes import register_routes
from .service import ServiceState


# Set up logging for the educates operator.

logging.getLogger("kopf.activities.probe").setLevel(logging.WARNING)
logging.getLogger("kopf.objects").setLevel(logging.WARNING)

logger = logging.getLogger("educates")


# Configuration to namespace to monitor for configuration resources.

OPERATOR_NAMESPACE = os.getenv("OPERATOR_NAMESPACE", "educates-config")

# Register the operator handlers for the training platform operator.
#
# TODO: These handler registrations are done against the global kopf registry
# and thus will not apply to secondary operator instances which are created in
# separate threads later as they will use their own registry. This means
# liveness probes aren't currently checking access to secondary clusters, and
# the settings made below don't apply to them either: GenericOperator passes
# them the scanning setting itself, but not the posting and watching ones.


@kopf.on.startup()
def configure(settings: kopf.OperatorSettings, **_) -> None:
    """Configures the kopf operator settings."""

    settings.posting.level = logging.ERROR
    settings.watching.connect_timeout = 1 * 60
    settings.watching.server_timeout = 5 * 60
    settings.watching.client_timeout = settings.watching.server_timeout + 10

    # Serve only the resources found at startup. Otherwise kopf lists and
    # watches CustomResourceDefinitions cluster wide, which the service account
    # has no RBAC for, and logs each failed retry as an error. This also stops
    # namespace observation, which only namespace patterns need, and the
    # operator names its one namespace exactly.

    settings.scanning.disabled = True


@kopf.on.login()
def login_fn(**kwargs) -> dict:
    """Returns login credentials to be used by the kopf operator framework using
    the pykube library so that the operator framework is using the same means of
    getting credentials as the pykube library."""

    return kopf.login_via_pykube(**kwargs)


@kopf.on.probe(id="api")
def check_api_access(**_) -> None:
    """Checks if we can access the Kubernetes API for the liveness probe. The
    kopf framework will handle the response to the liveness probe based on
    the result of this function. The kopf operator framework will also do
    basic checks to determine if the operator is still running and if it is
    able to process events."""

    try:
        api = pykube.HTTPClient(pykube.KubeConfig.from_env())
        pykube.Namespace.objects(api).get(name="default")

    except pykube.exceptions.KubernetesError:
        logger.error("Failed liveness probe request to Kubernetes API.")

        raise


# Process variables and shutdown handling. Signal handlers run in the main
# thread so we need to use global event objects to signal the kopf framework
# and HTTP server, which run in separate threads, to stop processing.

_kopf_main_process_thread = None  # pylint: disable=invalid-name
_kopf_main_event_loop = None  # pylint: disable=invalid-name

_aiohttp_main_process_thread = None  # pylint: disable=invalid-name
_aiohttp_main_event_loop = None  # pylint: disable=invalid-name

_shutdown_server_process_flag = threading.Event()


def shutdown_server_process(signum: int, *_) -> None:
    """Signal handler for shutting down the server process. This will set the
    stop flag for the kopf framework and HTTP server to stop processing."""

    logger.info("Signal handler called with signal %s.", signum)

    if _kopf_main_event_loop:
        _shutdown_server_process_flag.set()


def register_signal_handlers() -> None:
    """Registers signal handlers for the server process. This will allow the
    server process to be shutdown cleanly when a signal is received."""

    signal.signal(signal.SIGINT, shutdown_server_process)
    signal.signal(signal.SIGTERM, shutdown_server_process)


@kopf.on.cleanup()
async def cleanup_fn(**_) -> None:
    """Cleanup function for the operator."""

    # This is a workaround for a possible bug in kopf where the cleanup function
    # isn't being called when the operator is stopped. This sets the stop flag
    # for the operator to stop processing again. This may no longer be required.

    _shutdown_server_process_flag.set()


# Global data structures to be shared across the kopf operator and uvicorn
# server threads.

service_state = ServiceState(
    client_database=client_database,
    tenant_database=tenant_database,
    cluster_database=cluster_database,
)


def run_kopf() -> threading.Thread:
    """Run kopf in a separate thread."""

    def worker_thread():
        logger.info("Starting kopf framework main loop.")

        # Need to create and set the event loop since this isn't being
        # called in the main thread.

        global _kopf_main_event_loop  # pylint: disable=global-statement

        _kopf_main_event_loop = asyncio.new_event_loop()
        asyncio.set_event_loop(_kopf_main_event_loop)

        # Run the kopf operator framework until the shutdown flag is set.

        with contextlib.closing(_kopf_main_event_loop):
            _kopf_main_event_loop.run_until_complete(
                kopf.operator(
                    clusterwide=False,
                    namespaces=[OPERATOR_NAMESPACE],
                    stop_flag=_shutdown_server_process_flag,
                    memo=service_state,
                    liveness_endpoint="http://0.0.0.0:8081/healthz",
                )
            )

    # Start the kopf operator framework in a separate thread.

    thread = threading.Thread(target=worker_thread)
    thread.start()

    return thread


def run_aiohttp() -> threading.Thread:
    """Run aiohttp in a separate thread."""

    aiohttp_app = aiohttp.web.Application()

    aiohttp_app["service_state"] = service_state

    register_routes(aiohttp_app)

    runner = aiohttp.web.AppRunner(aiohttp_app)

    async def wait_for_process_shutdown() -> None:
        """Wait for the server process to shutdown and then shutdown the HTTP
        server."""

        # Wait for the shutdown flag to be set.

        while not _shutdown_server_process_flag.is_set():
            await asyncio.sleep(1)

        # Shutdown the aiohttp server.

        await runner.cleanup()

    def worker_thread() -> None:
        """Worker thread for running the HTTP server."""

        # Need to create a separate event loop for the HTTP server since this
        # isn't being called in the main thread.

        global _aiohttp_main_event_loop  # pylint: disable=global-statement

        _aiohttp_main_event_loop = asyncio.new_event_loop()
        asyncio.set_event_loop(_aiohttp_main_event_loop)

        async def run_app() -> None:
            await runner.setup()
            site = aiohttp.web.TCPSite(runner, "0.0.0.0", 8080)
            await site.start()

        with contextlib.closing(_aiohttp_main_event_loop):
            _aiohttp_main_event_loop.run_until_complete(
                asyncio.gather(run_app(), wait_for_process_shutdown())
            )

    # Start the HTTP server in a separate thread.

    thread = threading.Thread(target=worker_thread)
    thread.start()

    return thread


# Main entry point for the educates operator. This will start the kopf operator
# framework and the HTTP server.

if __name__ == "__main__":

    # Set up logging for the educates operator.

    logging.basicConfig(level=logging.INFO)
    logger.setLevel(logging.INFO)

    # Suppress verbose logging from urllib3 if ever set general log level to
    # more verbose setting.

    logging.getLogger("urllib3.connectionpool").setLevel(logging.INFO)

    # Register signal handlers for the server process.

    register_signal_handlers()

    # Start the kopf framework and HTTP server threads.

    _kopf_main_process_thread = run_kopf()
    _aiohttp_main_process_thread = run_aiohttp()

    # Wait for the kopf framework and HTTP server threads to complete. This
    # will block until the threads are finished which will only occur when the
    # shutdown process signal is received.

    _kopf_main_process_thread.join()
    _aiohttp_main_process_thread.join()
