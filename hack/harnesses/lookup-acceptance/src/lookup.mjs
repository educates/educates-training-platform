// HTTP to a local cluster's ingress, and the Lookup Service's REST API as
// a tenant client and an admin client call it.

import http from "node:http";
import https from "node:https";

// ingressRequest sends a request to a URL a local cluster's ingress serves.
// It connects to the cluster's listen address, the only address the cluster
// publishes on, whatever the host name resolves to, and verifies the
// certificate against the cluster's ingress CA. It never follows redirects.
export function ingressRequest(cluster, url, { method = "GET", headers = {}, body, timeout = 30_000 } = {}) {
  const u = new URL(url);
  const secure = u.protocol === "https:";
  const options = {
    host: cluster.listenAddress,
    port: u.port || (secure ? 443 : 80),
    path: `${u.pathname}${u.search}`,
    method,
    headers: { Host: u.host, ...headers },
    timeout,
  };
  if (secure) Object.assign(options, { servername: u.hostname, ca: cluster.caCertificate });
  if (body !== undefined) options.headers["Content-Length"] = Buffer.byteLength(body);
  return new Promise((resolve, reject) => {
    const req = (secure ? https : http).request(options, (res) => {
      let data = "";
      res.setEncoding("utf8");
      res.on("data", (d) => (data += d));
      res.on("end", () => resolve({ status: res.statusCode, headers: res.headers, body: data }));
    });
    req.on("timeout", () => req.destroy(new Error(`${method} ${url} had no answer within ${timeout / 1000} seconds`)));
    req.on("error", reject);
    req.end(body);
  });
}

export class LookupService {
  // cluster is the local cluster the service runs on, url its public URL.
  constructor(cluster, url) {
    this.cluster = cluster;
    this.url = url.replace(/\/$/, "");
  }

  async call(method, path, { token, json } = {}) {
    const headers = {};
    if (token) headers.Authorization = `Bearer ${token}`;
    let body;
    if (json !== undefined) {
      headers["Content-Type"] = "application/json";
      body = JSON.stringify(json);
    }
    const res = await ingressRequest(this.cluster, `${this.url}${path}`, { method, headers, body });
    let parsed = res.body;
    if (/^application\/json/.test(res.headers["content-type"] ?? "")) {
      try {
        parsed = JSON.parse(res.body);
      } catch {
        // Kept as text: the caller reports it.
      }
    }
    return { status: res.status, body: parsed };
  }

  // login returns an access token for the client, or throws with the answer.
  async login({ username, password }) {
    const res = await this.call("POST", "/auth/login", { json: { username, password } });
    if (res.status !== 200 || !res.body?.access_token) {
      throw new Error(`logging in as ${username} answered ${res.status}: ${text(res.body)}`);
    }
    return res.body.access_token;
  }

  // get returns the JSON body of an admin or tenant GET, or throws.
  async get(path, token) {
    const res = await this.call("GET", path, { token });
    if (res.status !== 200 || typeof res.body !== "object") {
      throw new Error(`GET ${path} answered ${res.status}: ${text(res.body)}`);
    }
    return res.body;
  }

  // requestSession asks for a session of the workshop for the user, as a
  // site's back end does, and returns the answer as a placement: the
  // response fields when it gave a session, else the status and body.
  async requestSession(token, { index, tenant, workshop, user, indexUrl }) {
    const started = Date.now();
    const placement = { index, user, requestedAt: new Date(started).toISOString() };
    try {
      const res = await this.call("POST", "/api/v1/workshops", {
        token,
        json: { tenantName: tenant, workshopName: workshop, clientUserId: user, clientActionId: `action-${index}`, clientIndexUrl: indexUrl },
      });
      placement.status = res.status;
      if (res.status === 200 && typeof res.body === "object") Object.assign(placement, res.body);
      else placement.body = text(res.body);
    } catch (e) {
      placement.status = null;
      placement.error = e.message;
    }
    placement.seconds = (Date.now() - started) / 1000;
    return placement;
  }
}

function text(body) {
  return (typeof body === "string" ? body : JSON.stringify(body)).trim().slice(0, 300);
}
