// Session acceptance harness: drives real workshop sessions on a local
// cluster the way an attendee would, and reports what it observed. See
// README.md beside the src directory.

import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { parseArgs } from "node:util";
import { defaultChrome, launchBrowser } from "./browser.mjs";
import { LocalCluster, mustRun } from "./cluster.mjs";
import { runFixture } from "./fixture.mjs";
import { onInterrupt, once } from "./interrupt.mjs";
import { AuthenticatedMirror } from "./mirror.mjs";
import { deployPortal, loadFixtures, removePortal, robotCredentials } from "./portal.mjs";
import { markdownReport, summaryTable } from "./report.mjs";
import { WebsiteTheme } from "./theme.mjs";

const HARNESS_DIR = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const REPO_ROOT = path.resolve(HARNESS_DIR, "..", "..", "..");

export const PROFILES = {
  quick: { holdMs: 60_000, probeEvery: 30_000 },
  full: { holdMs: 15 * 60_000, probeEvery: 60_000 },
};

const USAGE = `Usage: run.sh [options]

Drives a session of every fixture on a local cluster and writes a report.

  --local-cluster <name>  the named local cluster to drive; an empty value
                          selects the default local cluster (default:
                          $EDUCATES_LOCAL_CLUSTER, else the default cluster)
  --cli <path>            the educates CLI of the build under test (default:
                          this checkout's client-programs/bin/educates-<os>-<arch>)
  --profile quick|full    quick holds each console 1 minute, full 15 minutes
                          (default: quick)
  --fixtures <a,b,...>    run only these fixtures (default: every fixture)
  --hold <duration>       hold each console this long instead, as 90s or 5m
  --parallel <n>          sessions driven at once (default: 3)
  --out <dir>             results directory (default: results/<time>-<cluster>
                          beside this README)
  --portal <name>         name of the test portal (default: session-acceptance)
  --authenticated-mirror  pull the workshop image from an authenticated
                          registry mirror the run starts in the cluster, with
                          its pull secret named in the EducatesClusterConfig
  --withhold-pull-secret  with --authenticated-mirror, name no pull secret,
                          so that sessions can't pull the workshop image
  --keep                  leave the test portal, its workshops, the website
                          theme and the mirror in place
  --clean                 remove what an earlier run left in place, such as
                          with --keep, and exit
  --chrome <path>         Chrome to drive (default: $CHROME_PATH, else the
                          platform's Google Chrome)
  --headful               show Chrome's windows
  --list-fixtures         print the fixtures and exit
  -h, --help              print this help

Exit status: 0 when every fixture passed, 1 when one failed, 2 when the run
could not start.`;

