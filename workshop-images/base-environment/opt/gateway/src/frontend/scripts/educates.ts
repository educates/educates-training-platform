import * as url from "url"

import * as $ from "jquery"

import * as bootstrap from "bootstrap"

import { FitAddon } from "@xterm/addon-fit"
import { WebLinksAddon } from "@xterm/addon-web-links"
import { Terminal } from "@xterm/xterm"

import { ResizeSensor } from "css-element-queries"

import * as amplitude from '@amplitude/analytics-browser'

const FontFaceObserver = require("fontfaceobserver")

const _ = require("lodash")

const Split = require("split.js")

declare var gtag: Function
declare var clarity: Function

function is_touch_device() {
    return 'ontouchstart' in window || navigator.maxTouchPoints > 0
}

function is_ios() {
    return /iPad|iPhone|iPod/.test(navigator.userAgent)
}

function string_to_slug(str: string) {
    str = str.trim()
    str = str.toLowerCase()

    return str
        .replace(/[^a-z0-9 -]/g, "") // remove invalid chars
        .replace(/\s+/g, "-") // collapse whitespace and replace by -
        .replace(/-+/g, "-") // collapse dashes
        .replace(/^-+/, "") // trim - from start of text
        .replace(/-+$/, "") // trim - from end of text
}

async function send_analytics_event(event: string, data = {}, timeout = 0) {
    let payload = {
        event: {
            name: event,
            data: data
        }
    }

    console.log("Sending analytics event:", JSON.stringify(payload))

    let $body = $("body")

    let send_to_webhook = function (): Promise<unknown> {
        return new Promise(function (resolve, reject) {
            $.ajax({
                type: "POST",
                url: "/session/event",
                contentType: "application/json",
                data: JSON.stringify(payload),
                dataType: "json",
                success: function (data) {
                    resolve(data)
                },
                error: function (err) {
                    reject(err)
                }
            })
        })
    }

    let tasks: Promise<unknown>[] = [send_to_webhook().then(() => {
        // console.log("Sent analytics event to webhook", event)
    })]

    if ($body.data("google-tracking-id")) {
        let send_to_google = function (): Promise<unknown> {
            return new Promise((resolve) => {
                let callbacks = {
                    "event_callback": (arg) => resolve(arg)
                }

                gtag("event", event, Object.assign({}, callbacks, data))
            })
        }

        tasks.push(send_to_google().then(() => {
            // console.log("Sent analytics event to Google", event)
        }))
    }

    if ($body.data("amplitude-tracking-id")) {
        let send_to_amplitude = function (): Promise<unknown> {
            let globals = {
                "workshop_name": $body.data("workshop-name"),
                "session_name": $body.data("session-namespace"),
                "environment_name": $body.data("workshop-namespace"),
                "training_portal": $body.data("training-portal"),
                "ingress_domain": $body.data("ingress-domain"),
                "ingress_protocol": $body.data("ingress-protocol"),
                "session_owner": $body.data("session-owner"),
            }

            return amplitude.track(event, Object.assign({}, globals, data)).promise
        }

        tasks.push(send_to_amplitude().then(() => {
            // console.log("Sent analytics event to Amplitude", event)
        }))
    }

    function abort_after_ms(ms): Promise<unknown> {
        return new Promise(resolve => setTimeout(resolve, ms))
    }

    if (timeout) {
        try {
            await Promise.race([
                Promise.all(tasks).then(() => {
                    // console.log("Sent analytics event to all consumers", event)
                }),
                abort_after_ms(timeout)
            ])
        }
        catch (err) {
            console.log("Error sending analytics event", event, err)
        }
    }
    else {
        Promise.all(tasks).then(() => {
            // console.log("Sent analytics event to all consumers", event)
        })
    }
}

enum MessagesPacketType {
    HELLO,
    PING,
    MESSAGE,
}

interface MessagesHelloPacketArgs {
}

interface MessagesPacket {
    type: MessagesPacketType
    id: string
    args?: any
}

class MessagesChannel {
    private id: string
    private socket: WebSocket
    private sequence: number
    private reconnecting: boolean
    private reconnectTimer: any
    private reconnectStartTime: number
    private reconnectDelay: number
    private shutdown: boolean

    // Reconnection backoff configuration
    private readonly initialReconnectDelay: number = 250      // Start with 250ms delay
    private readonly maxReconnectDelay: number = 5000         // Max 5 seconds between attempts
    private readonly fastReconnectPeriod: number = 10000      // Fast reconnects for 10 seconds
    private readonly backoffMultiplier: number = 1.5          // Multiply delay by this after fast period
    private readonly abandonTimeout: number = 300000          // Give up after 5 minutes

    constructor(id: string) {
        this.id = id

        this.sequence = -1

        this.shutdown = false
        this.reconnecting = false
        this.reconnectStartTime = 0
        this.reconnectDelay = this.initialReconnectDelay

        this.configure_session()
    }

    private configure_session() {
        let parsed_url = url.parse(window.location.origin)

        let protocol = parsed_url.protocol == "https:" ? "wss" : "ws"
        let host = parsed_url.host
        let pathname = "/message/server"

        let server_url = `${protocol}://${host}${pathname}`

        console.log("Configure channel for messages", this.id)

        this.socket = new WebSocket(server_url)

        this.configure_handlers()
    }

    private configure_handlers() {
        if (this.shutdown)
            return

        console.log("Configure handlers for messages", this.id)

        this.socket.onerror = (event) => {
            console.error("WebSocket error observed:", event)
        }

        let socket: WebSocket = this.socket

        this.socket.onopen = async () => {
            // If the socket isn't the one currently associated with the
            // terminal then bail out straight away as some sort of mixup has
            // occurred. Close the socket for good measure.

            console.log("Connection opened for messages", this.id)

            if (this.reconnectTimer) {
                console.log("Clear reconnection timeout for messages", this.id)
                clearTimeout(this.reconnectTimer)
                this.reconnectTimer = null
            }

            if (socket !== this.socket) {
                console.warn("Multiple connections for messages", this.id)
                socket.close()
                return
            }

            this.reconnecting = false
            this.reconnectStartTime = 0
            this.reconnectDelay = this.initialReconnectDelay

            let args: MessagesHelloPacketArgs = {
            }

            this.send_message(MessagesPacketType.HELLO, args)

            // A sequence number of -1 means this is a completely new session.
            // In this case we need to setup the callback for receiving messages
            // over the channel and initiate the pings. We can only do this once
            // else we get duplicate registrations if we have to reconnect
            // because the connection is dropped.

            if (this.sequence == -1) {
                this.initiate_pings()

                // Set sequence number to 0 so we don't do this all again.

                this.sequence = 0
            }
            else {
                console.log("Re-connecting messages channel", this.id)
            }
        }

        this.socket.onmessage = async (evt) => {
            // If the socket isn't the one currently associated with the
            // channel then bail out straight away as some sort of mixup has
            // occurred. Close the socket for good measure.

            if (this.socket === null) {
                console.warn("Connection was abandoned for channel", this.id)
                socket.close()
                return
            }

            if (socket !== this.socket) {
                console.warn("Multiple connections for message channel", this.id)
                socket.close()
                return
            }

            let packet: MessagesPacket = JSON.parse(evt.data)

            if (packet.id == this.id) {
                switch (packet.type) {
                    case (MessagesPacketType.MESSAGE): {
                        let handler = action_table[packet.args.action]
                        if (handler !== undefined)
                            handler(packet.args.data)
                    }
                }
            }
            else {
                console.warn("Client session " + this.id + " received message for session " + packet.id)
            }
        }

        this.socket.onclose = (_evt: any) => {
            // If the socket isn't the one currently associated with the message
            // channel then bail out straight away as some sort of mixup has
            // occurred.

            if (socket !== this.socket)
                return

            let self = this

            this.socket = null

            if (this.shutdown)
                return

            function connect() {
                if (self.shutdown)
                    return

                let parsed_url = url.parse(window.location.origin)

                let protocol = parsed_url.protocol == "https:" ? "wss" : "ws"
                let host = parsed_url.host
                let pathname = "/message/server"

                let server_url = `${protocol}://${host}${pathname}`

                console.log("Attempt re-connect for message channel", self.id, "delay was", self.reconnectDelay, "ms")

                self.socket = new WebSocket(server_url)

                self.configure_handlers()
            }

            console.log("Messages connection was lost", self.id)

            // Track when reconnection started
            if (!this.reconnecting) {
                this.reconnectStartTime = Date.now()
                this.reconnectDelay = this.initialReconnectDelay
            }

            // Calculate the appropriate delay based on how long we've been trying
            let elapsed = Date.now() - this.reconnectStartTime

            if (elapsed > this.fastReconnectPeriod) {
                // After the fast reconnect period, increase the delay with backoff
                this.reconnectDelay = Math.min(
                    this.reconnectDelay * this.backoffMultiplier,
                    this.maxReconnectDelay
                )
            }

            setTimeout(connect, this.reconnectDelay)

            async function terminate() {
                self.reconnectTimer = null

                if (!self.reconnecting)
                    return

                console.log("Abandoning connection for messages", self.id)

                self.reconnecting = false
                self.shutdown = true

                self.socket = null
            }

            if (!this.reconnecting) {
                console.log("Trigger reconnection timeout for messages", self.id)

                self.reconnectTimer = setTimeout(terminate, self.abandonTimeout)

                this.reconnecting = true
            }
        }
    }

