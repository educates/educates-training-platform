import * as path from "path"
import * as express from "express"
import axios from "axios"

import { createProxyMiddleware } from "http-proxy-middleware"

let axios_retry = require("axios-retry").default

import { config } from "./config"

export function setup_workshop(app: express.Application) {
    let workshop_url = config.workshop_url || '/workshop/'

    app.get('/workshop/.redirect-when-workshop-is-ready', function (req, res) {
        // If renderer is declared as being remote, in which case a workshop URL
        // should be provided which maps to a qualified http/https URL, assume
        // that is really externally hosted workshop content and perform an
        // immediate redirect to that site.

        if (config.workshop_renderer == "remote")
            return res.redirect(workshop_url)

        // In the case of having static workshop content, then also redirect.
        // In this case it should normally redirect to /workshop/ although
        // technically could redirect to a different sub URL path.

        if (config.workshop_renderer == "static")
            return res.redirect(workshop_url)

        // Finally, if workshop content renderer isn't enabled then redirect as
        // well. This should probably end up with an error when redirected.

        if (!config.enable_workshop)
            return res.redirect(workshop_url)

        // The local renderer process only runs for the classic renderer type.
        // For any other local renderer the content is rendered up front and
        // served statically below, so there is no renderer to wait on and the
        // check would always fail. Mirrors the condition used when setting up
        // serving further down.

        if (config.workshop_renderer == "local" && config.local_renderer_type != "classic")
            return res.redirect(workshop_url)

        // Check whether the internal workshop content renderer is ready.

        let url = 'http://127.0.0.1:' + config.workshop_port

        if (config.workshop_renderer == "proxy")
            url = workshop_url

        var client = axios.create({ baseURL: url })

        var options = {
            retries: 3,
            retryDelay: (retryCount) => {
                return retryCount * 500
            }
        };

        axios_retry(client, options)

        let redirect_url = workshop_url

        if (config.workshop_renderer == "proxy")
            redirect_url = '/workshop/content/'

        client.get('/workshop/content/')
            .then((result) => {
                res.redirect(redirect_url)
            })
            .catch((error) => {
                console.log('Error with workshop backend', error)
                res.redirect(redirect_url)
            })
    })

    app.get("/workshop$", (req, res) => {
        res.redirect("/workshop/")
    })

    if (config.workshop_renderer == "remote") {
        app.get("/workshop/$", (req, res) => {
            res.redirect(workshop_url)
        })
    }
    else if (config.workshop_renderer == "local" && config.local_renderer_type == "classic") {
        app.use(createProxyMiddleware("/workshop/", {
            target: 'http://127.0.0.1:' + config.workshop_port,
            ws: true,
            onError: (err, req, res) => {
                // The error handler can be called for either HTTP requests
                // or a web socket connection. Check whether have writeHead
                // method, indicating it is a HTTP request. Otherwise it is
                // actually a socket object and shouldn't do anything.

                console.log("Proxy", err)

                if (res.writeHead)
                    res.status(503).render("proxy-error-page")
            }
        }))
    }
    else if (config.workshop_renderer == "proxy") {
        app.get("/workshop/$", (req, res) => {
            res.redirect('/workshop/content/')
        })

        let protocol = config.workshop_proxy["protocol"]
        let host = config.workshop_proxy["host"]
        let port = config.workshop_proxy["port"]
        let headers = config.workshop_proxy["headers"]
        let change_origin = config.workshop_proxy["changeOrigin"]
        let path_rewrite = config.workshop_proxy["pathRewrite"]

        let path_rewrite_map = {}

        for (let item of path_rewrite) {
            path_rewrite_map[item["pattern"]] = item["replacement"]
        }

        let target = `${protocol}://${host}:${port}`

        app.use(createProxyMiddleware("/workshop/content/", {
            target: target,
            changeOrigin: change_origin,
            pathRewrite: path_rewrite_map,
            ws: true,
            onProxyReq: (proxyReq, req, res) => {
                for (let i = 0; i < headers.length; i++) {
                    let header = headers[i]
                    let name = header["name"]
                    let value = header["value"] || ""
                    proxyReq.setHeader(name, value)
                }
            },
            onProxyReqWs: (proxyReq, req, socket, options, head) => {
                for (let i = 0; i < headers.length; i++) {
                    let header = headers[i]
                    let name = header["name"]
                    let value = header["value"] || ""
                    proxyReq.setHeader(name, value)
                }
            },
            onProxyRes: (proxyRes, req, res) => {
                delete proxyRes.headers["x-frame-options"]
                delete proxyRes.headers["content-security-policy"]
                res.append("Access-Control-Allow-Origin", ["*"])
                res.append("Access-Control-Allow-Methods", "GET,PUT,POST,DELETE,HEAD")
                res.append("Access-Control-Allow-Headers", "Content-Type")
            },
            onError: (err, req, res) => {
                // The error handler can be called for either HTTP requests
                // or a web socket connection. Check whether have writeHead
                // method, indicating it is a HTTP request. Otherwise it is
                // actually a socket object and shouldn't do anything.

                console.log("Proxy", err)

                if (res.writeHead)
                    res.status(503).render("proxy-error-page")
            }
        }))
    }
    else {
        // In the case of static workshop content the requirement is that it
        // be at a base URL with sub path of /workshop/content/ so redirect
        // to that. This is so there is no conflict with /workshop/static.

        app.get("/workshop/$", (req, res) => {
            res.redirect('/workshop/content/')
        })

        app.use("/workshop/content/", express.static(path.join(config.workshop_dir, "public")))
    }
}

// We expose select URLs for accessing workshop configuration.

export function setup_workshop_config(app: express.Application, token: string = null) {
    function handler(filename) {
        return express.static(filename)
    }

    if (token) {
        function auth_handler(filename) {
            return async function (req, res, next) {
                let request_token = req.query.token

                if (!request_token || request_token != token)
                    return next()

                return await handler(filename)(req, res, next)
            }
        }

        app.use("/config/environment", auth_handler("/home/eduk8s/.local/share/workshop/workshop-environment.json"))
        app.use("/config/variables", auth_handler("/home/eduk8s/.local/share/workshop/workshop-variables.json"))
        app.use("/config/kubeconfig", auth_handler("/home/eduk8s/.kube/config"))
        app.use("/config/id_rsa", auth_handler("/home/eduk8s/.ssh/id_rsa"))
        app.use("/config/id_rsa.pub", auth_handler("/home/eduk8s/.ssh/id_rsa.pub"))
    }
    else {
        app.use("/config/environment", handler("/home/eduk8s/.local/share/workshop/workshop-environment.json"))
        app.use("/config/variables", handler("/home/eduk8s/.local/share/workshop/workshop-variables.json"))
        app.use("/config/kubeconfig", handler("/home/eduk8s/.kube/config"))
        app.use("/config/id_rsa", handler("/home/eduk8s/.ssh/id_rsa"))
        app.use("/config/id_rsa.pub", handler("/home/eduk8s/.ssh/id_rsa.pub"))
    }
}
