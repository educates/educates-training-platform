import * as express from "express"
import * as path from "path"
import * as fs from "fs"

import { logger } from "./logger"
import { config } from "./config"

import * as content from "./content"

const BASEDIR = path.dirname(path.dirname(path.dirname(__dirname)))

// Calculate the list of modules with navigation path and a page index to make
// it easier to look up attributes of a module.

let modules = content.modules()
let module_index = content.module_index(modules)

logger.info("Modules", { modules: modules })
logger.info("Variables", { variables: config.variables })

// Setup all the sub routes.

export let router = express.Router()

// Redirect to the first page in navigation path if root.

router.get("/workshop/content/", (req, res) => {
    if (modules.length == 0)
        return res.send("No workshop content available.")

    res.redirect(path.join("/workshop/content", modules[0].path))
})

// If request matches a static file, serve up the contents immediately. We
// look in content directory as well as the directory for static assets.
// Looking in the content directory means that images for content can be
// colocated.

router.use("/workshop/content", express.static(config.content_dir))

router.use("/workshop/static/images", express.static(path.join(BASEDIR, "src/frontend/images")))
router.use("/workshop/static/styles", express.static(path.join(BASEDIR, "src/frontend/styles")))
router.use("/workshop/static/scripts", express.static(path.join(BASEDIR, "build/frontend/scripts")))

if (fs.existsSync("/opt/eduk8s/theme") && fs.lstatSync("/opt/eduk8s/theme").isDirectory()) {
    router.use("/workshop/static/theme", express.static("/opt/eduk8s/theme"))
}

// Also look for static files from packages installed by npm.

router.use("/workshop/static/bootstrap/css", express.static(path.join(BASEDIR, "node_modules/bootstrap/dist/css")))
router.use("/workshop/static/fontawesome", express.static(path.join(BASEDIR, "node_modules/@fortawesome/fontawesome-free")))
router.use("/workshop/static/asciidoctor/css", express.static(path.join(BASEDIR, "node_modules/@asciidoctor/core/dist/css")))
router.use("/workshop/static/highlight.js/styles", express.static(path.join(BASEDIR, "node_modules/highlight.js/styles")))

// Handle requests, allowing mapping to Markdown/AsciiDoc.

function load_head_html() {
    let html_pathname = "/opt/eduk8s/theme/workshop-instructions.html"

    if (!fs.existsSync(html_pathname))
        return ""

    return fs.readFileSync(html_pathname, "utf8")
}

const favicon_url = fs.existsSync("/opt/eduk8s/theme/favicon.ico")
    ? "/workshop/static/theme/favicon.ico"
    : "/workshop/static/images/favicon.ico"

router.get("/workshop/content/:pathname(*)", async function (req, res, next) {
    // Only allow a .html extension if an extension is supplied with the
    // request path. This is for compatability with previous rendering system.

    let pathname = req.params.pathname

    let extension = pathname.match(/\.[0-9a-z]+$/)

    if (extension) {
        if (extension[0] == ".html")
            pathname = pathname.slice(0, -5)
        else
            return next()
    }

    // Render content and generate page from template.

    let module = module_index[pathname]
    if (module) {
        let title = module.title
        let variables = config.variables.slice(0)

        try {
            let body = await content.render(module, variables)

            if (body !== undefined) {
                let options = {
                    config: config,
                    title: title,
                    content: body,
                    module: module,
                    modules: modules,
                    workshop_head_html: load_head_html(),
                    favicon_url: favicon_url,
                }

                return res.render("content-page", options)
            }
        } catch (error) {
            next(error)
        }
    }

    // Fall through to next handler if no match. This should result in a 404
    // Not Found being returned.

    next()
})

router.get("/", (req, res) => {
    res.redirect("/workshop/content/")
})

router.get("/workshop/", (req, res) => {
    res.redirect("/workshop/content/")
})