    private initiate_pings() {
        let self = this

        // Ping messages are only sent from client to backend server. Some
        // traffic is required when the session is otherwise idle, else you
        // can't tell if the connection has been dropped.

        function ping() {
            self.send_message(MessagesPacketType.PING)
            setTimeout(ping, 15000)
        }

        setTimeout(ping, 15000)
    }

    private send_message(type: MessagesPacketType, args?: any): boolean {
        if (!this.socket)
            return false

        if (this.socket.readyState === WebSocket.OPEN) {
            let packet: MessagesPacket = {
                type: type,
                id: this.id
            }

            if (args !== undefined)
                packet["args"] = args

            this.socket.send(JSON.stringify(packet))

            return true
        }

        return false
    }
}

enum TerminalsPacketType {
    HELLO,
    PING,
    DATA,
    RESIZE,
    EXIT,
    ERROR
}

interface TerminalsPacket {
    type: TerminalsPacketType
    id: string
    args?: any
}

interface TerminalsHelloPacketArgs {
    token: string
    cols: number
    rows: number
    seq: number
}

interface InboundDataPacketArgs {
    data: string
    sync: boolean
    seq: number
}

interface OutboundDataPacketArgs {
    data: string
}

interface ResizePacketArgs {
    cols: number
    rows: number
}

interface ErrorPacketArgs {
    reason: string
}

interface BufferedDataBlock {
    text: string
    bracketed: boolean
}

class TerminalSession {
    private id: string
    private element: HTMLElement
    private endpoint: string
    private terminal: Terminal
    private fitter: FitAddon
    private sensor: ResizeSensor
    private socket: WebSocket
    private started: Date
    private synced: boolean
    private sequence: number
    private blocked: boolean
    private buffer: BufferedDataBlock[]
    private reconnecting: boolean
    private reconnectTimer: any
    private reconnectStartTime: number
    private reconnectDelay: number
    private shutdown: boolean

    // Reconnection backoff configuration
    private readonly initialReconnectDelay: number = 250      // Start with 250ms delay
    private readonly maxReconnectDelay: number = 5000         // Max 5 seconds between attempts
    private readonly fastReconnectPeriod: number = 10000      // Fast reconnects for 10 seconds
    private readonly backoffMultiplier: number = 1.5          // Multiply delay by this after fast period
    private readonly abandonTimeout: number = 300000          // Give up after 5 minutes

    constructor(context: string, id: string, element: HTMLElement, endpoint: string) {
        this.id = id

        // If a context is supplied, prefix the terminal session ID used for
        // the backend with the context. This means can have similarly named
        // terminal IDs used on frontend, but in different contexts on the
        // backend so they don't clash.

        if (context)
            this.id = `${context}:${id}`

        this.element = element
        this.endpoint = endpoint

        this.synced = false
        this.sequence = -1

        this.blocked = true
        this.buffer = []

        this.shutdown = false
        this.reconnecting = false
        this.reconnectStartTime = 0
        this.reconnectDelay = this.initialReconnectDelay

        this.terminal = new Terminal({
            cursorBlink: true,
            fontFamily: "SourceCodePro"
        })

        this.fitter = new FitAddon()
        this.terminal.loadAddon(this.fitter)

        // We override the normal behavior of the terminal to open links as the
        // standard implementation creates a blank window and then tries to
        // update the target URL afterwards, but this fails when xterm.js is
        // used embedded in an iframe. Supplying the target URL at the time of
        // opening the window appears to work fine.

        function handleLinkClick(event: MouseEvent, uri: string): void {
            window.open(uri, '_blank');
        }

        this.terminal.loadAddon(new WebLinksAddon(handleLinkClick))

        // Ensure that the element the terminal is contained in is visible.
        // This is just to ensure that dimensions can be correctly calculated.

        let self = this

        function wait_until_visible() {
            if (!self.element.offsetParent)
                setTimeout(wait_until_visible, 300)
            else
                self.configure_session()
        }

        wait_until_visible()
    }

    private configure_session() {
        this.terminal.open(this.element)

        // We fit the window now. The size details will be sent with the
        // initial hello message sent to the terminal server.

        this.fitter.fit()

        let parsed_url = url.parse(window.location.origin)

        let protocol = parsed_url.protocol == "https:" ? "wss" : "ws"
        let host = parsed_url.host
        let pathname = "/terminal/server"

        let server_url = `${protocol}://${host}${pathname}`

        console.log("Configure session for terminal", this.id)

        this.socket = new WebSocket(server_url)

        this.configure_handlers()
        this.configure_sensors()

        // In case there is more than one terminal session displayed at one
        // time, if this is marked as the default terminal session ensure it
        // grabs focus.

        if ($(this.element).attr("data-default-terminal") == "true")
            this.focus()

        // Add a handler so know when get focus. This is used to track which
        // terminal session last had focus so that when switch tabs can restore
        // focus to the terminal session which previously had it.

        let self = this

        $(this.element).find("textarea.xterm-helper-textarea").first().focus(() => {
            $(self.element).closest("div.tab-pane").attr("data-active-terminal", self.id)
        })
    }

