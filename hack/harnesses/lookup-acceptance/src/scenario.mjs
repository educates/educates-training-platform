// The scenario a run sets up: on each cluster a training portal offering
// the same workshop at the same capacity, and on the lookup cluster the
// Lookup Service's configuration, which registers both clusters, a tenant
// reaching every portal, and an admin and a tenant API client.

import { randomBytes } from "node:crypto";

export const NAMES = {
  workshop: "lookup-acceptance",
  portal: "lookup-acceptance",
  tenant: "lookup-acceptance",
  adminClient: "lookup-acceptance-admin",
  tenantClient: "lookup-acceptance-tenant",
};

export const CONFIG_NAMESPACE = "educates-config";

const HARNESS_LABEL = { "training.educates.dev/harness": "lookup-acceptance" };

// Sessions placed by a run are activated within seconds and live until the
// run removes them; this only bounds those a run kept with --keep.
const SESSION_LIFETIME = "30m";

// The workshop has no content: placement needs the portal's sessions, not a
// working workshop.
export function workshopManifest() {
  return {
    apiVersion: "training.educates.dev/v1beta1",
    kind: "Workshop",
    metadata: { name: NAMES.workshop, labels: HARNESS_LABEL },
    spec: {
      title: "Lookup acceptance",
      description: "A workshop both clusters offer at the same capacity.",
      session: { applications: {} },
    },
  };
}

// portalManifest gives the portal and its environment room for capacity
// sessions, with no reserved sessions, so that every session a request gets
// is new and waits for activation in phase Allocating.
export function portalManifest(capacity) {
  return {
    apiVersion: "training.educates.dev/v1beta1",
    kind: "TrainingPortal",
    metadata: { name: NAMES.portal, labels: HARNESS_LABEL },
    spec: {
      portal: {
        registration: { type: "anonymous" },
        updates: { workshop: true },
        sessions: { maximum: capacity },
      },
      workshops: [
        { name: NAMES.workshop, capacity, reserved: 0, initial: 0, expires: SESSION_LIFETIME },
      ],
    },
  };
}

// remoteServer is the API server address of a local cluster that pods of
// another local cluster reach: its control plane container on Docker's kind
// network, by the name Docker's DNS gives it, which its API server
// certificate names.
export function remoteServer(cluster) {
  return `https://educates-${cluster.name}-control-plane:6443`;
}

// withServer returns the kubeconfig with its one server address replaced.
// 'educates admin lookup kubeconfig' writes the address of the caller's kube
// context, which on kind is a loopback port of the host that no pod reaches.
export function withServer(kubeconfig, server) {
  const pattern = /^(\s*server:\s*).*$/gm;
  const count = (kubeconfig.match(pattern) ?? []).length;
  if (count === 0) throw new Error("the kubeconfig names no server");
  if (count > 1) throw new Error(`the kubeconfig names ${count} servers, not one`);
  return kubeconfig.replace(pattern, (_, prefix) => `${prefix}${server}`);
}

export function newCredentials() {
  const password = () => randomBytes(18).toString("base64url");
  return {
    admin: { username: NAMES.adminClient, password: password() },
    tenant: { username: NAMES.tenantClient, password: password() },
  };
}

// lookupManifests registers the lookup cluster, reached through the
// Lookup Service's own remote-access token since its ClusterConfig names no
// kubeconfig, and the remote cluster, reached through the kubeconfig in the
// Secret, with a tenant whose empty selectors match both clusters' portals.
export function lookupManifests({ lookup, remote, kubeconfig, credentials }) {
  const meta = (name) => ({ name, namespace: CONFIG_NAMESPACE, labels: HARNESS_LABEL });
  const secretName = `${remote.name}-kubeconfig`;
  return [
    {
      apiVersion: "v1",
      kind: "Secret",
      metadata: meta(secretName),
      type: "Opaque",
      data: { config: Buffer.from(kubeconfig).toString("base64") },
    },
    {
      apiVersion: "lookup.educates.dev/v1beta1",
      kind: "ClusterConfig",
      metadata: meta(lookup.name),
      spec: { labels: [{ name: "harness", value: "lookup-acceptance" }] },
    },
    {
      apiVersion: "lookup.educates.dev/v1beta1",
      kind: "ClusterConfig",
      metadata: meta(remote.name),
      spec: {
        labels: [{ name: "harness", value: "lookup-acceptance" }],
        credentials: { kubeconfig: { secretRef: { name: secretName } } },
      },
    },
    {
      apiVersion: "lookup.educates.dev/v1beta1",
      kind: "TenantConfig",
      metadata: meta(NAMES.tenant),
      spec: {},
    },
    {
      apiVersion: "lookup.educates.dev/v1beta1",
      kind: "ClientConfig",
      metadata: meta(NAMES.adminClient),
      spec: { client: { password: credentials.admin.password }, roles: ["admin"] },
    },
    {
      apiVersion: "lookup.educates.dev/v1beta1",
      kind: "ClientConfig",
      metadata: meta(NAMES.tenantClient),
      spec: { client: { password: credentials.tenant.password }, roles: ["tenant"], tenants: [NAMES.tenant] },
    },
  ];
}

