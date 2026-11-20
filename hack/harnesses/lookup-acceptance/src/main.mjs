// Lookup acceptance harness: on two named local clusters, each with a
// training portal offering the same workshop at the same capacity, checks
// that the Lookup Service on the first alternates new sessions between the
// clusters and gives a returning user their session back. See README.md
// beside the src directory.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { LocalCluster, mustRun, run as runCommand } from "./clusters.mjs";
import { ingressRequest, LookupService } from "./lookup.mjs";
import { markdownReport, summary } from "./report.mjs";
import {
  apply,
  deployPortal,
  heldSessions,
  lookupManifests,
  NAMES,
  newCredentials,
  remoteServer,
  removePortal,
  waitFor,
  withServer,
} from "./scenario.mjs";
import { SuspensionWatch } from "./suspension.mjs";
import { judgeAlternation, judgeClusterSessions, judgeReturning } from "./verdict.mjs";

const HARNESS_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const REPO_ROOT = path.resolve(HARNESS_DIR, "..", "..", "..");
const HELPER = path.resolve(HARNESS_DIR, "..", "local-cluster.sh");

const DEFAULTS = {
  tag: "lookup-acceptance",
  lookupCluster: "lookup-acceptance-a",
  lookupAddress: "10.10.10.18",
  remoteCluster: "lookup-acceptance-b",
  remoteAddress: "10.10.10.19",
  sessions: 6,
};

// The portal deletes a session requested over its REST API that is not
// activated within 60 seconds of its request. The series is requested
// before any session is activated, so it has to fit in that window.
const MAX_SESSIONS = 10;
const REQUEST_GAP_MS = 1000;

// Where the portal sends a user whose session ends; it is never opened.
const INDEX_URL = "https://site.example.test/workshops/";

const USAGE = `Usage: run.sh [options]

Creates two named local clusters from a local build, each with a training
portal offering the same workshop at the same capacity, registers both with
the Lookup Service of the first, and checks that new sessions alternate
between the clusters and that a returning user gets their session back.
Deletes the clusters at the end.

  --tag <tag>               the PACKAGE_VERSION of the 'make local-build' to
                            check (default: ${DEFAULTS.tag})
  --cli <path>              the educates CLI that build produced (default:
                            this checkout's client-programs/bin/educates-<os>-<arch>)
  --lookup-cluster <name>   the cluster the Lookup Service is used on
                            (default: ${DEFAULTS.lookupCluster})
  --lookup-address <ip>     its listen address (default: ${DEFAULTS.lookupAddress})
  --remote-cluster <name>   the cluster it reaches as a remote cluster
                            (default: ${DEFAULTS.remoteCluster})
  --remote-address <ip>     its listen address (default: ${DEFAULTS.remoteAddress})
  --sessions <n>            new sessions to request, an even number from 2 to
                            ${MAX_SESSIONS} (default: ${DEFAULTS.sessions})
  --reuse                   use clusters an earlier run kept, created from the
                            same build, instead of refusing to start
  --keep                    leave the clusters and the scenario in place
  --delete                  delete the clusters an earlier run kept, and exit
  --out <dir>               results directory (default: results/<time> beside
                            this README)
  -h, --help                print this help

Exit status: 0 when every check passed, 1 when one failed or the run
stopped on an error, 2 when the run could not start.`;

class UsageError extends Error {}

function timestamp(d = new Date()) {
  return d.toISOString().replace(/[-:]/g, "").replace(/\..*/, "").replace("T", "-");
}

// defaultCli returns the CLI make local-build writes in this checkout, named
// after 'uname -s' and 'uname -m' as the Makefile names it.
function defaultCli() {
  const system = process.platform;
  let machine = process.arch;
  if (machine === "x64") machine = "amd64";
  else if (machine === "arm64" && system === "linux") machine = "aarch64";
  return path.join(REPO_ROOT, "client-programs", "bin", `educates-${system}-${machine}`);
}

