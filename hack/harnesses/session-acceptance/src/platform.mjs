// Reading and changing the platform's custom resources, for the changes a
// run makes to them, and waiting for the session-manager to run with them.

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// patchResource reads the resource, computes a JSON patch for it with
// compute, and applies it. The patches compute returns test the values they
// were computed from, so a patch fails when someone changed the resource in
// between; it is then computed again from a fresh read, up to three times.
export async function patchResource(cluster, [resource, name], compute) {
  let failure = "";
  for (let attempt = 1; attempt <= 3; attempt++) {
    const obj = await cluster.kubectlJson(["get", resource, name]);
    const ops = compute(obj);
    if (!ops.length) return;
    const r = await cluster.kubectl(["patch", resource, name, "--type=json", "-p", JSON.stringify(ops)]);
    if (r.code === 0) return;
    failure = (r.stderr || r.stdout).trim();
    await sleep(2000);
  }
  throw new Error(`patching ${resource} ${name} failed: ${failure}`);
}

// sessionManagerDeployment returns the name and namespace of the
// session-manager's Deployment, as the SessionManager's status names them.
export async function sessionManagerDeployment(cluster) {
  const sm = await cluster.kubectlJson(["get", "sessionmanagers.platform.educates.dev", "cluster"]);
  const ref = sm.status?.deploymentRef;
  if (!ref?.namespace) throw new Error("the SessionManager names no session-manager Deployment in status.deploymentRef");
  return ref;
}

// operatorNamespace returns the namespace the Educates operator runs in,
// where the EducatesClusterConfig's image pull Secrets live.
export async function operatorNamespace(cluster) {
  const list = await cluster.kubectlJson(["get", "deployments", "--all-namespaces", "-l", "app.kubernetes.io/name=educates-installer"]);
  const namespaces = [...new Set(list.items.map((d) => d.metadata.namespace))];
  if (namespaces.length !== 1) {
    throw new Error(`expected one Educates operator Deployment, labelled app.kubernetes.io/name=educates-installer, found ${namespaces.length}`);
  }
  return namespaces[0];
}

// waitForReady waits until the resource reports Ready for its current
// generation.
export async function waitForReady(cluster, [resource, name], { log, timeoutMs = 10 * 60_000 }) {
  log(`Waiting for ${resource} ${name} to be Ready`);
  const deadline = Date.now() + timeoutMs;
  let reason = "";
  while (Date.now() < deadline) {
    const obj = await cluster.kubectlJson(["get", resource, name]);
    const ready = (obj.status?.conditions ?? []).find((c) => c.type === "Ready");
    if (ready?.status === "True" && ready.observedGeneration === obj.metadata.generation) return;
    reason = ready ? `${ready.reason}: ${ready.message ?? ""}` : "no Ready condition";
    await sleep(5000);
  }
  throw new Error(`${resource} ${name} was not Ready within ${Math.round(timeoutMs / 60_000)} minutes: ${reason}`);
}

// The session-manager's runtime config: the Secret its chart renders and the
// key holding the config.
const CONFIG_SECRET = "educates-config";
const CONFIG_KEY = "educates-operator-config.yaml";

// waitForSessionManagerConfig waits until the session-manager runs with a
// runtime config whose text accepts approves: the config Secret's text
// passes, and the Deployment has rolled out one ready pod, created no
// earlier than the Secret last changed, carrying the Deployment's config
// checksum.
export async function waitForSessionManagerConfig(cluster, accepts, { what, log, timeoutMs = 10 * 60_000 }) {
  const { name, namespace } = await sessionManagerDeployment(cluster);
  log(`Waiting for the session-manager to run with ${what}`);
  const deadline = Date.now() + timeoutMs;
  let state = "";
  while (Date.now() < deadline) {
    state = await sessionManagerState(cluster, namespace, name, accepts);
    if (state === "ready") return;
    await sleep(5000);
  }
  throw new Error(`the session-manager did not run with ${what} within ${Math.round(timeoutMs / 60_000)} minutes: ${state}`);
}

async function sessionManagerState(cluster, namespace, name, accepts) {
  const secret = await cluster.kubectl(["-n", namespace, "get", "secret", CONFIG_SECRET, "--show-managed-fields", "-o", "json"]);
  if (secret.code !== 0) return `no Secret ${namespace}/${CONFIG_SECRET}`;
  const config = JSON.parse(secret.stdout);
  const text = Buffer.from(config.data?.[CONFIG_KEY] ?? "", "base64").toString("utf8");
  if (!accepts(text)) return "its runtime config does not have the change yet";
  const changed = Math.max(
    Date.parse(config.metadata.creationTimestamp),
    ...(config.metadata.managedFields ?? []).map((f) => Date.parse(f.time ?? 0) || 0),
  );

  const deployment = await cluster.kubectlJson(["-n", namespace, "get", "deployment", name]);
  const checksum = deployment.spec.template.metadata?.annotations?.["checksum/config"];
  const status = deployment.status ?? {};
  if (status.observedGeneration !== deployment.metadata.generation) return "its Deployment has not seen the change yet";
  const pods = await cluster.kubectlJson(["-n", namespace, "get", "pods", "-l", labelSelector(deployment.spec.selector.matchLabels)]);
  const live = pods.items.filter((p) => !p.metadata.deletionTimestamp);
  const ready = live.filter(
    (p) =>
      p.metadata.annotations?.["checksum/config"] === checksum &&
      Date.parse(p.metadata.creationTimestamp) >= changed &&
      p.status?.conditions?.some((c) => c.type === "Ready" && c.status === "True"),
  );
  if (ready.length !== 1 || live.length !== 1) return `its pods are rolling out (${ready.length} up to date and ready of ${live.length})`;
  return "ready";
}

function labelSelector(labels) {
  return Object.entries(labels)
    .map(([k, v]) => `${k}=${v}`)
    .join(",");
}
