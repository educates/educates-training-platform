"""This is a reference implementation for ssh proxy command which can tunnel
SSH into a workshop session. The argument should be the "wss://*/tunnel/" URL
for accessing the tunnelling proxy, or the "ws://*/tunnel/" URL when the
session is served over plain HTTP.

"""

import asyncio
import sys

import websockets
from websockets.asyncio.client import connect


async def connect_stdin_stdout():
    loop = asyncio.get_running_loop()

    reader = asyncio.StreamReader()
    protocol = asyncio.StreamReaderProtocol(reader)
    await loop.connect_read_pipe(lambda: protocol, sys.stdin)

    w_transport, w_protocol = await loop.connect_write_pipe(
        asyncio.streams.FlowControlMixin, sys.stdout
    )
    writer = asyncio.StreamWriter(w_transport, w_protocol, reader, loop)

    return reader, writer


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

    except websockets.exceptions.ConnectionClosedError:
        pass

    except ConnectionResetError:
        pass

    else:
        await websocket.close()


async def proxy(uri):
    reader, writer = await connect_stdin_stdout()

    try:
        async with connect(uri) as websocket:
            try:
                pipe1 = copy_websocket_to_socket(websocket, writer)
                pipe2 = copy_socket_to_websocket(reader, websocket)

                await asyncio.wait(
                    [asyncio.Task(pipe1), asyncio.Task(pipe2)],
                    return_when=asyncio.FIRST_COMPLETED,
                )
            finally:
                await websocket.close()

    except websockets.exceptions.InvalidStatus:
        pass


asyncio.run(proxy(sys.argv[1]))