function parseOptions() {
  let values;
  try {
    ({ values } = parseArgs({
      options: {
        tag: { type: "string", default: DEFAULTS.tag },
        cli: { type: "string" },
        "lookup-cluster": { type: "string", default: DEFAULTS.lookupCluster },
        "lookup-address": { type: "string", default: DEFAULTS.lookupAddress },
        "remote-cluster": { type: "string", default: DEFAULTS.remoteCluster },
        "remote-address": { type: "string", default: DEFAULTS.remoteAddress },
        sessions: { type: "string", default: String(DEFAULTS.sessions) },
        reuse: { type: "boolean", default: false },
        keep: { type: "boolean", default: false },
        delete: { type: "boolean", default: false },
        out: { type: "string" },
        help: { type: "boolean", short: "h", default: false },
      },
    }));
  } catch (e) {
    throw new UsageError(e.message);
  }
  const sessions = Number(values.sessions);
  if (!Number.isInteger(sessions) || sessions < 2 || sessions > MAX_SESSIONS || sessions % 2 !== 0) {
    throw new UsageError(`--sessions must be an even number from 2 to ${MAX_SESSIONS}`);
  }
  if (values["lookup-cluster"] === values["remote-cluster"]) throw new UsageError("the two clusters need different names");
  if (values["lookup-address"] === values["remote-address"]) throw new UsageError("the two clusters need different listen addresses");
  return { ...values, sessions };
}

