// The run's own test training portal: its fixture workshops, the robot
// credentials sessions are requested with, and its removal.

import fs from "node:fs";
import path from "node:path";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// Sessions stay alive this long without the portal's page open, since the
// harness holds sessions outside it, and the portal reports activity only
// from its own page.
const SESSION_LIFETIME = "120m";

export function loadFixtures(fixturesDir) {
  return fs
    .readdirSync(fixturesDir, { withFileTypes: true })
    .filter((e) => e.isDirectory())
    .map((e) => {
      const dir = path.join(fixturesDir, e.name);
      const meta = JSON.parse(fs.readFileSync(path.join(dir, "fixture.json"), "utf8"));
      return {
        name: e.name,
        dir,
        sample: meta.sample ?? null,
        summary: meta.summary ?? "",
        // Terminal lines the workshop prints as expected output, which the
        // walk would otherwise report as errors.
        ignoreTerminalLines: (meta.ignoreTerminalLines ?? []).map((r) => new RegExp(r)),
      };
    })
    .sort((a, b) => a.name.localeCompare(b.name));
}

export function portalManifest(name) {
  return {
    apiVersion: "training.educates.dev/v1beta1",
    kind: "TrainingPortal",
    metadata: { name, labels: { "training.educates.dev/harness": "session-acceptance" } },
    spec: {
      portal: {
        registration: { type: "anonymous" },
        updates: { workshop: true },
        sessions: { maximum: 20 },
        workshop: { defaults: { reserved: 0, initial: 0 } },
      },
      workshops: [],
    },
  };
}

async function portalExists(cluster, name) {
  const result = await cluster.kubectl(["get", "trainingportals.training.educates.dev", name, "--ignore-not-found", "-o", "name"]);
  return result.code === 0 && result.stdout.trim() !== "";
}

export function workshopName(portal, fixture) {
  return `${portal}-${fixture.name}`;
}

// deployPortal creates the test portal, after removing one left by an
// earlier run, and deploys each fixture to it with the CLI, publishing the
// files of the fixtures built from a workshop sample first.
export async function deployPortal({ cluster, portal, fixtures, allFixtures, samplesDir, buildDir, workshopVersion, log }) {
  // Remove what an earlier run left: the portal, or namespaces of it that
  // are still terminating.
  await removePortal({ cluster, portal, fixtures: allFixtures, log });

  log(`Creating the test portal ${portal}`);
  const apply = await cluster.kubectl(["apply", "-f", "-"], { input: JSON.stringify(portalManifest(portal)) });
  if (apply.code !== 0) throw new Error(`creating the test portal ${portal} failed: ${apply.stderr.trim()}`);

  for (const fixture of fixtures) {
    const dir = path.join(buildDir, fixture.name);
    fs.rmSync(dir, { recursive: true, force: true });
    if (fixture.sample) {
      fs.cpSync(path.join(samplesDir, fixture.sample), dir, {
        recursive: true,
        // Leave out what rendering the sample locally leaves behind.
        filter: (src) => !/[\\/]workshop[\\/](public|\.hugo_build\.lock)$/.test(src),
      });
    }
    fs.mkdirSync(path.join(dir, "resources"), { recursive: true });
    fs.copyFileSync(path.join(fixture.dir, "workshop.yaml"), path.join(dir, "resources", "workshop.yaml"));

    if (fixture.sample) {
      log(`Publishing the files of ${fixture.name} (from workshop-samples/${fixture.sample})`);
      await cluster.runCli(["workshop", "publish", dir, "--workshop-version", workshopVersion]);
    }
    log(`Deploying ${fixture.name} to ${portal}`);
    await cluster.educates([
      "cluster", "workshop", "deploy",
      "--file", dir,
      "--portal", portal,
      "--name", workshopName(portal, fixture),
      "--workshop-version", workshopVersion,
      "--reserved", "0",
      "--initial", "0",
      "--orphaned", SESSION_LIFETIME,
      "--expires", SESSION_LIFETIME,
    ]);
  }

  return waitForPortal({ cluster, portal, log });
}

async function waitForPortal({ cluster, portal, log }) {
  log(`Waiting for the test portal ${portal} to run`);
  const deadline = Date.now() + 10 * 60_000;
  while (Date.now() < deadline) {
    const tp = await cluster.kubectlJson(["get", "trainingportals.training.educates.dev", portal]);
    const status = tp.status?.educates;
    // The phase turns Running before the portal's own Deployment is up.
    if (status?.phase === "Running" && status.url && status.namespace) {
      const rollout = await cluster.kubectl(
        ["-n", status.namespace, "rollout", "status", "deployment/training-portal", `--timeout=${Math.max(1, Math.round((deadline - Date.now()) / 1000))}s`],
        { timeout: 11 * 60_000 },
      );
      if (rollout.code === 0) return cluster.kubectlJson(["get", "trainingportals.training.educates.dev", portal]);
    }
    await sleep(5000);
  }
  throw new Error(`the test portal ${portal} was not running and available within 10 minutes`);
}

// robotCredentials reads the portal's robot credentials from the Secret its
// status names under status.educates.credentialSecrets.robot.
export async function robotCredentials(cluster, trainingPortal) {
  const status = trainingPortal.status?.educates ?? {};
  const ref = status.credentialSecrets?.robot;
  if (!ref) {
    throw new Error(`the TrainingPortal ${trainingPortal.metadata?.name} names no robot credentials Secret under status.educates.credentialSecrets.robot`);
  }
  const name = typeof ref === "string" ? ref : ref.name;
  const namespace = (typeof ref === "object" && ref.namespace) || status.namespace;
  const secret = await cluster.kubectlJson(["-n", namespace, "get", "secret", name]);
  const value = (key) => Buffer.from(secret.data?.[key] ?? "", "base64").toString("utf8");
  const creds = {
    source: `Secret ${namespace}/${name}, named by status.educates.credentialSecrets.robot`,
    username: value("username"),
    password: value("password"),
    clientId: value("client-id"),
    clientSecret: value("client-secret"),
  };
  const missing = ["username", "password", "clientId", "clientSecret"].filter((k) => !creds[k]);
  if (missing.length) throw new Error(`robot credentials from ${creds.source} lack ${missing.join(", ")}`);
  return creds;
}

// removePortal deletes the test portal, which deletes its environments and
// sessions, deletes the fixture workshops, and waits for the namespaces of
// the portal, its environments and its sessions to go, so that the next
// run can create the portal again.
export async function removePortal({ cluster, portal, fixtures, log }) {
  if (await portalExists(cluster, portal)) {
    log(`Deleting the test portal ${portal}`);
    await cluster.educates(["cluster", "portal", "delete", "--portal", portal]);
  }
  const names = fixtures.map((f) => workshopName(portal, f));
  if (names.length) {
    await cluster.kubectl(["delete", "workshops.training.educates.dev", ...names, "--ignore-not-found"]);
  }
  const owned = new RegExp(`^namespace/${portal}-(ui|w\\d+)(-|$)`);
  const deadline = Date.now() + 15 * 60_000;
  for (let first = true; ; first = false) {
    const list = await cluster.kubectl(["get", "namespaces", "-o", "name"]);
    const left = list.stdout.split("\n").filter((n) => owned.test(n));
    if (list.code === 0 && left.length === 0 && !(await portalExists(cluster, portal))) return;
    if (first) log(`Waiting for the namespaces of the test portal ${portal} to go`);
    if (Date.now() > deadline) {
      throw new Error(`the test portal ${portal} or its namespaces were still there after 15 minutes: ${left.join(", ")}`);
    }
    await sleep(5000);
  }
}