    private configure_handlers() {
        if (this.shutdown)
            return

        console.log("Configure handlers for terminal", this.id)

        $(this.element).removeClass("notify-closed notify-exited")
        $(this.element).removeClass("notify-hijacked notify-forbidden")

        $("#refresh-button").removeClass("terminal-" + this.id + "-refresh-required")

        this.socket.onerror = (event) => {
            console.error("WebSocket error observed:", event)
        }

        let socket: WebSocket = this.socket

        this.socket.onopen = async () => {
            // If the socket isn't the one currently associated with the
            // terminal then bail out straight away as some sort of mixup has
            // occurred. Close the socket for good measure.

            console.log("Connection opened to terminal", this.id)

            if (this.reconnectTimer) {
                console.log("Clear reconnection timeout for terminal", this.id)
                clearTimeout(this.reconnectTimer)
                this.reconnectTimer = null
            }

            if (socket !== this.socket) {
                console.warn("Multiple connections to terminal", this.id)
                socket.close()
                return
            }

            this.reconnecting = false
            this.reconnectStartTime = 0
            this.reconnectDelay = this.initialReconnectDelay

            this.started = new Date()

            // The sequence number indicates from where in the buffered data
            // kept by the server side, data should be returned on an initial
            // connection. This is to avoid replaying data we already have
            // previously received. Instead will only get what haven't seen
            // yet. If this is a completely new session, the sequence number
            // will start out as -1 so we will be sent everything.

            let args: TerminalsHelloPacketArgs = {
                token: this.endpoint,
                cols: this.terminal.cols,
                rows: this.terminal.rows,
                seq: this.sequence
            }

            this.send_message(TerminalsPacketType.HELLO, args)

            // A sequence number of -1 means this is a completely new session.
            // In this case we need to setup the callback for receiving input
            // data from the terminal and initiate the pings. We can only do
            // this once else we get duplicate registrations if we have to
            // reconnect because the connection is dropped.

            if (this.sequence == -1) {
                console.log("Connecting terminal", this.id)

                this.terminal.onData((data) => {
                    // Only send data through to the backend if the initial
                    // data synchronization has been completed.

                    if (this.synced) {
                        let args: OutboundDataPacketArgs = { data: data }
                        this.send_message(TerminalsPacketType.DATA, args)
                    }
                })

                this.initiate_pings()

                // Set sequence number to 0 so we don't do this all again.

                this.sequence = 0

                // Schedule unblocking of output to remote session and flush
                // out anything which was buffered while waiting. It is
                // blocked initially to allow any terminal to be visible
                // and the remote shell or application to start and display
                // any initial prompt. If a reconnect occurs while we were
                // waiting, discard the data.

                setTimeout(async () => {
                    if (socket !== this.socket) {
                        this.buffer = []
                        return
                    }

                    this.blocked = false

                    let buffer = this.buffer
                    this.buffer = []

                    if (buffer.length) {
                        console.log("Flushing data to terminal", this.id)

                        for (let block of buffer)
                            await this.paste(block.text, block.bracketed)
                    }
                }, 1000)

                // Generate analytics event to track terminal connect.

                send_analytics_event("Terminal/Connect", { terminal_id: this.id })
            }
            else {
                console.log("Re-connecting terminal", this.id)

                // Generate analytics event to track terminal reconnect.

                send_analytics_event("Terminal/Reconnect", { terminal_id: this.id })
            }
        }

        this.socket.onmessage = async (evt) => {
            // If the socket isn't the one currently associated with the
            // terminal then bail out straight away as some sort of mixup has
            // occurred. Close the socket for good measure.

            if (this.socket === null) {
                console.warn("Connection was abandoned to terminal", this.id)
                socket.close()
                return
            }

            if (socket !== this.socket) {
                console.warn("Multiple connections to terminal", this.id)
                socket.close()
                return
            }

            let packet: TerminalsPacket = JSON.parse(evt.data)

            if (packet.id == this.id) {
                switch (packet.type) {
                    case (TerminalsPacketType.DATA): {
                        let args: InboundDataPacketArgs = packet.args

                        await this.write(args.data)

                        // Update the sequence number to that received on the
                        // DATA message from the server side. This affects
                        // what sequence number of sent on a HELLO message
                        // if we have to reconnect because the connection is
                        // dropped. It ensures that on reconnection we only
                        // get data we haven't seen before.

                        this.sequence = args.seq

                        // If this was the initial synchronization packet when
                        // reconnecting due to a browser refresh then we need to
                        // enable sending of data through to backend. We don't
                        // initially pass data through when handling the initial
                        // data as that could result in spurious escape
                        // sequences being sent to the backend from replaying
                        // past data which would then sit there as pending input
                        // to the terminal when not the original application.

                        if (args.sync) {
                            console.log("Synchronized terminal", this.id)
                            this.synced = true
                        }

                        break
                    }
                    case (TerminalsPacketType.EXIT): {
                        console.log("Terminal has exited", this.id)

                        $(this.element).addClass("notify-exited")

                        $("#refresh-button").addClass("terminal-" + this.id + "-refresh-required")

                        this.scrollToBottom()
                        await this.write("\r\nExited\r\n")

                        this.socket.close()

                        this.shutdown = true
                        this.socket = null

                        // Generate analytics event to track terminal exit.

                        send_analytics_event("Terminal/Exited", { terminal_id: this.id })

                        break
                    }
                    case (TerminalsPacketType.ERROR): {
                        let args: ErrorPacketArgs = packet.args

                        // Right now we only expect to receive reasons of
                        // "Forbidden" and "Hijacked". This is used to set
                        // an element class so can provide visual indicator
                        // to a user. Otherwise nothing is done for an error.

                        $(this.element).addClass(`notify-${args.reason.toLowerCase()}`)

                        break
                    }
                }
            }
            else {
                console.warn("Client session " + this.id + " received message for session " + packet.id)
            }
        }

        this.socket.onclose = (_evt: any) => {
            // If the socket isn't the one currently associated with the
            // terminal then bail out straight away as some sort of mixup has
            // occurred.

            if (socket !== this.socket)
                return

            let self = this

            // If the socket connection to the backend terminal server is
            // closed, it doesn't mean that the backend terminal session
            // has exited. The socket connection could have been lost due to
            // restart of a router between the frontend client and the
            // backend. This is in particular a problem when nginx is used
            // for ingress in a Kubernetes cluster. Every time a new ingress
            // is created, existing connections will at some point be dropped.
            // Usually this is about 4 minutes after a new ingress is added.
            // What we therefore do is attempt to reconnect, but where we
            // give up after 1 second. This is usually enough to transparently
            // recover from connections being dropped due to a router restart.
            // If reconnection fails, it truly means the backend session
            // had been terminated, or there was still an issue with a router.
            // In the latter case, a manual reconnection, or page refresh,
            // would need to be triggered.

            this.socket = null

            if (this.shutdown)
                return

            function connect() {
                if (self.shutdown)
                    return

                let parsed_url = url.parse(window.location.origin)

                let protocol = parsed_url.protocol == "https:" ? "wss" : "ws"
                let host = parsed_url.host
                let pathname = "/terminal/server"

                let server_url = `${protocol}://${host}${pathname}`

                console.log("Attempt re-connect to terminal", self.id, "delay was", self.reconnectDelay, "ms")

                self.socket = new WebSocket(server_url)

                self.configure_handlers()
            }

            console.log("Terminal connection was lost", self.id)

            if (self.started !== null) {
                let finished = new Date()
                let duration = (finished.getTime() - self.started.getTime()) / 1000;

                console.log("Terminal session duration", duration)

                self.started = null
            }

            // Track when reconnection started
            if (!this.reconnecting) {
                this.reconnectStartTime = Date.now()
                this.reconnectDelay = this.initialReconnectDelay
            }

            // Calculate the appropriate delay based on how long we've been trying
            let elapsed = Date.now() - this.reconnectStartTime

            if (elapsed > this.fastReconnectPeriod) {
                // After the fast reconnect period, increase the delay with backoff
                this.reconnectDelay = Math.min(
                    this.reconnectDelay * this.backoffMultiplier,
                    this.maxReconnectDelay
                )
            }

            setTimeout(connect, this.reconnectDelay)

            async function terminate() {
                self.reconnectTimer = null

                if (!self.reconnecting)
                    return

                console.log("Abandoning connection to terminal", self.id)

                self.reconnecting = false
                self.shutdown = true

                self.socket = null

                $(self.element).addClass("notify-closed")

                $("#refresh-button").addClass("terminal-" + self.id + "-refresh-required")

                self.scrollToBottom()
                await self.write("\r\nClosed\r\n")

                // Generate analytics event to track terminal close.

                send_analytics_event("Terminal/Closed", { terminal_id: this.id })
            }

            if (!this.reconnecting) {
                console.log("Trigger reconnection timeout for terminal", self.id)

                self.reconnectTimer = setTimeout(terminate, self.abandonTimeout)

                this.reconnecting = true
            }
        }
    }