async function main() {
  const options = parseOptions();
  if (options.help) {
    console.log(USAGE);
    return 0;
  }

  // run.sh starts node from this harness's directory, so that Volta picks
  // its Node version, and passes the caller's directory for relative paths.
  const callerDir = process.env.HARNESS_CALLER_CWD || process.cwd();
  const resolvePath = (p) => (p ? path.resolve(callerDir, p) : p);
  const cli = resolvePath(options.cli) || defaultCli();
  if (!fs.existsSync(cli)) throw new UsageError(`no educates CLI at ${cli}; build it with 'make local-build PACKAGE_VERSION=${options.tag}' or pass --cli`);

  const lookup = new LocalCluster({ name: options["lookup-cluster"], listenAddress: options["lookup-address"], cli, helper: HELPER });
  const remote = new LocalCluster({ name: options["remote-cluster"], listenAddress: options["remote-address"], cli, helper: HELPER });
  const clusters = [lookup, remote];
  const deleteCommand = `run.sh --delete${clusterNameOptions(options)}`;

  if (options.delete) return deleteClusters(clusters, (m) => console.log(m));

  const version = (await mustRun(cli, ["version"])).stderr.trim();
  if (version !== options.tag) {
    throw new UsageError(`${cli} is the CLI of build '${version}', not '${options.tag}'; pass the --tag it was built with, or build it with 'make local-build PACKAGE_VERSION=${options.tag}'`);
  }
  const cliVersion = (await mustRun(cli, ["version", "--full"])).stderr.trim();

  // A cluster an earlier run left is used only when asked for, and only when
  // it is the cluster this run would create.
  const markers = new Map();
  for (const c of clusters) {
    const marker = await c.marker();
    if (!marker) continue;
    if (!options.reuse) {
      throw new UsageError(`local cluster ${c.name} exists, left by an earlier run; pass --reuse to use it, or delete it with '${deleteCommand}'`);
    }
    if (marker.TAG !== options.tag) throw new UsageError(`local cluster ${c.name} runs build '${marker.TAG}', not '${options.tag}'; delete it with '${deleteCommand}'`);
    if (marker.LISTEN_ADDRESS !== c.requestedAddress) {
      throw new UsageError(`local cluster ${c.name} listens on ${marker.LISTEN_ADDRESS}, not ${c.requestedAddress}; pass its address`);
    }
    markers.set(c.name, marker);
  }

  const outDir = resolvePath(options.out) || path.join(HARNESS_DIR, "results", timestamp());
  fs.mkdirSync(outDir, { recursive: true });
  const logFile = fs.openSync(path.join(outDir, "run.log"), "a");
  const say = (message) => {
    const line = `${new Date().toISOString().slice(11, 19)} ${message}`;
    console.log(line);
    fs.writeSync(logFile, `${line}\n`);
  };

  const run = {
    tag: options.tag,
    cli,
    cliVersion,
    sessions: options.sessions,
    // Room for one more session than each cluster is given, so that the
    // ordering, not a full portal, decides every placement.
    capacity: options.sessions / 2 + 1,
    keep: options.keep,
    started: new Date().toISOString(),
    clusters: [],
    checks: [],
    placements: [],
    returns: [],
  };
  say(`CLI ${cli} (${cliVersion}); results in ${outDir}`);

  // A sleep of the host from the first cluster to the last check voids the
  // run; one during the teardown does not.
  const watch = new SuspensionWatch({
    onSuspension: (s) => say(`The harness did not run for ${s.seconds} seconds, from ${s.from} to ${s.to}, as when the host sleeps; this run proves nothing`),
  });
  watch.start();
  try {
    await check(run, { lookup, remote, markers, say });
  } catch (e) {
    run.error = e.message;
    say(`The run stopped: ${e.message}`);
  } finally {
    watch.stop();
    run.suspensions = watch.suspensions;
    await collectDiagnostics(run, { lookup, remote, outDir, say });
    run.clusters = clusters.map((c) => ({
      name: c.name,
      role: c === lookup ? "runs the Lookup Service" : "remote",
      listenAddress: c.listenAddress ?? c.requestedAddress,
      domain: c.domain,
      created: Boolean(c.created),
    }));
    if (options.keep) {
      say(`Keeping the clusters; run again with --reuse, or delete them with '${deleteCommand}'`);
    } else if ((await deleteClusters([remote, lookup], say)) !== 0) {
      run.teardownError = "a cluster could not be deleted; see run.log";
    }
  }

  const expectedChecks = 3 + clusters.length + 1;
  run.passed = !run.error && !run.teardownError && run.suspensions.length === 0
    && run.checks.length === expectedChecks && run.checks.every((c) => c.passed);
  run.finished = new Date().toISOString();
  fs.writeFileSync(path.join(outDir, "report.json"), JSON.stringify(run, null, 2));
  fs.writeFileSync(path.join(outDir, "report.md"), markdownReport(run));
  console.log("");
  console.log(summary(run));
  console.log("");
  say(`${run.passed ? "PASS" : "FAIL"}: report in ${path.join(outDir, "report.md")}`);
  fs.closeSync(logFile);
  return run.passed ? 0 : 1;
}

// clusterNameOptions returns the cluster name options a later run needs to
// find this run's clusters, the ones that differ from the defaults.
function clusterNameOptions(options) {
  const given = [];
  if (options["lookup-cluster"] !== DEFAULTS.lookupCluster) given.push(`--lookup-cluster ${options["lookup-cluster"]}`);
  if (options["remote-cluster"] !== DEFAULTS.remoteCluster) given.push(`--remote-cluster ${options["remote-cluster"]}`);
  return given.map((g) => ` ${g}`).join("");
}

// deleteClusters deletes each cluster local-cluster.sh created, and
// returns 0, or 1 when one could not be deleted.
async function deleteClusters(clusters, say) {
  let code = 0;
  for (const c of clusters) {
    if (!(await c.marker())) {
      say(`No local cluster ${c.name} created by local-cluster.sh to delete`);
      continue;
    }
    say(`Deleting local cluster ${c.name}`);
    try {
      await c.delete(say);
    } catch (e) {
      say(e.message);
      code = 1;
    }
  }
  return code;
}