function parseDuration(text) {
  const m = /^(\d+)(s|m)$/.exec(text ?? "");
  if (!m) throw new Error(`invalid duration '${text}': use a number of seconds or minutes, as 90s or 5m`);
  return Number(m[1]) * (m[2] === "m" ? 60_000 : 1000);
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

function timestamp(d = new Date()) {
  return d.toISOString().replace(/[-:]/g, "").replace(/\..*/, "").replace("T", "-");
}

async function main() {
  // run.sh starts node from this harness's directory, so that Volta picks
  // its Node version, and passes the caller's directory for relative paths.
  const callerDir = process.env.HARNESS_CALLER_CWD || process.cwd();
  const resolvePath = (p) => (p ? path.resolve(callerDir, p) : p);

  const { values } = parseArgs({
    options: {
      "local-cluster": { type: "string" },
      cli: { type: "string" },
      profile: { type: "string", default: "quick" },
      fixtures: { type: "string" },
      hold: { type: "string" },
      parallel: { type: "string", default: "3" },
      out: { type: "string" },
      portal: { type: "string", default: "session-acceptance" },
      "authenticated-mirror": { type: "boolean", default: false },
      "withhold-pull-secret": { type: "boolean", default: false },
      keep: { type: "boolean", default: false },
      clean: { type: "boolean", default: false },
      chrome: { type: "string" },
      headful: { type: "boolean", default: false },
      "list-fixtures": { type: "boolean", default: false },
      help: { type: "boolean", short: "h", default: false },
    },
  });
  if (values.help) {
    console.log(USAGE);
    return 0;
  }

  const allFixtures = loadFixtures(path.join(HARNESS_DIR, "fixtures"));
  if (values["list-fixtures"]) {
    for (const f of allFixtures) console.log(`${f.name.padEnd(24)} ${f.summary}`);
    return 0;
  }

  const profileName = values.profile;
  if (!PROFILES[profileName]) throw new UsageError(`unknown profile '${profileName}': use quick or full`);
  const profile = { ...PROFILES[profileName] };
  if (values.hold) profile.holdMs = parseDuration(values.hold);

  let fixtures = allFixtures;
  if (values.fixtures) {
    const wanted = values.fixtures.split(",").map((s) => s.trim()).filter(Boolean);
    const unknown = wanted.filter((w) => !allFixtures.some((f) => f.name === w));
    if (unknown.length) throw new UsageError(`unknown fixtures: ${unknown.join(", ")}; see --list-fixtures`);
    fixtures = allFixtures.filter((f) => wanted.includes(f.name));
  }
  const parallel = Number(values.parallel);
  if (!Number.isInteger(parallel) || parallel < 1) throw new UsageError(`--parallel must be a positive number`);
  if (values["withhold-pull-secret"] && !values["authenticated-mirror"]) {
    throw new UsageError("--withhold-pull-secret needs --authenticated-mirror");
  }
  if (values.clean && values.keep) throw new UsageError("--clean and --keep can't be given together");

  const clusterName = values["local-cluster"] ?? process.env.EDUCATES_LOCAL_CLUSTER ?? "";
  const cli = resolvePath(values.cli) || defaultCli();
  if (!fs.existsSync(cli)) throw new UsageError(`no educates CLI at ${cli}; build it or pass --cli`);
  const chrome = resolvePath(values.chrome) || defaultChrome();
  if (!fs.existsSync(chrome)) throw new UsageError(`no Chrome at ${chrome}; pass --chrome or set CHROME_PATH`);

  const say = (message) => console.log(`${new Date().toISOString().slice(11, 19)} ${message}`);

  const cluster = new LocalCluster({ name: clusterName, cli });
  try {
    await cluster.resolve();
  } catch (e) {
    throw new UsageError(e.message);
  }
  const cliVersion = (await mustRun(cli, ["version", "--full"])).stderr.trim() || "unknown";
  say(`Driving ${cluster.description} (${cluster.context}): ${cluster.protocol}://*.${cluster.domain} on ${cluster.listenAddress}`);

  const theme = new WebsiteTheme({ cluster, portal: values.portal, log: say });
  const mirror = new AuthenticatedMirror({ cluster, portal: values.portal, withholdPullSecret: values["withhold-pull-secret"], log: say });
  // removeAll removes the test portal first, since its sessions use the
  // theme and the mirror, and then the theme and the mirror.
  const removeAll = async () => {
    await removePortal({ cluster, portal: values.portal, fixtures: allFixtures, log: say });
    await theme.remove();
    await mirror.remove();
  };
  if (values.clean) {
    await removeAll();
    say("Removed what earlier runs left in place");
    return 0;
  }

  say(`CLI ${cli} (${cliVersion}), profile ${profileName}, fixtures ${fixtures.map((f) => f.name).join(", ")}`);

  const outDir = resolvePath(values.out) || path.join(HARNESS_DIR, "results", `${timestamp()}-${clusterName || "default"}`);
  fs.mkdirSync(outDir, { recursive: true });
  const buildDir = path.join(outDir, "build");

  const run = {
    cluster,
    cli,
    cliVersion,
    profileName,
    profile,
    portal: values.portal,
    started: new Date().toISOString(),
  };

  let results = [];
  // Every browser the run launches, its fixtures' own included, so that the
  // run closes them all when it ends or is interrupted.
  const browsers = new Set();
  const launch = async (profileDir) => {
    const launched = await launchBrowser({ chrome, cluster, profileDir, headful: values.headful });
    browsers.add(launched);
    launched.once("disconnected", () => browsers.delete(launched));
    return launched;
  };
  let exitCode = 0;
  // restore puts the cluster back as the run found it. The run's end and a
  // signal that interrupts the run both ask for it, and it runs once.
  const restore = once(async () => {
    await Promise.all([...browsers].map((b) => b.close().catch(() => {})));
    fs.rmSync(path.join(outDir, "chrome-profile"), { recursive: true, force: true });
    if (values.keep) {
      say(`Keeping the test portal ${values.portal}, its website theme and any mirror; remove them with 'run.sh --clean' and the same --local-cluster and --portal`);
    } else {
      await removeAll().catch((e) => {
        say(`Removing the test portal, its website theme or the mirror failed: ${e.message}`);
        exitCode = 1;
      });
    }
  });
  const interruption = onInterrupt(restore, { log: say });
  try {
    // Remove what an earlier run left: the portal, or namespaces of it that
    // are still terminating, and a mirror, which this run may not use. The
    // theme is installed over one an earlier run left.
    await removePortal({ cluster, portal: values.portal, fixtures: allFixtures, log: say });
    await mirror.remove();
    if (values["authenticated-mirror"]) {
      await mirror.install();
      run.mirror = mirror.describe();
    }
    await theme.install();
    run.theme = `${theme.describe()}, marker ${theme.marker}`;

    const trainingPortal = await deployPortal({
      cluster,
      portal: values.portal,
      theme: theme.name,
      fixtures,
      samplesDir: path.join(REPO_ROOT, "workshop-samples"),
      buildDir,
      workshopVersion: clusterName || "default",
      log: say,
    });
    const credentials = await robotCredentials(cluster, trainingPortal);
    run.credentialsSource = credentials.source;
    say(`Robot credentials from ${credentials.source}`);

    const workshops = {};
    for (const f of fixtures) {
      workshops[f.name] = await cluster.kubectlJson(["get", "workshops.training.educates.dev", `${values.portal}-${f.name}`]);
    }

    const browser = await launch(path.join(outDir, "chrome-profile"));
    const h = {
      cluster,
      browser,
      // A fixture that needs a browser of its own launches it alike.
      launchBrowser: launch,
      outDir,
      portal: values.portal,
      portalUrl: trainingPortal.status.educates.url.replace(/\/$/, ""),
      credentials,
      workshops,
      profile,
      theme,
      mirror: values["authenticated-mirror"] ? mirror : null,
      say,
    };

    const queue = [...fixtures];
    const worker = async () => {
      for (let f = queue.shift(); f; f = queue.shift()) results.push(await runFixture(h, f));
    };
    await Promise.all(Array.from({ length: Math.min(parallel, fixtures.length) }, worker));
    results.sort((a, b) => a.fixture.localeCompare(b.fixture));
    exitCode = results.every((r) => r.passed) ? 0 : 1;
  } finally {
    await restore();
  }
  // An interrupted run ends with its signal's status once the restore is
  // done, and writes no report.
  if (interruption.signal) await interruption.exiting;

  run.finished = new Date().toISOString();
  fs.writeFileSync(path.join(outDir, "report.json"), JSON.stringify({ ...run, cluster: { ...cluster }, results }, null, 2));
  fs.writeFileSync(path.join(outDir, "report.md"), markdownReport(run, results));
  console.log("");
  console.log(summaryTable(results));
  console.log("");
  say(`${exitCode === 0 ? "PASS" : "FAIL"}: report in ${path.join(outDir, "report.md")}`);
  return exitCode;
}

class UsageError extends Error {}

main().then(
  (code) => process.exit(code),
  (e) => {
    console.error(`session-acceptance: ${e.message}`);
    if (!(e instanceof UsageError)) console.error(e.stack);
    process.exit(e instanceof UsageError ? 2 : 1);
  },
);
