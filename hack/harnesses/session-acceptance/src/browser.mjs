// Driving a session in Chrome the way an attendee would: requesting it as
// the portal's robot client, activating it at once, walking its instruction
// pages and clicking their actions, and holding its console open.

import fs from "node:fs";
import path from "node:path";
import puppeteer from "puppeteer-core";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Lines in a terminal that count as an error in the workshop. Commands the
// actions type, shown after the prompt or as continuation lines, are not
// output and never count.
export const TERMINAL_ERROR = /forbidden|error|cannot|denied|refused|not found/i;
const TERMINAL_INPUT = /^(\[[^\]]*\] ?\$ |> )/;

export function isTerminalError(line, ignore = []) {
  return TERMINAL_ERROR.test(line) && !TERMINAL_INPUT.test(line) && !ignore.some((r) => r.test(line));
}

// Actions the walk skips: dashboard:open-url opens a browser window, which
// stalls a headless page.
const SKIPPED_HANDLERS = new Set(["dashboard:open-url"]);

export function defaultChrome() {
  if (process.env.CHROME_PATH) return process.env.CHROME_PATH;
  if (process.platform === "darwin") return "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome";
  return "/usr/bin/google-chrome";
}

// launchBrowser starts Chrome with a throwaway profile, mapping every name
// under the cluster's ingress domain to the cluster's listen address, the
// only address its ingress is published on. Chrome enables its HTTPS-First
// balanced mode by itself, which makes headless Chrome refuse a page served
// over plain HTTP with net::ERR_BLOCKED_BY_CLIENT, so that is turned off for
// a cluster served over plain HTTP; it changes nothing for one over HTTPS.
export function launchBrowser({ chrome, cluster, profileDir, headful }) {
  return puppeteer.launch({
    executablePath: chrome,
    headless: !headful,
    userDataDir: profileDir,
    acceptInsecureCerts: true,
    args: [
      `--host-resolver-rules=MAP *.${cluster.domain} ${cluster.listenAddress}`,
      "--disable-features=HttpsFirstBalancedModeAutoEnable",
      "--window-size=1600,1000",
      "--no-first-run",
      "--no-default-browser-check",
    ],
    defaultViewport: { width: 1600, height: 1000 },
    protocolTimeout: 300_000,
  });
}

// Requests to the portal's REST API run in a page on the portal's own
// origin, so the browser's name mapping and certificate handling apply.
export class PortalClient {
  constructor(page, portalUrl, credentials) {
    this.page = page;
    this.portalUrl = portalUrl;
    this.credentials = credentials;
    this.token = null;
  }

  // open logs in as the robot client. The portal's ingress can answer with
  // a server error for a while after its Deployment is available.
  async open() {
    const deadline = Date.now() + 3 * 60_000;
    for (;;) {
      await this.page.goto(`${this.portalUrl}/workshops/catalog/`, { waitUntil: "domcontentloaded", timeout: 120_000 });
      const result = await this.page.evaluate(async (c) => {
        const body = new URLSearchParams({ grant_type: "password", username: c.username, password: c.password });
        const r = await fetch("/oauth2/token/", {
          method: "POST",
          headers: { Authorization: "Basic " + btoa(`${c.clientId}:${c.clientSecret}`), "Content-Type": "application/x-www-form-urlencoded" },
          body,
        });
        return { status: r.status, body: await r.text() };
      }, this.credentials);
      if (result.status === 200) {
        this.token = JSON.parse(result.body).access_token;
        return;
      }
      if (result.status < 500 || Date.now() > deadline) {
        throw new Error(`robot login failed: HTTP ${result.status} ${result.body.slice(0, 200)}`);
      }
      await sleep(5000);
    }
  }

  async call(url) {
    return this.page.evaluate(
      async (u, token) => {
        const r = await fetch(u, { headers: { Authorization: `Bearer ${token}` }, cache: "no-store" });
        return { status: r.status, body: await r.text() };
      },
      url,
      this.token,
    );
  }

  // environmentFor waits for the portal to list a running environment of
  // the workshop and returns its name.
  async environmentFor(workshop, timeoutMs = 10 * 60_000) {
    const deadline = Date.now() + timeoutMs;
    while (Date.now() < deadline) {
      const r = await this.call("/workshops/catalog/environments/");
      if (r.status === 200) {
        const env = JSON.parse(r.body).environments.find((e) => e.workshop.name === workshop || e.workshop.resource === workshop);
        if (env) return env.name;
      }
      await sleep(5000);
    }
    throw new Error(`the portal listed no running environment of ${workshop} within ${timeoutMs / 60_000} minutes`);
  }

  async requestSession(environment) {
    const indexUrl = encodeURIComponent(`${this.portalUrl}/`);
    const r = await this.call(`/workshops/environment/${environment}/request/?index_url=${indexUrl}`);
    return { status: r.status, body: r.body, details: r.status === 200 ? JSON.parse(r.body) : null };
  }