async function check(run, { lookup, remote, markers, say }) {
  const clusters = [lookup, remote];

  for (const c of clusters) {
    if (markers.has(c.name)) {
      say(`Using local cluster ${c.name}, which an earlier run kept`);
      continue;
    }
    say(`Creating local cluster ${c.name} on ${c.requestedAddress} from build ${run.tag}; this takes several minutes`);
    await c.create(run.tag, say);
    c.created = true;
  }

  for (const c of clusters) await c.resolve();
  if (lookup.protocol !== remote.protocol) {
    throw new Error(`${lookup.name} is served over ${lookup.protocol} and ${remote.name} over ${remote.protocol}; the harness checks two clusters served the same way`);
  }
  // The Lookup Service trusts its own cluster's CA, so over HTTPS the
  // remote portal's certificate has to chain to the same root, as it does
  // for clusters created from one CLI data home. Clusters served over plain
  // HTTP have no CA.
  if (lookup.protocol === "https" && lookup.caCertificate.trim() !== remote.caCertificate.trim()) {
    throw new Error(`the ingress CAs of ${lookup.name} and ${remote.name} differ, so the Lookup Service can't verify the remote portal; create both from one CLI data home`);
  }
  say(`Lookup Service on ${lookup.name} (${lookup.protocol}://*.${lookup.domain} on ${lookup.listenAddress}); remote cluster ${remote.name} (${remote.protocol}://*.${remote.domain} on ${remote.listenAddress})`);

  run.lookupUrl = await waitFor(
    `the LookupService on ${lookup.name} becoming Ready`,
    async () => {
      const ls = await lookup.kubectlJson(["get", "lookupservices.platform.educates.dev", "cluster"]);
      const ready = (ls.status?.conditions ?? []).find((c) => c.type === "Ready");
      return ready?.status === "True" && ls.status?.url ? { done: true, value: ls.status.url } : { state: ready ?? "no Ready condition" };
    },
    { timeout: 10 * 60_000, interval: 5000 },
  );

  // A cluster an earlier run kept still holds its portal and sessions.
  await Promise.all(clusters.map((c) => removePortal(c, say)));
  await Promise.all(clusters.map((c) => deployPortal(c, run.capacity, say)));

  say(`Registering ${lookup.name} and ${remote.name} with the Lookup Service; ${remote.name}'s kubeconfig names ${remoteServer(remote)}`);
  const kubeconfig = withServer((await remote.educates(["admin", "lookup", "kubeconfig"])).stdout, remoteServer(remote));
  const credentials = newCredentials();
  await apply(lookup, lookupManifests({ lookup, remote, kubeconfig, credentials }));

  const service = new LookupService(lookup, run.lookupUrl);
  const tokens = await waitFor(
    "the Lookup Service accepting the run's clients",
    async () => ({ done: true, value: { admin: await service.login(credentials.admin), tenant: await service.login(credentials.tenant) } }),
    { timeout: 2 * 60_000 },
  );
  await awaitInventory(service, tokens, clusters, run.capacity, say);

  // New sessions, requested one after the other as a site's users arrive,
  // and activated only once the series is done, so every session placed
  // before a request still waits for activation, in phase Allocating, as
  // the sessions of #975 did.
  say(`Requesting ${run.sessions} new sessions of ${NAMES.workshop} through tenant ${NAMES.tenant}, one user each`);
  for (let i = 1; i <= run.sessions; i++) {
    const placement = await service.requestSession(tokens.tenant, {
      index: i,
      tenant: NAMES.tenant,
      workshop: NAMES.workshop,
      user: `user-${i}`,
      indexUrl: INDEX_URL,
    });
    run.placements.push(placement);
    say(`  ${describePlacement(placement)}`);
    if (i < run.sessions) await new Promise((r) => setTimeout(r, REQUEST_GAP_MS));
  }
  record(run, "New sessions alternate between the clusters", judgeAlternation(run.placements, clusters.map((c) => c.name)));

  say("Activating each session, as its user's browser would");
  const placed = run.placements.filter((p) => p.status === 200);
  for (const p of placed) p.activation = await activate(clusters, p);
  const unopened = placed.filter((p) => !p.activation.ok);
  record(run, "Every session's activation URL opens it", unopened.length
    ? { passed: false, detail: unopened.map((p) => `request ${p.index}'s activation URL answered ${p.activation.status} ${p.activation.location ?? p.activation.error ?? ""}`.trim()).join("; ") }
    : { passed: true, detail: `${placed.length} activation URLs redirected to their session` });

  await awaitAllocated(service, tokens, placed, say);
  record(run, "The clusters hold the sessions placed", await settledClusterSessions(clusters, run.placements));

  // A returning user on each cluster: the first user placed there asks for
  // the workshop again.
  for (const c of clusters) {
    const name = `A returning user on ${c.name} gets their session back`;
    const first = run.placements.find((p) => p.status === 200 && p.clusterName === c.name);
    if (!first) {
      record(run, name, { passed: false, detail: `no request got a session on ${c.name}` });
      continue;
    }
    const again = await service.requestSession(tokens.tenant, {
      index: run.placements.length + run.returns.length + 1,
      tenant: NAMES.tenant,
      workshop: NAMES.workshop,
      user: first.user,
      indexUrl: INDEX_URL,
    });
    run.returns.push(again);
    say(`  ${describePlacement(again)}`);
    let verdict = judgeReturning(first, again);
    if (verdict.passed) {
      again.activation = await activate(clusters, again);
      verdict = again.activation.ok
        ? { passed: true, detail: `${verdict.detail}, and its activation URL opens it` }
        : { passed: false, detail: `${verdict.detail}, but its activation URL answered ${again.activation.status} ${again.activation.location ?? again.activation.error ?? ""}`.trim() };
    }
    record(run, name, verdict);
  }

  // A reacquire that made a session would show as one no request got.
  await new Promise((r) => setTimeout(r, 10_000));
  record(run, "Returning users add no session", judgeClusterSessions(run.placements, await heldByCluster(clusters)));

  function record(run, name, verdict) {
    run.checks.push({ name, ...verdict });
    say(`${verdict.passed ? "PASS" : "FAIL"}  ${name}: ${verdict.detail}`);
  }
}

