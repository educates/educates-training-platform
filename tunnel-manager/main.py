import asyncio
import logging
import signal
import os

import websockets

import kopf
import pykube

logger = logging.getLogger("educates")

ingress_domain = os.environ.get("INGRESS_DOMAIN", "educates-local-dev.test")
environment_name = os.environ.get("ENVIRONMENT_NAME", "")

event_loop = None
stop_flag = None

sessions = {}


def xgetattr(obj, key, default=None):
    """Looks up a property within an object using a dotted path as key.
    If the property isn't found, then return the default value.

    """

    keys = key.split(".")
    value = default

    for key in keys:
        value = obj.get(key)
        if value is None:
            return default

        obj = value

    return value


@kopf.on.startup()
def configure(settings: kopf.OperatorSettings, **_):
    settings.posting.level = logging.DEBUG
    settings.watching.connect_timeout = 1 * 60
    settings.watching.server_timeout = 5 * 60
    settings.watching.client_timeout = settings.watching.server_timeout + 10


@kopf.on.login()
def login_fn(**kwargs):
    return kopf.login_via_pykube(**kwargs)


@kopf.on.probe(id="api")
def check_api_access(**kwargs):
    try:
        api = pykube.HTTPClient(pykube.KubeConfig.from_env())
        pykube.Namespace.objects(api).get(name="default")

    except pykube.exceptions.KubernetesError:
        logger.error("failed request to Kubernetes API")

        raise


@kopf.on.cleanup()
async def cleanup_fn(logger, **kwargs):
    logger.info("stopping kopf framework main loop.")

    # Workaround for possible kopf bug, set stop flag.

    stop_flag.set_result(None)


@kopf.on.event("training.educates.dev", "v1beta1", "workshopsessions")
def workshop_session_event(name, type, event, logger, **_):
    obj = event["object"]

    ingress = f"{name}.{ingress_domain}"
    namespace = xgetattr(obj, "spec.environment.name", "")
    port = 22

    # Only process workshop sessions associated with the workshop environment
    # we care about, ignore everything else.

    if namespace != environment_name:
        return

    if type == "DELETED":
        if name in sessions:
            logger.info("deleting session record %s", name)
            sessions.pop(name, None)
            return

    # Only process workshop sessions where sshd is enabled and tunnel is also
    # enabled. In practice it shouldn't be necessary to test for either of
    # these as this tunnel shouldn't even be running if they weren't set in the
    # workshop definition, but check anyway.

    sshd_enabled = xgetattr(obj, "status.educates.sshd.enabled", False)
    sshd_tunnel_enabled = xgetattr(obj, "status.educates.sshd.tunnel.enabled", False)

    if sshd_enabled and sshd_tunnel_enabled:
        logger.info("updating session record %s", name)

        # The sessions table is keyed by the host ingress which should be used
        # to access the workshop session using the tunnel.

        sessions[ingress] = {
            "name": name,
            "host": f"{name}.{namespace}",
            "port": port,
        }


def get_endpoint_details(session):
    """Find user authenticated by token or return None."""
    return sessions.get(session)


async def copy_websocket_to_socket(websocket, socket):
    try:
        while data := await websocket.recv():
            socket.write(data)

    except websockets.exceptions.ConnectionClosedOK:
        pass

    except websockets.exceptions.ConnectionClosedError:
        pass

    finally:
        socket.close()


async def copy_socket_to_websocket(socket, websocket):
    try:
        while data := await socket.read(2048):
            await websocket.send(data)

    except websockets.exceptions.ConnectionClosedOK:
        pass

    else:
        await websocket.close()


async def websocket_proxy(websocket, path):
    ingress = websocket.request_headers["Host"]
    details = get_endpoint_details(ingress)

    # If can't find a workshop session for the host ingress then we just
    # close the connection straight away.

    if details is None:
        await websocket.close()
        return

    logger.info("ingress hostname %s", ingress)
    logger.info("accepted connection for session %s", details["name"])
    logger.info(
        "proxying connection to service %s:%s", details["host"], details["port"]
    )

    reader, writer = await asyncio.open_connection(details["host"], details["port"])

    try:
        pipe1 = copy_websocket_to_socket(websocket, writer)
        pipe2 = copy_socket_to_websocket(reader, websocket)

        await asyncio.wait(
            [asyncio.Task(pipe1), asyncio.Task(pipe2)],
            return_when=asyncio.FIRST_COMPLETED,
        )

    finally:
        await websocket.close()

    logger.info("closing connection for session %s", details["name"])


async def proxy_server(stop_flag):
    async with websockets.serve(websocket_proxy, host="", port=8080):
        await stop_flag

        logger.info("proxy server stopped")


async def kopf_operator(stop_flag):
    await kopf.operator(clusterwide=True, stop_flag=stop_flag)

    logger.info("kopf operator stopped")


async def main():
    global stop_flag
    global event_loop

    loop = asyncio.get_running_loop()

    stop_flag = loop.create_future()

    loop.add_signal_handler(signal.SIGINT, stop_flag.set_result, None)
    loop.add_signal_handler(signal.SIGTERM, stop_flag.set_result, None)

    await asyncio.gather(proxy_server(stop_flag), kopf_operator(stop_flag))


if __name__ == "__main__":
    logging.basicConfig(level=logging.INFO)
    logger.setLevel(logging.DEBUG)

    asyncio.run(main())