    private configure_sensors() {
        // This monitors the element the terminal is embedded in for
        // changes in size and triggers a recalculation of the terminal
        // size. In order to avoid problems with multiple resize events
        // occuring, the callbacks are throttled so at most one is
        // allowed in the specified time period.

        this.sensor = new ResizeSensor(this.element, _.throttle(() => {
            this.resize_terminal()
        }, 500))
    }

    private initiate_pings() {
        let self = this

        // Ping messages are only sent from client to backend server. Some
        // traffic is required when the session is otherwise idle, else you
        // can't tell if the connection has been dropped.

        function ping() {
            self.send_message(TerminalsPacketType.PING)
            setTimeout(ping, 15000)
        }

        setTimeout(ping, 15000)
    }

    private resize_terminal() {
        // As long as the dimensions are valid, the terminal is re-fit to
        // the element size and a RESIZE message is sent to the backend
        // server. The dimensions on the server side are using to notify
        // the psudeo tty for the terminal process of the change in window
        // dimenions allowing any application to adjust to the new size
        // and refresh the terminal screen.  

        if (this.element.clientWidth > 0 && this.element.clientHeight > 0) {
            this.fitter.fit()

            let args: ResizePacketArgs = {
                cols: this.terminal.cols,
                rows: this.terminal.rows
            }

            this.send_message(TerminalsPacketType.RESIZE, args)
        }
    }

    private send_message(type: TerminalsPacketType, args?: any): boolean {
        if (!this.socket)
            return false

        if (this.socket.readyState === WebSocket.OPEN) {
            let packet: TerminalsPacket = {
                type: type,
                id: this.id
            }

            if (args !== undefined)
                packet["args"] = args

            this.socket.send(JSON.stringify(packet))

            return true
        }

        return false
    }

    async write(text: string) {
        let self = this

        function writeSync(text: string | Uint8Array): Promise<void> {
            return new Promise<void>(resolve => self.terminal.write(text, resolve));
        }

        await writeSync(text)
    }

    focus() {
        this.terminal.focus()
    }

    blur() {
        this.terminal.blur()
    }

    clear() {
        this.terminal.clear()
    }

    scrollToBottom() {
        this.terminal.scrollToBottom()
    }

    async paste(text: string, bracketed: boolean = true) {
        if (!this.blocked) {
            if (this.terminal.modes.bracketedPasteMode && !bracketed) {
                await this.write("\x1b[?2004l").then(_ => {
                    this.terminal.paste(text)
                }).then(async _ => {
                    await this.write("\x1b[?2004h")
                })
            }
            else {
                await this.terminal.paste(text)
            }
        } else
            this.buffer.push({ text: text, bracketed: bracketed })
    }

    close() {
        if (this.socket)
            this.socket.close()
    }

    reconnect() {
        if (!this.shutdown)
            return

        // Where the socket connection had previously been lost and an
        // automatic reconnect didn't work, this allows a new attempt to
        // reconnect to be made.

        this.shutdown = false
        this.sequence = 0
        this.reconnectStartTime = 0
        this.reconnectDelay = this.initialReconnectDelay

        let self = this

        console.log("Request to force reconnect terminal", self.id)

        function connect() {
            if (self.shutdown)
                return

            let parsed_url = url.parse(window.location.origin)

            let protocol = parsed_url.protocol == "https:" ? "wss" : "ws"
            let host = parsed_url.host
            let pathname = "/terminal/server"

            let server_url = `${protocol}://${host}${pathname}`

            console.log("Attempt force re-connect to terminal", self.id, "delay was", self.reconnectDelay, "ms")

            self.socket = new WebSocket(server_url)

            self.configure_handlers()
        }

        setTimeout(connect, this.reconnectDelay)

        function terminate() {
            self.reconnectTimer = null

            if (!self.reconnecting)
                return

            console.log("Abandoning connection to terminal", self.id)

            self.reconnecting = false
            self.shutdown = true

            self.socket = null
        }

        if (!this.reconnecting) {
            console.log("Trigger reconnection timeout for terminal", self.id)

            self.reconnectTimer = setTimeout(terminate, self.abandonTimeout)

            this.reconnecting = true
        }
    }

    set_theme(value: any) {
        this.terminal.options.theme = value
    }
}

class Terminals {
    sessions: { [id: string]: TerminalSession } = {}

    constructor() {
        // Search for all elements with class "terminal". For these we insert
        // a terminal directly into the page connected using a web socket.
        // Since we are using a class, there can be multiple instances. The
        // id of the terminal session being connected to is taken from the
        // "session-id" data attribute. The "endpoint-id" is a unique value
        // identifying the particular terminal server backend. It acts as a
        // crude method of ensuring that a frontend client is talking to the
        // correct backend since they must match.

        $(".terminal").each((index: number, element: HTMLElement) => {
            this.initialize_terminal(element)
        })
    }

    initialize_terminal(element: HTMLElement) {
        let $body = $("body")
        let context = $body.data("user-context")
        let id: string = $(element).data("session-id")
        let endpoint: string = $(element).data("endpoint-id")

        console.log("Initializing terminal", id)

        this.sessions[id] = new TerminalSession(context, id, element, endpoint)

        // Append a div to element with translucent text overlaid on
        // the terminal. Only applies to terminals sessions 1-3.

        let overlay = $(`<div class="terminal-overlay
                terminal-overlay-${id}"></div>`)[0]

        element.append(overlay)
    }

    // The following are the only APIs which separate frontend application
    // code should use to interact with terminals.

    async paste_to_terminal(text: string, id: string = "1") {
        let terminal = this.sessions[id]

        if (terminal) {
            await terminal.paste(text)

            if (!is_touch_device() && !is_ios()) {
                terminal.focus()
            }
            else {
                terminal.blur()
            }
        }
    }

    async paste_to_all_terminals(text: string) {
        await this.paste_to_terminal(text, "1")
        await this.paste_to_terminal(text, "2")
        await this.paste_to_terminal(text, "3")

        this.select_terminal("1")
    }

    async interrupt_terminal(id: string = "1") {
        let terminal = this.sessions[id]

        if (terminal) {
            terminal.scrollToBottom()
            await terminal.paste(String.fromCharCode(0x03))

            if (!is_touch_device() && !is_ios()) {
                terminal.focus()
            }
            else {
                terminal.blur()
            }
        }
    }

    async interrupt_all_terminals() {
        await this.interrupt_terminal("1")
        await this.interrupt_terminal("2")
        await this.interrupt_terminal("3")

        this.select_terminal("1")
    }

    select_terminal(id: string = "1"): boolean {
        let terminal = this.sessions[id]

        if (terminal) {
            terminal.focus()

            return true
        }

        return false
    }