function describePlacement(p) {
  if (p.status !== 200) return `request ${p.index} for ${p.user}: ${p.status ?? "no answer"} ${p.body ?? p.error ?? ""} (${p.seconds.toFixed(1)}s)`;
  return `request ${p.index} for ${p.user}: ${p.clusterName} ${p.portalName}/${p.environmentName} ${p.sessionName} (${p.seconds.toFixed(1)}s)`;
}

// awaitInventory waits until the Lookup Service lists the run's portal on
// both clusters, each with one Running environment of the workshop at the
// run's capacity and no session, and lists the workshop for the tenant.
async function awaitInventory(service, tokens, clusters, capacity, say) {
  say("Waiting for the Lookup Service to list both clusters' portals");
  await waitFor(
    "the Lookup Service listing both clusters' portals and environments",
    async () => {
      const { portals } = await service.get("/api/v1/portals", tokens.admin);
      const state = {};
      for (const c of clusters) {
        const portal = portals.find((p) => p.cluster === c.name && p.name === NAMES.portal);
        if (!portal || portal.phase !== "Running") {
          state[c.name] = portal ? `portal ${portal.phase}` : "no portal";
          continue;
        }
        const { environments } = await service.get(`/api/v1/clusters/${c.name}/portals/${NAMES.portal}/environments`, tokens.admin);
        const envs = environments.filter((e) => e.workshop === NAMES.workshop);
        const e = envs[0];
        if (envs.length !== 1 || e.phase !== "Running" || e.capacity !== capacity || e.allocated !== 0 || e.available !== 0) {
          state[c.name] = envs.map((x) => `${x.name} ${x.phase} capacity ${x.capacity} allocated ${x.allocated} available ${x.available}`).join("; ") || "no environment";
          continue;
        }
        state[c.name] = "ready";
      }
      if (!Object.values(state).every((s) => s === "ready")) return { state };
      const { workshops } = await service.get(`/api/v1/tenants/${NAMES.tenant}/workshops`, tokens.tenant);
      return workshops.some((w) => w.name === NAMES.workshop) ? { done: true } : { state: { tenant: `${NAMES.workshop} not listed` } };
    },
    { timeout: 5 * 60_000 },
  );
}

