// Running one fixture: a session requested, checked, walked and held as an
// attendee would, and its result.

import fs from "node:fs";
import path from "node:path";
import { PortalClient, holdConsole, newFixtureLog, openDashboard, walkInstructions } from "./browser.mjs";
import { consoleLog, kubectlChecks, tunnelCheck, waitForSessionPod } from "./checks.mjs";
import { workshopName } from "./portal.mjs";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Session requests are spaced out: the portal has failed a request made in
// the same second as others with "database is locked".
let lastRequest = 0;
async function spacedRequest(fn) {
  const wait = lastRequest + 10_000 - Date.now();
  lastRequest = Math.max(Date.now(), lastRequest + 10_000);
  if (wait > 0) await sleep(wait);
  return fn();
}

export async function runFixture(h, fixture) {
  const dir = path.join(h.outDir, fixture.name);
  fs.mkdirSync(dir, { recursive: true });
  const log = newFixtureLog(dir);
  const say = (message) => h.say(`[${fixture.name}] ${message}`);
  const spec = h.workshops[fixture.name]?.spec?.session?.applications ?? {};
  const result = {
    fixture: fixture.name,
    summary: fixture.summary,
    features: {
      console: !!spec.console?.enabled,
      vcluster: !!spec.vcluster?.enabled,
      tunnel: !!(spec.sshd?.enabled && spec.sshd?.tunnel?.enabled),
    },
    problems: [],
    notes: [],
  };
  const started = Date.now();
  const context = await h.browser.createBrowserContext();
  const pages = new Set();
  // Pages an action opens, and dialogs, block a headless page: close them.
  // The harness's own pages are known by the time the delay ends.
  context.on("targetcreated", async (target) => {
    const p = await target.page().catch(() => null);
    if (!p) return;
    await sleep(2000);
    if (!pages.has(p)) {
      log("popup-closed", { url: target.url().split("?")[0] });
      await p.close().catch(() => {});
    }
  });
  const newPage = async () => {
    const p = await context.newPage();
    pages.add(p);
    p.on("dialog", async (d) => {
      log("dialog-dismissed", { type: d.type(), message: d.message().slice(0, 120) });
      await d.dismiss().catch(() => {});
    });
    p.on("console", (m) => {
      if (m.type() === "error") log("page-console-error", { text: m.text().slice(0, 300) });
    });
    return p;
  };

  let portal = null;
  try {
    const apiPage = await newPage();
    portal = new PortalClient(apiPage, h.portalUrl, h.credentials);
    await portal.open();
    const environment = await portal.environmentFor(workshopName(h.portal, fixture));
    result.environment = environment;

    let request;
    for (let attempt = 1; ; attempt++) {
      request = await spacedRequest(() => portal.requestSession(environment));
      if (request.status === 200 || request.status < 500 || attempt === 3) break;
      result.notes.push(`session request attempt ${attempt} answered HTTP ${request.status}: ${request.body.slice(0, 120)}`);
      log("request-retry", { attempt, status: request.status, body: request.body.slice(0, 300) });
    }
    if (request.status !== 200) throw new Error(`session request failed: HTTP ${request.status} ${request.body.slice(0, 200)}`);
    const details = request.details;
    result.session = details.name;
    log("session-requested", { name: details.name, environment, workshop: details.workshop });
    say(`session ${details.name} requested`);

    const page = await newPage();
    await openDashboard(page, h.portalUrl, details, h.cluster, log);

    const pod = await waitForSessionPod(h.cluster, environment, details.name, log);
    result.pod = pod;
    result.kubectl = await kubectlChecks(h.cluster, environment, pod);
    log("kubectl-checks", result.kubectl);
    say(`kubectl checks done, walking the instructions`);

    result.walk = await walkInstructions(page, dir, log, { ignore: fixture.ignoreTerminalLines });
    say(`walked ${result.walk.pages} pages, ${result.walk.clicked} actions clicked`);
    if (!result.walk.completed) result.problems.push("the walk did not reach the last instruction page");
    for (const f of result.walk.failed) result.problems.push(`walk: ${f.page}${f.action !== undefined ? ` action ${f.action}` : ""}: ${f.error}`);
    for (const e of result.walk.terminalErrors) result.problems.push(`terminal error on ${e.page}: ${e.line}`);

    if (result.features.console) {
      say(`holding the console for ${Math.round(h.profile.holdMs / 1000)}s`);
      const hold = await holdConsole(page, dir, details.name, log, h.profile);
      result.console = summariseHold(hold);
      result.console.log = await consoleLog(h.cluster, environment, pod);
    }

    if (result.features.tunnel) {
      say("connecting over the SSH tunnel from this host");
      result.tunnel = await tunnelCheck(h.cluster, environment, details.name, dir);
      log("tunnel", result.tunnel);
      if (!result.tunnel.connected) result.problems.push(`tunnel: ${result.tunnel.output || "no connection"}`);
    }

    const restarts = await h.cluster.kubectlJson(["-n", environment, "get", "pod", pod]);
    const workshopContainer = restarts.status?.containerStatuses?.find((c) => c.name === "workshop");
    result.workshopContainerRestarts = workshopContainer?.restartCount ?? null;
  } catch (e) {
    result.problems.push(`harness: ${String(e.message || e).slice(0, 400)}`);
    log("fatal", { error: String(e.stack || e).slice(0, 2000) });
  } finally {
    if (portal?.token && result.session) {
      const t = await portal.terminate(result.session).catch((e) => ({ status: -1, body: String(e) }));
      log("session-terminated", { status: t.status });
    }
    await context.close().catch(() => {});
  }
  result.seconds = Math.round((Date.now() - started) / 1000);
  result.passed = result.problems.length === 0;
  fs.writeFileSync(path.join(dir, "result.json"), JSON.stringify(result, null, 2));
  say(result.passed ? "passed" : `failed: ${result.problems.length} problem(s)`);
  return result;
}

// summariseHold reduces the console probes to whether the console answered
// throughout, what its namespace selector showed, and what it listed.
export function summariseHold(hold) {
  const probes = hold.probes;
  const answered = probes.filter((p) => p.pods?.status === 200).length;
  const last = probes[probes.length - 1] ?? {};
  const selectors = [...new Set(probes.map((p) => p.selector).filter(Boolean))];
  return {
    tab: hold.tab,
    probes: probes.length,
    answered,
    state: !hold.tab ? "no console tab" : answered === 0 ? "down" : answered === probes.length ? "up" : "intermittent",
    maxPodListMs: Math.max(0, ...probes.map((p) => p.pods?.ms ?? 0)),
    selector: selectors.join(", ") || null,
    namespaceList: last.namespaceList ?? null,
    settings: last.settings ?? null,
    maxNotifications: Math.max(0, ...probes.map((p) => p.notifications ?? 0)),
  };
}
