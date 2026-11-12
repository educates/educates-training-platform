"""Defines initialization functions for starting up kopf operator framework
in a separate thread.

"""

import os
import signal
import asyncio
import contextlib
import functools
import logging

from threading import Thread, Event

from asgiref.sync import sync_to_async

import mod_wsgi
import kopf
import pykube
import requests

logger = logging.getLogger("educates")

_event_loop = None  # pylint: disable=invalid-name


class Task:
    """Encapsulation of an instance of a background task. It binds the
    function implementing the task with the arguments to use when calling it,
    along with details of when to call it and whether it should run as a
    periodic task.

    """

    def __init__(
        self, wrapped, name, delay, repeat, args, kwargs
    ):  # pylint: disable=too-many-arguments
        """Capture details of the function implementing the task and the
        parameters for invoking it.

        """

        self.wrapped = wrapped
        self.name = name
        self.delay = delay
        self.repeat = repeat
        self.args = args
        self.kwargs = kwargs

    def execute(self):
        """Execute the function for the task synchronously."""

        return self.wrapped(*self.args, **self.kwargs)

    def schedule(self, *, delay=None):
        """Schedule a task to be run using the asyncio library. The delay
        before running the task which was originally specified can be
        overridden at this point if necessary.

        """

        delay = delay or self.delay

        async def sleep():
            if delay > 0.0:
                await asyncio.sleep(delay)

        @sync_to_async(thread_sensitive=False)
        def execute():
            """Executes the function, capturing details of any exception and
            logging it since nothing will even wait on the results.

            """

            try:
                logger.debug(
                    "Executing task %s %s %s.", self.name, self.args, self.kwargs
                )
                return self.wrapped(*self.args, **self.kwargs)

            except Exception:  # pylint: disable=broad-except
                logger.exception("Exception raised by task %s.", self.name)

        async def task():
            """The asynchronous task wrapper for the function. Handles once
            off execution as well as looping forever with delay.

            """

            await sleep()

            if self.repeat:
                while True:
                    await execute()
                    await sleep()

            else:
                return await execute()

        # This schedules the task to be run by the asyncio loop. This is
        # done using threadsafe registration mechanism as can be called by
        # request handler thread of mod_wsgi and not necessarily a function
        # executing under asyncio.

        return asyncio.run_coroutine_threadsafe(task(), _event_loop)


def background_task(wrapped=None, *, name=None, delay=0.0, repeat=False):
    """Designates a synchronous function as an asynchronous task. The function
    can be defined to be executed once, or set up to be called repeatedly.

    """

    # Parameters for the task are optional so if any were supplied return
    # a partially bound version of the decorator function for subsequent
    # applyication to the function implementing the task.

    if wrapped is None:
        return functools.partial(background_task, delay=delay, repeat=repeat)

    name = name or wrapped.__qualname__

    # Return a function which encapsulates the details of the task and which
    # in turn returns a task wrapper object. Code using this still needs to
    # called either execute() or schedule() on the task object which is
    # returned.

    def wrapper(*args, **kwargs):
        return Task(wrapped, name, delay, repeat, args, kwargs)

    return wrapper


ready_flag = Event()
stop_flag = Event()


@kopf.on.startup()
def configure(settings: kopf.OperatorSettings, **_):
    settings.posting.level = logging.DEBUG
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

    except pykube.exceptions.KubernetesError as exc:
        logger.exception("Failed request to Kubernetes API.")

        raise kopf.PermanentError("Failed request to Kubernetes API.") from exc

@kopf.on.probe(id="portal")
def check_portal_access(**kwargs):
    try:
        headers = {'User-Agent': 'training-portal-probe/1.0.0'}
        res = requests.get('http://localhost:8080/accounts/login/', headers=headers)

    except requests.exceptions.RequestException as exc:
        logger.exception("Failed request to portal interface.")

        raise kopf.PermanentError("Failed request to portal interface.") from exc

    if res.status_code != 200:
        logger.error("Failed request to portal interface.")

        raise kopf.PermanentError("Unexpected HTTP response from portal")


@kopf.on.cleanup()
async def cleanup_fn(logger, **kwargs):
    logger.info("Stopping kopf framework main loop.")

    # If stop flag not already set, then kopf has decided to shutdown itself
    # which means we need to signal the process itself to shutdown. Note that
    # the whole pod isn't restarted, just the process. After restart of the
    # process if there is a still a problem which means kopf can't run, then
    # kopf will not start up again and loading of WSGI application should hang.
    # The result should be that readiness probe fails and pod is restarted.

    if not stop_flag.is_set():
        os.kill(os.getpid(), signal.SIGTERM)

    # Workaround for possible kopf bug, set stop flag. Since we are killing
    # the process if it wasn't set, this is probably redundant, but do it
    # anyway so process shuts down promptly and doesn't hang on kopf resulting
    # in forced process shutdown.

    stop_flag.set()


def initialize_kopf():
    """Run kopf in a separate thread and register a shutdown handler with
    mod_wsgi to ensure we clean things up properly on process shutdown.

    """

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
                    ready_flag=ready_flag,
                    stop_flag=stop_flag,
                    liveness_endpoint="http://0.0.0.0:8081/healthz",
                )
            )

            logger.info("Closing asyncio event loop.")

        logger.info("Exiting kopf framework main loop.")

    thread = Thread(target=worker)

    # Register the shutdown handler with mod_wsgi so everything cleaned
    # up properly when the process is being stopped.

    def shutdown_handler(*_, **__):
        stop_flag.set()
        thread.join()

    mod_wsgi.subscribe_shutdown(shutdown_handler)  # pylint: disable=no-member

    # Startup kopf framework.

    thread.start()

    # Wait until kopf has initialized before continuing.

    ready_flag.wait()