// awaitAllocated waits until the Lookup Service shows every placed session
// in phase Allocated, as a returning user's request then finds it.
async function awaitAllocated(service, tokens, placed, say) {
  say("Waiting for the Lookup Service to show every session in use");
  await waitFor(
    "the Lookup Service showing every session Allocated",
    async () => {
      const pending = [];
      for (const p of placed) {
        const { sessions } = await service.get(`/api/v1/clusters/${p.clusterName}/portals/${p.portalName}/environments/${p.environmentName}/sessions`, tokens.admin);
        const s = sessions.find((x) => x.name === p.sessionName);
        if (s?.phase !== "Allocated") pending.push(`${p.sessionName} on ${p.clusterName}: ${s?.phase ?? "not listed"}`);
      }
      return pending.length ? { state: pending } : { done: true };
    },
    { timeout: 90_000 },
  );
}

async function heldByCluster(clusters) {
  return Object.fromEntries(await Promise.all(clusters.map(async (c) => [c.name, await heldSessions(c)])));
}

// settledClusterSessions judges the clusters' sessions once they agree with
// the placements, or after a minute, by what they hold then.
async function settledClusterSessions(clusters, placements) {
  const deadline = Date.now() + 60_000;
  for (;;) {
    const verdict = judgeClusterSessions(placements, await heldByCluster(clusters));
    if (verdict.passed || Date.now() > deadline) return verdict;
    await new Promise((r) => setTimeout(r, 3000));
  }
}

// activate opens a session's activation URL as the user's browser would,
// on the cluster that serves it. The portal answers with a redirect to the
// session's page.
async function activate(clusters, p) {
  const cluster = clusters.find((c) => c.serves(p.sessionActivationUrl));
  if (!cluster) return { ok: false, error: `no cluster of the run serves ${p.sessionActivationUrl}` };
  try {
    const res = await ingressRequest(cluster, p.sessionActivationUrl);
    const location = res.headers.location ? new URL(res.headers.location, p.sessionActivationUrl).pathname : null;
    return { ok: res.status === 302 && location === `/workshops/session/${p.sessionName}/`, status: res.status, location };
  } catch (e) {
    return { ok: false, error: e.message };
  }
}

async function collectDiagnostics(run, { lookup, remote, outDir, say }) {
  run.held = {};
  for (const c of [lookup, remote]) {
    try {
      run.held[c.name] = await heldSessions(c);
    } catch {
      // The cluster may not exist: the run stopped before creating it.
    }
  }
  const logs = await runCommand("kubectl", ["--kubeconfig", lookup.kubeconfig, "--context", lookup.context, "-n", "educates", "logs", "deployment/lookup-service", "--timestamps"]);
  if (logs.code === 0) fs.writeFileSync(path.join(outDir, "lookup-service.log"), logs.stdout);
  else if (run.lookupUrl) say(`Could not read the Lookup Service's log: ${logs.stderr.trim()}`);
}

main().then(
  (code) => process.exit(code),
  (e) => {
    console.error(`lookup-acceptance: ${e.message}`);
    if (!(e instanceof UsageError)) console.error(e.stack);
    process.exit(e instanceof UsageError ? 2 : 1);
  },
);
