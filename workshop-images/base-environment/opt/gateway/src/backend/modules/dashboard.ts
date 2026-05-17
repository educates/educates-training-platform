import * as express from "express"
import * as fs from "fs"
import * as path from "path"

import { send_analytics_event } from "./session"

const home_directory = require('os').homedir()

import { config } from "./config"

function load_head_html() {
    let html_pathname = "/opt/eduk8s/theme/workshop-dashboard.html"

    if (!fs.existsSync(html_pathname))
        return ""

    return fs.readFileSync(html_pathname, "utf8")
}

function load_started_html() {
    let html_pathname = "/opt/eduk8s/theme/workshop-started.html"

    if (!fs.existsSync(html_pathname))
        return ""

    return fs.readFileSync(html_pathname, "utf8")
}

function load_finished_html() {
    let html_pathname = "/opt/eduk8s/theme/workshop-finished.html"

    if (!fs.existsSync(html_pathname))
        return ""

    return fs.readFileSync(html_pathname, "utf8")
}

const favicon_url = fs.existsSync("/opt/eduk8s/theme/favicon.ico")
    ? "/static/theme/favicon.ico"
    : "/static/images/favicon.ico"

export function setup_dashboard(app: express.Application, oauth2_client: any) {
    if (!config.enable_dashboard)
        return

    app.get("/dashboard/", async (req, res) => {
        if (!req.session.page_hits)
            req.session.page_hits = 1
        else
            req.session.page_hits++

        let locals = { "workshop_ready": true, "workshop_status": "", "time_started": req.session.started, "page_hits": req.session.page_hits }

        let setup_scripts_failed_file = path.join(home_directory, ".local", "share", "workshop", "setup-scripts.failed")

        if (fs.existsSync(setup_scripts_failed_file)) {
            locals["workshop_ready"] = false
            locals["workshop_status"] = "setup-scripts-failed"

            let data = { error: "setup-scripts-failed" }

            try {
                await send_analytics_event(req.session, oauth2_client, "Workshop/Error", { data: data })
            } catch (err) {
                // Ignore any error as we don't want it prevent page loading.
            }
        }

        let download_workshop_failed_file = path.join(home_directory, ".local", "share", "workshop", "download-workshop.failed")

        if (fs.existsSync(download_workshop_failed_file)) {
            locals["workshop_ready"] = false
            locals["workshop_status"] = "download-workshop-failed"

            let data = { error: "download-workshop-failed" }

            try {
                await send_analytics_event(req.session, oauth2_client, "Workshop/Error", { data: data })
            } catch (err) {
                // Ignore any error as we don't want it prevent page loading.
            }
        }

        locals["session_owner"] = ""
        locals["user_context"] = ""

        if (req.session.identity) {
            if (req.session.identity.owner)
                locals["session_owner"] = req.session.identity.owner

            if (req.session.identity.staff)
                locals["user_context"] = req.session.identity.user
        }

        locals["workshop_head_html"] = load_head_html()
        locals["workshop_started_html"] = load_started_html()
        locals["workshop_finished_html"] = load_finished_html()
        locals["favicon_url"] = favicon_url

        res.render("dashboard-page", locals)
    })
}