    clear_terminal(id: string = "1") {
        let terminal = this.sessions[id]

        if (terminal) {
            terminal.scrollToBottom()
            terminal.clear()
        }
    }

    clear_all_terminals() {
        this.clear_terminal("1")
        this.clear_terminal("2")
        this.clear_terminal("3")

        this.select_terminal("1")
    }

    async execute_in_terminal(command: string, id: string = "1", clear: boolean = false) {
        if (command == "<ctrl-c>" || command == "<ctrl+c>")
            return this.interrupt_terminal(id)

        let terminal = this.sessions[id]

        if (terminal) {
            terminal.scrollToBottom()

            if (clear)
                terminal.clear()

            await terminal.paste(command + "\r", false)

            if (!is_touch_device() && !is_ios()) {
                terminal.focus()
            }
            else {
                terminal.blur()
            }
        }
    }

    async execute_in_all_terminals(command: string, clear: boolean = false) {
        if (command == "<ctrl-c>" || command == "<ctrl+c>")
            return await this.interrupt_all_terminals()

        await this.execute_in_terminal(command, "1")
        await this.execute_in_terminal(command, "2")
        await this.execute_in_terminal(command, "3")

        this.select_terminal("1")
    }

    disconnect_terminal(id: string = "1") {
        let terminal = this.sessions[id]

        if (terminal)
            terminal.close()
    }

    disconnect_all_terminals() {
        for (let id in this.sessions)
            this.sessions[id].close()
    }

    reconnect_terminal(id: string = "1") {
        let terminal = this.sessions[id]

        if (terminal)
            terminal.reconnect()
    }

    reconnect_all_terminals() {
        for (let id in this.sessions)
            this.sessions[id].reconnect()
    }

    set_theme_all_terminals(value: any) {
        for (let id in this.sessions) {
            this.sessions[id].set_theme(value)
        }
    }
}

class Dashboard {
    private dashboard: any
    private expiration: number
    private extendable: boolean
    private expiring: boolean
    private messages: MessagesChannel

