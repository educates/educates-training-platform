import os
import asyncio
import contextlib
import logging
import signal
import socket
import time

from threading import Thread, Event

import kopf
import pykube


logging.basicConfig(level=logging.INFO)

logging.getLogger("aiohttp.access").setLevel(logging.WARNING)
logging.getLogger("kopf.activities.probe").setLevel(logging.WARNING)
logging.getLogger("kopf.objects").setLevel(logging.WARNING)

logger = logging.getLogger("educates")
logger.setLevel(os.environ.get("LOG_LEVEL", "INFO").upper())

logger.info(
    "Logging level set to %s.", logging.getLevelName(logger.getEffectiveLevel())
)


def check_dns_is_ready():
    # Check that DNS is actually ready and able to resolve the DNS for the
    # Kubernetes control plane. This is a workaround for the fact that the DNS
    # service may not be ready when the pod starts. Check at intervals but bail
    # out and raise the original exception if we can't resolve the DNS name
    # after 60 seconds.

    logger.info("Checking DNS resolution for Kubernetes control plane.")

    start_time = time.time()

    while True:
        try:
            socket.getaddrinfo("kubernetes.default.svc", 0, flags=socket.AI_CANONNAME)
            break
        except socket.gaierror:
            if time.time() - start_time > 60:
                raise

            # Wait for 1 second before trying again.

            logger.info(
                "DNS resolution for Kubernetes control plane is not ready yet, sleeping..."
            )

            time.sleep(1)

    logger.info("DNS resolution for Kubernetes control plane is ready.")


# Check that DNS is actually ready before importing the handlers.

check_dns_is_ready()


from handlers import namespace
from handlers import secret
from handlers import secretcopier
from handlers import secretexporter
from handlers import secretimporter
from handlers import secretinjector
from handlers import serviceaccount

_event_loop = None  # pylint: disable=invalid-name

_stop_flag = Event()


@kopf.on.startup()
def configure(settings: kopf.OperatorSettings, **_):
    # Only post log messages as Kubernetes events when the log level is set to
    # WARNING, ERROR or higher. Setting at INFO or DEBUG level generates too
    # much noise in events history.

    settings.posting.level = logging.WARNING

    settings.watching.connect_timeout = 1 * 60
    settings.watching.server_timeout = 5 * 60
    settings.watching.client_timeout = settings.watching.server_timeout + 10

    # Serve only the resources found at startup. Otherwise kopf lists and
    # watches CustomResourceDefinitions cluster wide, which the service account
    # has no RBAC for, and logs each failed retry as an error. This also stops
    # namespace observation, which only namespace patterns need.

    settings.scanning.disabled = True


@kopf.on.login()
def login_fn(**kwargs):
    return kopf.login_via_pykube(**kwargs)


@kopf.on.probe(id="api")
def check_api_access(**kwargs):
    try:
        api = pykube.HTTPClient(pykube.KubeConfig.from_env())
        pykube.Namespace.objects(api).get(name="default")

    except pykube.exceptions.KubernetesError:
        logger.error("Failed request to Kubernetes API.")

        raise


@kopf.on.cleanup()
async def cleanup_fn(logger, **kwargs):
    logger.info("Stopping kopf framework main loop.")

    # Workaround for possible kopf bug, set stop flag.

    _stop_flag.set()


def run_kopf():
    """Run kopf in a separate thread and wait for it to complete."""

    def worker():
        logger.info("Starting kopf framework main loop.")

        # Need to create and set the event loop since this isn't being
        # called in the main thread.

        global _event_loop  # pylint: disable=invalid-name,global-statement

        _event_loop = asyncio.new_event_loop()
        asyncio.set_event_loop(_event_loop)

        with contextlib.closing(_event_loop):
            # Run event loop until flagged to shutdown.

            _event_loop.run_until_complete(
                kopf.operator(
                    clusterwide=True,
                    stop_flag=_stop_flag,
                    liveness_endpoint="http://0.0.0.0:8080/healthz",
                )
            )

            logger.info("Closing asyncio event loop.")

        logger.info("Exiting kopf framework main loop.")

    thread = Thread(target=worker)

    # Startup kopf framework.

    thread.start()

    return thread


def shutdown(signum, frame):
    logger.info("Signal handler called with signal %s.", signum)
    if _event_loop:
        _stop_flag.set()


if __name__ == "__main__":
    signal.signal(signal.SIGINT, shutdown)
    signal.signal(signal.SIGTERM, shutdown)

    thread = run_kopf()
    thread.join()