// apply applies manifests server side, so that no last-applied annotation
// copies the kubeconfig Secret's token, and a later run's apply takes over
// the fields an earlier one set.
export async function apply(cluster, manifests) {
  const input = manifests.map((m) => JSON.stringify(m)).join("\n");
  await cluster.mustKubectl(
    ["apply", "--server-side", "--force-conflicts", "--field-manager=lookup-acceptance", "-f", "-"],
    { input },
  );
}

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// waitFor polls check until it returns a value other than undefined, and
// throws, with what describe says about the last state seen, when it has not
// within timeout.
export async function waitFor(what, check, { timeout, interval = 3000, describe } = {}) {
  const deadline = Date.now() + timeout;
  let last;
  for (;;) {
    try {
      last = await check();
      if (last?.done) return last.value;
    } catch (e) {
      last = { error: e.message };
    }
    if (Date.now() > deadline) {
      const seen = describe ? describe(last) : last?.error ?? JSON.stringify(last?.state ?? last);
      throw new Error(`${what} did not happen within ${Math.round(timeout / 1000)} seconds; last seen: ${seen}`);
    }
    await sleep(interval);
  }
}

async function portalExists(cluster) {
  const result = await cluster.kubectl(["get", "trainingportals.training.educates.dev", NAMES.portal, "--ignore-not-found", "-o", "name"]);
  return result.code === 0 && result.stdout.trim() !== "";
}

// removePortal deletes the run's portal, which deletes its environment and
// sessions, and waits for their namespaces to go, so that a run on a
// cluster an earlier run kept starts from no sessions.
export async function removePortal(cluster, log) {
  if (await portalExists(cluster)) {
    log(`Deleting the portal ${NAMES.portal} an earlier run left on ${cluster.name}`);
    await cluster.mustKubectl(["delete", "trainingportals.training.educates.dev", NAMES.portal, "--timeout=10m"], { timeout: 11 * 60_000 });
  }
  const owned = new RegExp(`^namespace/${NAMES.portal}-(ui|w\\d+)(-|$)`);
  await waitFor(
    `the namespaces of ${NAMES.portal} on ${cluster.name} going`,
    async () => {
      const list = await cluster.mustKubectl(["get", "namespaces", "-o", "name"]);
      const left = list.stdout.split("\n").filter((n) => owned.test(n));
      return left.length === 0 ? { done: true } : { state: left };
    },
    { timeout: 15 * 60_000, interval: 5000 },
  );
}

// deployPortal applies the workshop and the portal, and waits for the
// portal's Deployment to be available and its environment to be Running.
export async function deployPortal(cluster, capacity, log) {
  log(`Deploying the portal ${NAMES.portal} on ${cluster.name}, with room for ${capacity} sessions`);
  await apply(cluster, [workshopManifest(), portalManifest(capacity)]);
  const portal = await waitFor(
    `the portal ${NAMES.portal} on ${cluster.name} running`,
    async () => {
      const tp = await cluster.kubectlJson(["get", "trainingportals.training.educates.dev", NAMES.portal]);
      const status = tp.status?.educates ?? {};
      if (status.phase !== "Running" || !status.url || !status.namespace) return { state: { phase: status.phase } };
      // The phase turns Running before the portal's own Deployment is up.
      const rollout = await cluster.kubectl(["-n", status.namespace, "rollout", "status", "deployment/training-portal", "--timeout=5s"]);
      return rollout.code === 0 ? { done: true, value: tp } : { state: { phase: status.phase, deployment: "not available" } };
    },
    { timeout: 15 * 60_000, interval: 5000 },
  );
  await waitFor(
    `the environment of ${NAMES.portal} on ${cluster.name} running`,
    async () => {
      const list = await cluster.kubectlJson(["get", "workshopenvironments.training.educates.dev", "-l", `training.educates.dev/portal.name=${NAMES.portal}`]);
      const phases = list.items.map((e) => e.status?.educates?.phase ?? "none");
      return phases.length === 1 && phases[0] === "Running" ? { done: true } : { state: phases };
    },
    { timeout: 10 * 60_000, interval: 5000 },
  );
  return portal;
}

// heldSessions lists the cluster's sessions of the run's portal, as the
// cluster holds them.
export async function heldSessions(cluster) {
  const list = await cluster.kubectlJson(["get", "workshopsessions.training.educates.dev", "-l", `training.educates.dev/portal.name=${NAMES.portal}`]);
  return list.items
    .map((s) => ({ name: s.metadata.name, phase: s.status?.educates?.phase ?? null, user: s.status?.educates?.user ?? null }))
    .sort((a, b) => a.name.localeCompare(b.name));
}