    constructor() {
        let $body = $("body")

        console.log("Initializing message channel")

        this.messages = new MessagesChannel("0")

        if ($("#dashboard").length) {
            // To indicate progress, update message on startup cover panel. Also
            // hide the cover panel after 15 seconds if we don't get through all
            // the dashboard configuration for some reason or if the user
            // changes browser tab when it is starting, which can sometimes
            // result in the notification that page has loaded being dropped by
            // the browser.

            setTimeout(() => {
                $("#startup-cover-panel-message>h5").text("Configuring session...")
            }, 1000)

            setTimeout(() => {
                $("#startup-cover-panel").hide()
            }, 15000)

            // The web interface can either have a workshop panel on the left
            // and workarea panel on the right, or it can have just a workarea
            // panel. If there is both, we need to split the two

            console.log("Adding split for workshop/workarea")

            if ($("#workshop-panel").length) {
                this.dashboard = Split(["#workshop-panel", "#workarea-panel"], {
                    gutterSize: 8,
                    sizes: [35, 65],
                    cursor: "col-resize",
                    direction: "horizontal",
                    snapOffset: 120,
                    minSize: 0
                })
            }

            // Add action whereby if double click on vertical divider, will
            // either collapse or expand the workshop panel.

            $("#workshop-panel").next("div.gutter-horizontal").on("dblclick", () => {
                if (this.dashboard.getSizes()[0] < 5.0)
                    this.dashboard.setSizes([35, 65])
                else
                    this.dashboard.collapse(0)
            })

            // If the terminal layout is "lower", then the terminal is being
            // deployed the main workarea, so need to split the workarea.

            console.log("Adding split for terminal below workarea")

            if ($("#workarea-1").length && $("#workarea-2").length) {
                Split(["#workarea-1", "#workarea-2"], {
                    gutterSize: 8,
                    sizes: [70, 30],
                    cursor: "row-resize",
                    direction: "vertical"
                })
            }
        }

        if ($("#terminal-1").length)
            console.log("One or more terminals enabled")

        if ($("#terminal-3").length) {
            console.log("Adding split for three terminals")

            Split(["#terminal-1", "#terminal-2", "#terminal-3"], {
                gutterSize: 8,
                sizes: [50, 25, 25],
                cursor: "row-resize",
                direction: "vertical"
            })
        }
        else if ($("#terminal-2").length) {
            console.log("Adding split for two terminals")

            Split(["#terminal-1", "#terminal-2"], {
                gutterSize: 8,
                sizes: [60, 40],
                cursor: "row-resize",
                direction: "vertical"
            })
        }

        // Reorder the dashboard tabs and ensure the first tab is the one that
        // is displayed and has focus.

        if ($body.data("dashboard-tabs")) {
            let tabs = $body.data("dashboard-tabs").split(",")

            let tab_ids = []

            for (let id in tabs) {
                tab_ids.push(`${tabs[id]}-tab`)
            }

            let navbar = $("#workarea-nav")

            let matched_tabs = {}

            let remaining_tabs = []

            $('#workarea-nav > li').each((_, element) => {
                let id = $(element).children().first().attr("id")
                if (tab_ids.includes(id)) {
                    matched_tabs[id] = element
                } else {
                    remaining_tabs.push(element)
                }
            })

            navbar.empty()

            tab_ids.forEach(name => {
                if (name in matched_tabs) {
                    navbar.append(matched_tabs[name])
                }
            })

            remaining_tabs.forEach(element => navbar.append(element))

            let tab_anchor = $(`#${tabs[0]}`)

            if (tab_anchor.length != 0) {
                let tab_object = new bootstrap.Tab(tab_anchor.get(0))
                tab_object.show()

                // Must also trigger click handlers.

                tab_anchor.get(0).click()
            }
        }

        // Add a click action to any panel which will set focus for active
        // terminal session when panel exposed. We need to do this with a
        // slight delay to ensure the terminal is visible first.

        $("div.tab-pane").each(function () {
            let div = $(this)
            let trigger = div.attr("aria-labelledby")
            if (trigger) {
                $("#" + trigger).on("click", () => {
                    if (div.attr("data-active-terminal")) {
                        setTimeout(() => {
                            terminals.select_terminal(div.attr("data-active-terminal"))
                        }, 250)
                    }
                })
            }
        })

        // Add a click action to any panel with a child iframe set up for
        // delayed loading when first click performed.

        $("iframe[data-src]").each(function () {
            let iframe = $(this)
            let trigger = iframe.parent().attr("aria-labelledby")
            if (trigger) {
                $("#" + trigger).on("click", () => {
                    if (iframe.data("src")) {
                        iframe.prop("src", iframe.data("src"))
                        iframe.data("src", "")
                    }
                })
            }
        })

        // Add a click action to confirmation button of exit/finish workshop
        // dialog in order to generate analytics event and redirect browser
        // back to portal for possible deletion of the workshop session.

        $("#terminate-session-dialog-confirm").on("click", async (event) => {
            console.log("Workshop is being terminated")

            $("#startup-cover-panel-message>h5").text("Cleaning up session...")
            $("#startup-cover-panel").show()

            $("#terminate-session-dialog").hide()

            await send_analytics_event("Workshop/Terminate", {}, 2500)

            setTimeout(() => {
                window.top.location.href = $(event.target).data("restart-url")
            }, 1000)
        })

        $("#finished-workshop-dialog-confirm").on("click", async (event) => {
            console.log("Workshop has been finished")

            $("#startup-cover-panel-message>h5").text("Cleaning up session...")
            $("#startup-cover-panel").show()

            $("#finished-workshop-dialog").hide()

            await send_analytics_event("Workshop/Finish", {}, 2500)

            setTimeout(() => {
                window.top.location.href = $(event.target).data("restart-url")
            }, 1000)
        })

        // Add a click action to confirmation button of expired workshop
        // dialog to redirect browser back to portal.

        $("#workshop-expired-dialog-confirm").on("click", (event) => {
            console.log("Workshop has expired")

            window.top.location.href = $(event.target).data("restart-url")
        })

        // Add a click action for the expand button to enable making full
        // screen or exiting full screen.

        if (document.fullscreenEnabled) {
            $("#fullscreen-button").on("click", (event) => {
                let $glyph = $("#fullscreen-button>span")
                if ($glyph.hasClass("fa-expand-arrows-alt")) {
                    if (document.documentElement.requestFullscreen) {
                        document.documentElement.requestFullscreen()
                        if (event.shiftKey) {
                            terminals.set_theme_all_terminals({
                                background: '#fafafa',
                                foreground: '#000000',
                                cursor: '#000000'
                            })
                        }
                    }
                }
                else if ($glyph.hasClass("fa-compress-arrows-alt")) {
                    if (document.exitFullscreen) {
                        document.exitFullscreen()
                    }
                }
            })

            $(document).on("fullscreenchange", () => {
                let $glyph = $("#fullscreen-button>span")
                if (document.fullscreenElement) {
                    $glyph.addClass("fa-compress-arrows-alt")
                    $glyph.removeClass("fa-expand-arrows-alt")
                }
                else {
                    $glyph.addClass("fa-expand-arrows-alt")
                    $glyph.removeClass("fa-compress-arrows-alt")
                    terminals.set_theme_all_terminals({
                        background: '#000000',
                        foreground: '#ffffff',
                        cursor: '#ffffff'
                    })
                }
            })
        }
        else {
            $("#fullscreen-button").hide()
        }

        // Add a click action for the refresh button to enable reloading
        // of workshop content, terminals or exposed tab.

        $("#refresh-button").on("click", (event) => {
            if (event.shiftKey) {
                if (this.dashboard) {
                    // If shift is pressed we target the workshop panel,
                    // either reloading it, or expanding the panel if it
                    // was previously collapsed.

                    if (this.dashboard.getSizes()[0] >= 5.0) {
                        let iframe = $("#workshop-iframe")
                        let iframe_window: any = iframe.contents().get(0)
                        iframe_window.location.reload()
                    }
                    else {
                        this.dashboard.setSizes([35, 65])
                    }
                }
            }
            else {
                // If terminal layout is "lower" and terminals are candidates
                // for reconnecting, then reconnect the terminals rather than
                // refresh the dashboard.

                let $body = $("body")
                let reconnect = false

                if ($(event.target).is("[class$='-refresh-required']"))
                    reconnect = true

                if ($body.data("terminal-layout") == "lower" && reconnect) {
                    terminals.reconnect_all_terminals()
                }
                else {
                    let active = $("#workarea-navbar-div li a.active")

                    if (active.length) {
                        if (active.attr("id") != "terminal-tab") {
                            let href = active.attr("href")
                            if (href) {
                                let terminal = $(href + " div.terminal")
                                if (terminal) {
                                    terminals.reconnect_terminal(terminal.data("session-id"))
                                }
                                else {
                                    let iframe = $(href + " iframe")
                                    if (iframe.length)
                                        iframe.attr("src", iframe.attr("src"))
                                }
                            }
                        }
                        else {
                            terminals.reconnect_all_terminals()
                        }
                    }
                }
            }
        })

        // Add a click action to menu items for opening target URL in a
        // separate window.

        $(".open-window").on("click", (event) => {
            window.open($(event.target).data("url"))
        })

        // Initiate countdown timer if enabled for workshop session. Also
        // add a click action to the button so the workshop session can be
        // extended.

        let self = this

        async function check_countdown() {
            function current_time() {
                return Math.floor(new Date().getTime() / 1000)
            }

            function time_remaining() {
                let now = current_time()
                return Math.max(0, self.expiration - now)
            }

            function format_time_digits(num: number, pad: boolean = true) {
                if (num < 10 && pad) {
                    let s = "0" + num
                    return s.substr(s.length - 2)
                }
                else {
                    return num.toString()
                }
            }

            function format_countdown(countdown: number) {
                let text = ' '

                let hours = Math.floor(countdown / (60 * 60))
                let minutes = Math.floor((countdown - (hours * 60 * 60)) / 60)
                let seconds = countdown % 60

                if (hours) {
                    text = text + format_time_digits(hours, false)
                    text = text + ':' + format_time_digits(minutes)
                }
                else {
                    text = text + format_time_digits(minutes)
                }

                text = text + ':' + format_time_digits(seconds)

                return text
            }

            let button = $("#countdown-button")
            let timer = $("#countdown-timer")
            let update = false

            if (self.expiration !== undefined) {
                let countdown = time_remaining()

                timer.html(format_countdown(countdown))
                button.removeClass('d-none')

                // let extendable = self.extendable

                // if (extendable === undefined)
                //     extendable = countdown <= 300

                if (self.expiring) {
                    if (self.extendable) {
                        button.addClass("btn-warning")
                        button.removeClass("btn-default")
                        button.removeClass("btn-transparent")
                        button.removeClass("btn-danger")
                    }
                    else {
                        button.addClass("btn-danger")
                        button.removeClass("btn-default")
                        button.removeClass("btn-transparent")
                        button.removeClass("btn-warning")
                    }
                }
                else {
                    button.addClass("btn-default")
                    button.addClass("btn-transparent")
                    button.removeClass("btn-warning")
                    button.removeClass("btn-danger")
                }

                if (countdown && !((countdown + 2) % 15))
                    update = true

                if (countdown <= 0) {
                    if (!$("#workshop-expired-dialog").data("expired")) {
                        $("#workshop-expired-dialog").data("expired", "true")

                        let modal

                        modal = new bootstrap.Modal(document.getElementById("workshop-failed-dialog"))
                        modal.hide()

                        modal = new bootstrap.Modal(document.getElementById("workshop-expired-dialog"))
                        modal.show()

                        await send_analytics_event("Workshop/Expired", {}, 2500)
                    }
                }
            }
            else {
                button.addClass('d-none')
                timer.html('')

                update = true
            }

            if (update) {
                $.ajax({
                    type: 'GET',
                    url: "/session/schedule",
                    cache: false,
                    success: (data, textStatus, xhr) => {
                        if (data.expires) {
                            let now = current_time()
                            let countdown = Math.max(0, Math.floor(data.countdown))
                            self.expiration = now + countdown
                            self.extendable = data.extendable
                            self.expiring = data.expiring
                        }

                        setTimeout(check_countdown, 500)
                    },
                    error: () => {
                        setTimeout(check_countdown, 500)
                    }
                })
            }
            else {
                setTimeout(check_countdown, 500)
            }
        }

        if ($("#countdown-button").length) {
            setTimeout(check_countdown, 500)

            $("#countdown-button").on("click", () => {
                $.ajax({
                    type: 'GET',
                    url: "/session/extend",
                    cache: false,
                    success: (data, textStatus, xhr) => {
                        if (data.expires) {
                            let now = Math.floor(new Date().getTime() / 1000)
                            let countdown = Math.max(0, Math.floor(data.countdown))
                            self.expiration = now + countdown
                            self.extendable = data.extendable
                            self.expiring = data.expiring
                            let button = $("#countdown-button")
                            button.addClass("btn-default")
                            button.addClass("btn-transparent")
                            button.removeClass("btn-danger")
                            button.removeClass("btn-warning")
                        }
                    },
                    error: () => { }
                })
            })
        }

        // Hide the startup cover panel across the dashboard once the page has
        // finished loading. The cover panel hides adjustments in dashboard as
        // it is being displayed.

        $(window).on('load', () => {
            setTimeout(() => {
                $("#startup-cover-panel").hide()
            }, 1000)

            let $body = $("body")

            if ($body.data("workshop-ready") == false) {
                let modal = new bootstrap.Modal(document.getElementById("workshop-failed-dialog"))
                modal.show()
            }
            else if ($body.data("page-hits") == "1") {
                if ($("#started-workshop-dialog").length) {
                    let modal = new bootstrap.Modal(document.getElementById("started-workshop-dialog"))
                    modal.show()
                }
            }
        })

        // Select whatever is the first tab of the navbar so it is displayed.

        console.log("Expose first dashboard tab")

        let first_element = $("#workarea-nav>li>a").get(0)

        if (first_element !== undefined) {
            let tab_object = new bootstrap.Tab(first_element)
            tab_object.show()

            // Must also trigger click handlers.

            first_element.click()
        }
    }

