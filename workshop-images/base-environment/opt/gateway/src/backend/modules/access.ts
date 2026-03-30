import * as express from "express"
import * as basic_auth from "express-basic-auth"
import * as fs from "fs"
import * as path from "path"
import * as https from "https"
import { v4 as uuidv4 } from "uuid"

const { AuthorizationCode } = require('simple-oauth2')

const axios = require("axios").default

import { logger } from "./logger"

// For standalone container deployment of workshop, provide the ability to
// enable authentication using HTTP Basic authentication. In this case there
// will be no user object added to the client session.

const AUTH_USERNAME = process.env.AUTH_USERNAME
const AUTH_PASSWORD = process.env.AUTH_PASSWORD

async function install_basic_auth(app: express.Application) {
    logger.info("Register basic auth handler")

    app.use(basic_auth({
        challenge: true,
        realm: "Terminal",
        authorizer: (username: string, password: string) => {
            return username == AUTH_USERNAME && password == AUTH_PASSWORD
        }
    }))
}

// For OAuth access a handshake is perform against the portal web application.
// This mode of operation is setup by the following environment variables.

const PORTAL_CLIENT_ID = process.env.PORTAL_CLIENT_ID
const PORTAL_CLIENT_SECRET = process.env.PORTAL_CLIENT_SECRET
const PORTAL_URL = process.env.PORTAL_URL
const PORTAL_API_URL = process.env.PORTAL_API_URL

const INGRESS_PROTOCOL = process.env.INGRESS_PROTOCOL

const SESSION_NAME = process.env.SESSION_NAME

// These functions provide details on the project the deployment is, the
// service account name and the service token. These are only used when using
// cluster OAuth and rely on the service account details being mounted into
// the container.

function project_name(): string {
    const account_path = "/var/run/secrets/kubernetes.io/serviceaccount"
    const namespace_path = path.join(account_path, "namespace")

    return fs.readFileSync(namespace_path, "utf8")
}

function service_account_name(name: string): string {
    const prefix = "system:serviceaccount"
    const namespace = project_name()

    return prefix + ":" + namespace + ":" + name
}

function service_account_token(): string {
    const account_path = "/var/run/secrets/kubernetes.io/serviceaccount"
    const token_path = path.join(account_path, "token")

    return fs.readFileSync(token_path, "utf8")
}

// When using OAuth against the portal, after the user has authenticated,
// access the details of the session. We will be permitted if we are
// the owner or a staff member.

async function get_session_authorization(access_token: string) {
    const options = {
        baseURL: PORTAL_API_URL,
        headers: { "Authorization": "Bearer " + access_token },
        responseType: "json"
    };

    const url = "/workshops/session/" + SESSION_NAME + "/authorize/"

    return (await axios.get(url, options)).data
}

async function verify_session_access(access_token: any) {
    let details = await get_session_authorization(access_token["token"]["access_token"])

    logger.info("Session details", details)

    return details
}

// Setup the OAuth callback that the OAuth server makes a request against to
// deliver the access code when authentication has been successful.

let handshakes = {}

function register_oauth_callback(app: express.Application, oauth2_config: any, oauth2_client: any, verify_user: any) {
    logger.info("Register OAuth callback.")

    app.get("/oauth_callback", async (req, res) => {
        try {
            let code: string = <string>req.query.code
            let state: string = <string>req.query.state

            // If we seem to have no record of the specific handshake state,
            // redirect back to the main page and start over.

            if (handshakes[state] === undefined)
                return res.redirect("/")

            // This retrieves the next URL to redirect to from the session for
            // this particular oauth handshake.

            let next_url = handshakes[state]

            delete handshakes[state]

            // Obtain the user access token using the authorization code.

            let redirect_uri = [INGRESS_PROTOCOL, "://", req.get("host"),
                "/oauth_callback"].join("")

            let token_config = {
                redirect_uri: redirect_uri,
                scope: "user:info",
                code: code
            }

            logger.debug("token_config", { config: token_config })

            let access_token = await oauth2_client.getToken(token_config)

            logger.debug("access_token", { token: access_token })

            // Now we need to verify whether this user is allowed access
            // to the project.

            req.session.identity = await verify_user(access_token)

            if (!req.session.identity)
                return res.status(403).json("Access forbidden")

            req.session.token = JSON.stringify(access_token)

            req.session.started = (new Date()).toISOString()

            logger.info("User access granted", req.session.identity)

            return res.redirect(next_url)
        } catch (error) {
            logger.error('Unexpected error occurred', { error: error.message })

            return res.status(500).json("Authentication failed")
        }
    })
}