  terminate(session) {
    return this.call(`/workshops/session/${session}/terminate/`);
  }
}

// openDashboard activates the session through the portal and opens the
// session dashboard the portal embeds top-level, which runs the session's
// own login in the page. It returns the dashboard URL.
export async function openDashboard(page, portalUrl, details, cluster, log) {
  await page.goto(`${portalUrl}${details.url}`, { waitUntil: "domcontentloaded", timeout: 120_000 });
  const host = cluster.hostname(details.name);
  let dashboardUrl = null;
  for (let i = 0; i < 150 && !dashboardUrl; i++) {
    dashboardUrl = await page
      .evaluate((h) => [...document.querySelectorAll("iframe")].map((f) => f.src).find((s) => s && new URL(s).hostname === h) || null, host)
      .catch(() => null);
    if (!dashboardUrl) await sleep(2000);
  }
  if (!dashboardUrl) throw new Error(`the portal's session page never embedded the dashboard on ${host}`);
  log("dashboard-url", { url: dashboardUrl.split("?")[0] });
  await page.goto(dashboardUrl, { waitUntil: "networkidle2", timeout: 180_000 }).catch((e) => log("dashboard-load-slow", { error: String(e).slice(0, 200) }));
  return dashboardUrl;
}

async function settledInstructionsFrame(page) {
  // An action can make the dashboard replace the instructions iframe, so
  // find it again and wait until its document names its current page.
  for (let i = 0; i < 30; i++) {
    const frame =
      page.frames().find((f) => f.name() === "workshop-iframe" && !f.detached) ||
      page.frames().find((f) => /\/workshop\/content\//.test(f.url()));
    if (frame) {
      const slug = await frame.evaluate(() => document.body && document.body.getAttribute("data-current-page")).catch(() => null);
      if (slug) return { frame, slug };
    }
    await sleep(1000);
  }
  return { frame: null, slug: null };
}

// terminalLines returns the rows each terminal shows. xterm renders only
// the visible rows into the page.
function terminalLines(page) {
  return page
    .evaluate(() =>
      [...document.querySelectorAll(".xterm-rows")].map((rows) =>
        [...rows.children].map((r) => r.textContent.replace(/\s+$/, "")).filter((l) => l),
      ),
    )
    .catch(() => []);
}

export async function screenshot(page, dir, name, log) {
  try {
    await page.screenshot({ path: path.join(dir, `${name}.png`) });
  } catch (e) {
    log("screenshot-failed", { name, error: String(e).slice(0, 200) });
  }
}

// walkInstructions walks every instruction page, clicking every action on
// it but those SKIPPED_HANDLERS names, and collects the error lines the
// terminals show after each action and at the end of each page, but those
// matching one of ignore.
export async function walkInstructions(page, dir, log, { actionPause = 4000, ignore = [] } = {}) {
  const result = { pages: 0, clicked: 0, skipped: 0, failed: [], terminalErrors: [], completed: false };
  const seenErrors = new Set();
  const collectErrors = async (slug) => {
    for (const lines of await terminalLines(page)) {
      for (const line of lines) {
        if (isTerminalError(line, ignore) && !seenErrors.has(line)) {
          seenErrors.add(line);
          result.terminalErrors.push({ page: slug, line: line.slice(0, 300) });
          log("terminal-error", { page: slug, line });
        }
      }
    }
  };

  const seen = new Set();
  for (let pageNo = 1; pageNo <= 60; pageNo++) {
    await sleep(3000);
    let { frame, slug } = await settledInstructionsFrame(page);
    if (!frame) {
      result.failed.push({ page: `#${pageNo}`, error: "no instructions page loaded" });
      log("no-instructions-frame", { pageNo });
      return result;
    }
    if (seen.has(slug)) {
      result.failed.push({ page: slug, error: "the next page link leads back to a page already walked" });
      log("instructions-loop", { pageNo, slug });
      return result;
    }
    seen.add(slug);
    result.pages++;
    const count = await frame.$$eval(".clickable-action", (els) => els.length).catch(() => 0);
    log("instructions-page", { pageNo, slug, actions: count });

    for (let i = 0; i < count; i++) {
      try {
        ({ frame } = await settledInstructionsFrame(page));
        if (!frame) throw new Error("instructions page gone");
        const actions = await frame.$$(".clickable-action");
        if (!actions[i]) throw new Error("action missing after the page reloaded");
        const info = await actions[i].evaluate((el) => {
          el.scrollIntoView({ block: "center" });
          const title = el.querySelector(".clickable-action__title") || el;
          return { handler: el.getAttribute("data-handler"), title: title.textContent.trim().slice(0, 80) };
        });
        if (SKIPPED_HANDLERS.has(info.handler)) {
          result.skipped++;
          log("action-skipped", { pageNo, i, ...info });
          continue;
        }
        const header = (await actions[i].$(".clickable-action__header")) || actions[i];
        await header.click();
        result.clicked++;
        log("action-clicked", { pageNo, i, ...info });
        await sleep(actionPause);
        await collectErrors(slug);
      } catch (e) {
        result.failed.push({ page: slug, action: i, error: String(e).slice(0, 200) });
        log("action-failed", { pageNo, i, error: String(e).slice(0, 200) });
      }
    }
    await sleep(5000);
    await collectErrors(slug);
    await screenshot(page, dir, `page-${String(pageNo).padStart(2, "0")}`, log);

    ({ frame } = await settledInstructionsFrame(page));
    const next = frame ? await frame.evaluate(() => document.body.getAttribute("data-next-page")).catch(() => null) : null;
    if (!next) {
      result.completed = true;
      log("instructions-end", { pageNo });
      return result;
    }
    const moved = await page
      .evaluate((n) => {
        const el = document.querySelector("#workshop-iframe");
        if (!el) return false;
        el.src = `/workshop/content/${n}/`;
        return true;
      }, next)
      .catch(() => false);
    if (!moved) {
      result.failed.push({ page: slug, error: `could not move to the next page ${next}` });
      log("navigate-failed", { pageNo, next });
      return result;
    }
  }
  result.failed.push({ page: "-", error: "more than 60 instruction pages" });
  return result;
}

// holdConsole opens the dashboard's Console tab and probes the console from
// inside its own frame, every probeEvery, until holdMs has passed.
export async function holdConsole(page, dir, namespace, log, { holdMs, probeEvery }) {
  const result = { tab: false, probes: [] };
  for (const tab of await page.$$("a.nav-link")) {
    const text = await tab.evaluate((el) => el.textContent.trim()).catch(() => "");
    const visible = await tab.evaluate((el) => !!(el.offsetWidth || el.offsetHeight)).catch(() => false);
    if (/^console$/i.test(text) && visible) {
      await tab.click();
      result.tab = true;
      break;
    }
  }
  log("console-tab", { clicked: result.tab });
  if (!result.tab) return result;

  const consoleFrame = () =>
    page.frames().find((f) => {
      try {
        return new URL(f.url()).hostname.startsWith("console-");
      } catch {
        return false;
      }
    });
  for (let i = 0; i < 30 && !consoleFrame(); i++) await sleep(2000);
  await sleep(15000);

  const end = Date.now() + holdMs;
  for (let n = 0; ; n++) {
    const frame = consoleFrame();
    let probe;
    if (!frame) {
      probe = { n, error: "no console frame" };
    } else {
      probe = await frame
        .evaluate(async (ns) => {
          const out = {};
          const json = async (url) => {
            const t0 = performance.now();
            const r = await fetch(url, { cache: "no-store" });
            const ms = Math.round(performance.now() - t0);
            let body = null;
            try {
              body = await r.json();
            } catch {
              body = null;
            }
            return { status: r.status, ms, body };
          };
          try {
            const r = await json("/api/v1/namespace");
            out.namespaceList = {
              status: r.status,
              names: (r.body?.namespaces || []).map((x) => x.objectMeta?.name),
              errors: (r.body?.errors || []).map((e) => e.ErrStatus?.code ?? String(e).slice(0, 80)),
            };
          } catch (e) {
            out.namespaceList = { error: String(e).slice(0, 120) };
          }
          try {
            const r = await json(`/api/v1/pod/${ns}`);
            out.pods = { status: r.status, ms: r.ms };
          } catch (e) {
            out.pods = { error: String(e).slice(0, 120) };
          }
          try {
            const r = await json("/api/v1/settings/global");
            out.settings = {
              status: r.status,
              defaultNamespace: r.body?.defaultNamespace,
              namespaceFallbackList: r.body?.namespaceFallbackList,
              itemsPerPage: r.body?.itemsPerPage,
              disableAccessDeniedNotifications: r.body?.disableAccessDeniedNotifications,
            };
          } catch (e) {
            out.settings = { error: String(e).slice(0, 120) };
          }
          const selector = document.querySelector("kd-namespace-selector, .kd-namespace-selector, mat-select");
          out.selector = selector ? selector.textContent.trim().slice(0, 80) : null;
          // The unread count on the dashboard's notifications button.
          const counter = document.querySelector(".kd-notifications-counter");
          out.notifications = counter ? Number.parseInt(counter.textContent.trim(), 10) || 0 : 0;
          return out;
        }, namespace)
        .catch((e) => ({ error: String(e).slice(0, 200) }));
      probe = { n, ...probe };
    }
    result.probes.push(probe);
    log("console-probe", probe);
    await screenshot(page, dir, `console-${String(n).padStart(2, "0")}`, log);
    if (Date.now() >= end) break;
    await sleep(Math.min(probeEvery, Math.max(0, end - Date.now())));
  }
  return result;
}

// newFixtureLog returns a logger appending JSON lines to the fixture's log.
export function newFixtureLog(dir) {
  const file = path.join(dir, "log.jsonl");
  return (event, data = {}) => {
    fs.appendFileSync(file, JSON.stringify({ t: new Date().toISOString(), event, ...data }) + "\n");
  };
}