    session_owner(): string {
        return $("body").data("session-owner")
    }

    finished_workshop() {
        let modal = new bootstrap.Modal(document.getElementById("finished-workshop-dialog"))
        modal.show()
    }

    terminate_session() {
        let modal = new bootstrap.Modal(document.getElementById("terminate-session-dialog"))
        modal.show()
    }

    verify_origin(origin: string): boolean {
        if (origin == window.origin)
            return true

        let result = false

        let $body = $("body")
        let workshop_url = $body.data("workshop-url")

        if (workshop_url && workshop_url.match(/^https?:/)) {
            let url = new URL(workshop_url)

            if (origin == url.origin) {
                result = true
            }
        }

        $(".dashboard-iframe").each((index: number, element: HTMLElement) => {
            let src = element.dataset["src"]

            if (src) {
                let url = new URL(src)

                if (origin == url.origin) {
                    result = true
                    return false
                }
            }
        })

        return result
    }

    preview_image(src: string, title: string) {
        $("#preview-image-element").attr("src", src)
        $("#preview-image-title").text(title)
        let modal = new bootstrap.Modal(document.getElementById("preview-image-dialog"))
        modal.show()
    }

    reload_dashboard(name: string, url: string = "", focus: boolean = true): boolean {
        let id = string_to_slug(name)

        let tab_anchor = document.querySelector(`#${id}-tab`)

        if (tab_anchor === null)
            return this.create_dashboard(name, url, focus)

        if (focus) {
            let tab_object = new bootstrap.Tab(tab_anchor)
            tab_object.show()

            // Must also trigger click handlers.

            $(tab_anchor).click()
        }

        if (name != "terminal") {
            let tab = $(tab_anchor)
            let href = tab.attr("href")
            if (href) {
                let terminal = $(href + " div.terminal")
                if (terminal.length) {
                    terminals.reconnect_terminal(terminal.data("session-id"))
                }
                else {
                    let iframe = $(href + " iframe")
                    if (iframe.length)
                        iframe.attr("src", url || iframe.attr("src"))
                }
            }
        }
        else {
            terminals.reconnect_all_terminals()
        }

        return true
    }

    expose_terminal(session: string): boolean {
        session = String(session)

        let id = string_to_slug(session)

        let terminal = $(`#terminal-${id}`)

        let tab_anchor = $(`#${terminal.data("tab")}`)

        if (!tab_anchor.length)
            return false

        // Mark the terminal as the active terminal for the panel it is
        // contained within so that when the panel is exposed, the terminal
        // will be set to have focus.

        $(tab_anchor.attr("href")).attr("data-active-terminal", session)

        let tab_object = new bootstrap.Tab(tab_anchor.get(0))
        tab_object.show()

        // Must also trigger click handlers.

        tab_anchor.get(0).click()

        return true
    }

    expose_dashboard(name: string): boolean {
        let id = string_to_slug(name)

        let tab_anchor = $(`#${id}-tab`)

        if (!tab_anchor.length)
            return false

        let tab_object = new bootstrap.Tab(tab_anchor.get(0))
        tab_object.show()

        // Must also trigger click handlers.

        tab_anchor.get(0).click()

        return true
    }

    create_dashboard(name: string, url: string, focus: boolean = true): boolean {
        if (!name)
            return

        let id = string_to_slug(name)

        // Make sure dashboard with desired name doesn't already exist.

        if ($(`#${id}-tab`).length)
            return false

        // Work out if embedding a terminal or external site reference.

        let terminal: string = null

        if (url.startsWith("terminal:")) {
            terminal = url.replace("terminal:", "")
            url = null
        }

        if (!terminal)
            url = url || "about:blank"

        // Create new tab. The button needs to be insert before the
        // "#workarea-controls". The panel and iframe need to be added at
        // the end of "#workarea-panels".

        let tab_li = $(`<li class="nav-item"></li>`)

        let tab_anchor = $(`<a class="nav-link" id="${id}-tab"
            data-bs-toggle="tab" href="#${id}-panel" role="tab"
            aria-controls="${id}-panel" data-transient-tab="true"></a>`).text(name)

        tab_li.append(tab_anchor)

        let panel_div = $(`<div id="${id}-panel"
            class="tab-pane fade show panel-div iframe-div"
            role="tabpanel" aria-labelledby="${id}-tab"></div>`)

        if (terminal) {
            let $body = $("body")
            let endpoint_id = $body.data("endpoint-id")

            let terminal_div = $(`<div class="terminal"
                id="terminal-${terminal}"
                data-endpoint-id="${endpoint_id}"
                data-session-id="${terminal}"
                data-tab="${id}-tab"
                data-default-terminal="true"></div>`)

            panel_div.append(terminal_div)

            terminals.initialize_terminal(terminal_div[0])
        }
        else {
            let iframe_div = $(`<iframe class="dashboard-iframe" src="${url}" data-src="${url}"></iframe>`)

            panel_div.append(iframe_div)
        }

        $("#workarea-controls").before(tab_li)
        $("#workarea-panels").append(panel_div)

        // Add a click action which will set focus for active terminal session
        // when panel exposed. We need to do this with a slight delay to ensure
        // the terminal is visible first.

        if (terminal) {
            panel_div.attr("data-active-terminal", terminal)
            tab_anchor.on("click", () => {
                if (panel_div.attr("data-active-terminal")) {
                    setTimeout(() => {
                        terminals.select_terminal(panel_div.attr("data-active-terminal"))
                    }, 250)
                }
            })
        }

        // Now trigger click action on the tab to expose new dashboard tab.

        if (focus) {
            let tab_object = new bootstrap.Tab(tab_anchor.get(0))
            tab_object.show()

            // Must also trigger click handlers.

            tab_anchor.get(0).click()
        }

        return true
    }

    delete_dashboard(name: string): boolean {
        let id = string_to_slug(name)

        let tab_anchor = $(`#${id}-tab`)
        let panel_div = $(`#${id}-panel`)

        // Make sure dashboard with desired name exists and that it is a
        // transient tab that can be deleted.

        if (!tab_anchor.length || !tab_anchor.data("transient-tab"))
            return false

        // Remove tab and panel. It is actually parent of the tab that needs
        // to be removed

        tab_anchor.parent().remove()
        panel_div.remove()

        // If tab is active, revert back to the first dashboard tab.

        if (tab_anchor.hasClass("active")) {
            let first_element = $("#workarea-nav>li>a")[0]
            let tab_object = new bootstrap.Tab(first_element)
            tab_object.show()

            // Must also trigger click handlers.

            tab_anchor.get(0).click()
        }

        return true
    }