// Setup up redirection to the OAuth server authorization endpoint.

function register_oauth_handshake(app: express.Application, oauth2_client: any) {
    logger.info("Register OAuth handshake")

    app.get("/oauth_handshake", (req, res) => {
        // Stash the next URL after authentication in the user session keyed
        // by unique code for this oauth handshake. Use the code as the state
        // for oauth requests.

        let state = uuidv4()

        handshakes[state] = req.query.next

        let redirect_uri = [INGRESS_PROTOCOL, "://", req.get("host"),
            "/oauth_callback"].join("")

        const authorization_uri = oauth2_client.authorizeURL({
            redirect_uri: redirect_uri,
            scope: "user:info",
            state: state
        });

        logger.debug('authorization_uri', { uri: authorization_uri })

        res.redirect(authorization_uri)
    })

    app.use((req, res, next) => {
        if (!req.session.identity) {
            let next_url: string = encodeURIComponent(req.url)
            res.redirect("/oauth_handshake?next=" + next_url)
        }
        else {
            next()
        }
    })
}

// Preferred method of use is to use workshops in conjunction with the
// training portal, which provides an OAuth provider endpoint for
// authentication.

function setup_oauth_config(metadata: any, client_id: string, client_secret: string) {
    let config = {
        client: {
            id: client_id,
            secret: client_secret
        },
        auth: {
            tokenHost: metadata["issuer"],
            authorizePath: metadata["authorization_endpoint"],
            tokenPath: metadata["token_endpoint"]
        },
        options: {
            authorizationMethod: "body",
        },
        http: {
            rejectUnauthorized: false
        }
    }

    return config
}

async function install_portal_auth(app: express.Application) {
    const issuer = PORTAL_URL
    const portal_api_url = PORTAL_API_URL

    const client_id: string = PORTAL_CLIENT_ID
    const client_secret: string = PORTAL_CLIENT_SECRET

    const oauth2_metadata = {
        issuer: issuer,
        authorization_endpoint: issuer + "/oauth2/authorize/",
        token_endpoint: portal_api_url + "/oauth2/token/"
    }

    logger.info("OAuth server metadata", { metadata: oauth2_metadata })

    const oauth2_config = setup_oauth_config(oauth2_metadata, client_id,
        client_secret)

    const oauth2_client = new AuthorizationCode(oauth2_config)

    logger.info("OAuth server config", { oauth2_config: oauth2_config })

    register_oauth_callback(app, oauth2_config, oauth2_client, verify_session_access)
    register_oauth_handshake(app, oauth2_client)

    return oauth2_client
}

// Authentication via OAuth always has priority if configuration for HTTP
// basic authentication is also provided. Setting HTTP basic authentication
// password to "*" has side affect of disabling authentication. This is to
// cope with situation where wasn't possible to prevent the passing of any
// environment variables because of them being required parameters in a
// template.

export async function setup_access(app: express.Application): Promise<any> {
    let oauth2_client: any

    if (PORTAL_CLIENT_ID) {
        logger.info("Install portal oauth support.")

        oauth2_client = await install_portal_auth(app)
    }
    else if (AUTH_USERNAME) {
        if (AUTH_USERNAME != "*") {
            logger.info("Install HTTP Basic auth support.")

            await install_basic_auth(app)
        }
        else {
            logger.info("All authentication has been disabled.")
        }
    }

    return oauth2_client
}

// Helper function for checking whether access token is going to expire and
// request a new one using the refresh token. If we fail to refresh the token
// we just log it and return without failing. This will result in higher
// level function needing the access token to fail instead.

const EXPIRATION_WINDOW_IN_SECONDS = 15 * 60

export async function check_for_access_token_expiry(session: any, oauth2_client: any) {
    let access_token = oauth2_client.createToken(JSON.parse(session.token))

    function expiring(): boolean {
        return access_token.token.expires_at - (Date.now() + EXPIRATION_WINDOW_IN_SECONDS * 1000) <= 0
    }

    if (expiring()) {
        try {
            logger.debug("Refreshing accessing token", { token: access_token })

            let refresh_params = {
                scope: "user:info"
            }

            access_token = await access_token.refresh(refresh_params)

            logger.debug("Refreshed access token", { token: access_token })

            session.token = JSON.stringify(access_token)
        } catch (error) {
            logger.error("Error refreshing access token", { error: error.message })
        }
    }
}