    reload_terminal() {
        this.reload_dashboard("terminal")
    }

    reload_workshop() {
        if (this.dashboard) {
            if (this.dashboard.getSizes()[0] >= 5.0) {
                let iframe = $("#workshop-iframe")
                let iframe_window: any = iframe.contents().get(0)
                iframe_window.location.reload()
            }
            else {
                this.dashboard.setSizes([35, 65])
            }
        }
    }

    collapse_workshop() {
        if (this.dashboard)
            this.dashboard.collapse(0)
    }
}

export let dashboard: Dashboard
export let terminals: Terminals

interface TerminalExecuteOptions {
    command: string;
    session: string
    clear: boolean
}

interface TerminalExecuteAllOptions {
    command: string
    clear: boolean
}

interface TerminalSelectOptions {
    session: string;
}

interface TerminalPasteOptions {
    text: string
    session: string
}

interface DashboardSelectOptions {
    name: string
}

interface DashboardCreateOptions {
    name: string
    url: string
    focus: boolean
}

interface DashboardPreviewOptions {
    src: string
    title: string
}

interface OpenUrlOptions {
    url: string
}

const action_table = {
    "terminal:execute": async function (args: TerminalExecuteOptions) {
        let id = args.session || "1"
        if (id == "*") {
            dashboard.expose_dashboard("terminal")
            terminals.execute_in_all_terminals(args.command, args.clear)
        }
        else {
            dashboard.expose_terminal(id)
            await terminals.execute_in_terminal(args.command, id, args.clear)
        }
    },
    "terminal:execute-all": async function (args: TerminalExecuteAllOptions) {
        dashboard.expose_dashboard("terminal")
        await terminals.execute_in_all_terminals(args.command, args.clear)
    },
    "terminal:clear": function (args: TerminalSelectOptions) {
        let id = args.session || "1"
        if (id == "*") {
            dashboard.expose_dashboard("terminal")
            terminals.clear_all_terminals()
        }
        else {
            dashboard.expose_terminal(id)
            terminals.clear_terminal(id)
        }
    },
    "terminal:clear-all": function () {
        dashboard.expose_dashboard("terminal")
        terminals.clear_all_terminals()
    },
    "terminal:interrupt": function (args: TerminalSelectOptions) {
        let id = args.session || "1"
        if (id == "*") {
            dashboard.expose_dashboard("terminal")
            terminals.interrupt_all_terminals()
        }
        else {
            dashboard.expose_terminal(id)
            terminals.interrupt_terminal(id)
        }
    },
    "terminal:interrupt-all": function () {
        dashboard.expose_dashboard("terminal")
        terminals.interrupt_all_terminals()
    },
    "terminal:input": function (args: TerminalPasteOptions) {
        dashboard.expose_terminal(args.session)
        terminals.paste_to_terminal(args.text, args.session)
    },
    "terminal:select": function (args: TerminalSelectOptions) {
        let id = args.session || "1"
        dashboard.expose_terminal(id)
        terminals.select_terminal(id)
    },
    "dashboard:open-dashboard": function (args: DashboardSelectOptions) {
        dashboard.expose_dashboard(args.name)
    },
    "dashboard:create-dashboard": function (args: DashboardCreateOptions) {
        dashboard.create_dashboard(args.name, args.url, args.focus)
    },
    "dashboard:delete-dashboard": function (args: DashboardSelectOptions) {
        dashboard.delete_dashboard(args.name)
    },
    "dashboard:reload-dashboard": function (args: DashboardCreateOptions) {
        dashboard.reload_dashboard(args.name, args.url, args.focus)
    },
    "dashboard:preview-image": function (args: DashboardPreviewOptions) {
        dashboard.preview_image(args.src, args.title)
    },
    "dashboard:finished-workshop": function () {
        dashboard.finished_workshop()
    },
    "dashboard:terminate-session": function () {
        dashboard.terminate_session()
    },
    "dashboard:open-url": function (args: OpenUrlOptions) {
        window.open(args.url, "_blank")
    },
}

window.addEventListener("message", function (event) {
    if (dashboard.verify_origin(event.origin)) {
        let handler = action_table[event.data.action]
        if (handler !== undefined)
            handler(event.data.data)
    }
})

function initialize_dashboard() {
    console.log("Initalizing dashboard")

    dashboard = new Dashboard()

    console.log("Initializing terminals")

    terminals = new Terminals()
}

$(document).ready(async () => {
    // Generate analytics events if a tracking ID is provided.

    let $body = $("body")

    if ($body.data("google-tracking-id")) {
        gtag("set", {
            "custom_map": {
                "dimension1": "workshop_name",
                "dimension2": "session_name",
                "dimension3": "environment_name",
                "dimension4": "training_portal",
                "dimension5": "ingress_domain",
                "dimension6": "ingress_protocol",
                "dimension7": "session_owner"
            }
        })

        let gsettings = {
            "workshop_name": $body.data("workshop-name"),
            "session_name": $body.data("session-namespace"),
            "environment_name": $body.data("workshop-namespace"),
            "training_portal": $body.data("training-portal"),
            "ingress_domain": $body.data("ingress-domain"),
            "ingress_protocol": $body.data("ingress-protocol"),
            "session_owner": $body.data("session-owner")
        }

        if ($body.data("ingress-protocol") == "https")
            gsettings["cookie_flags"] = "max-age=86400;secure;samesite=none"

        gtag("config", $body.data("google-tracking-id"), gsettings)
    }

    if ($body.data("clarity-tracking-id")) {
        clarity("set", "workshop_name", $body.data("workshop-name"))
        clarity("set", "session_name", $body.data("session-namespace"))
        clarity("set", "environment_name", $body.data("workshop-namespace"))
        clarity("set", "training_portal", $body.data("training-portal"))
        clarity("set", "ingress_domain", $body.data("ingress-domain"))
        clarity("set", "ingress_protocol", $body.data("ingress-protocol"))
        clarity("set", "session_owner", $body.data("session-owner"))
        clarity("identify", $body.data("session-owner"))
    }

    if ($body.data("amplitude-tracking-id")) {
        amplitude.init($body.data("amplitude-tracking-id"), undefined, { defaultTracking: { sessions: true, pageViews: true, formInteractions: true, fileDownloads: true } })
    }

    send_analytics_event("Workshop/Load")

    if ($body.data("page-hits") == "1")
        send_analytics_event("Workshop/Start")

    // In order to support use of "powerline" tool for fancy shell prompts
    // we need to use a custom font with modifications for special glyphs
    // used by powerline. Because fonts usually only load when a browser
    // first detects the font is required, this usually results in fonts
    // being wrongly displayed if first time a user is using this. To avoid
    // that we use a font loader to explicitly load the fonts first before
    // start initializing the terminals.

    let font_400 = new FontFaceObserver("SourceCodePro", { weight: 400 });
    let font_700 = new FontFaceObserver("SourceCodePro", { weight: 700 });

    let font_400_loader = font_400.load()
    let font_700_loader = font_700.load()

    font_400_loader.then(() => {
        font_700_loader.then(() => {
            console.log("Loaded fonts okay")
            initialize_dashboard()
        }), () => {
            console.log("Failed to load fonts")
            initialize_dashboard()
        }
    }), () => {
        font_700_loader.then(() => {
            console.log("Failed to load fonts")
            initialize_dashboard()
        }), () => {
            console.log("Failed to load fonts")
            initialize_dashboard()
        }
    }
})
