// Checks run against a session from outside its browser page: kubectl in
// the workshop container, the console's own log, and the SSH tunnel.

import fs from "node:fs";
import path from "node:path";
import { run } from "./cluster.mjs";

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

const PULL_FAILURES = new Set(["ErrImagePull", "ImagePullBackOff"]);

// imagePullFailure returns the first container of the pod, init containers
// first, that waits on a failed image pull, with the kubelet's reason and
// message, or null when none does.
export function imagePullFailure(pod) {
  const statuses = [...(pod.status?.initContainerStatuses ?? []), ...(pod.status?.containerStatuses ?? [])];
  for (const s of statuses) {
    const waiting = s.state?.waiting;
    if (waiting && PULL_FAILURES.has(waiting.reason)) {
      return { container: s.name, image: s.image, reason: waiting.reason, message: waiting.message ?? "" };
    }
  }
  return null;
}

// A session's pod that has failed to pull an image for this long fails the
// fixture, rather than the wait running out: the kubelet keeps retrying,
// but a refused pull doesn't heal. It is well inside the two minutes after
// which the portal's session page deletes a session that has not started.
const PULL_FAILURE_GRACE_MS = 45_000;

// waitForSessionPod waits for the session's pod in the environment
// namespace to be ready, and for its workshop container to have the
// kubeconfig its setup writes. It fails early when the pod has failed to
// pull an image for PULL_FAILURE_GRACE_MS, even if the pod has been deleted
// since.
export async function waitForSessionPod(cluster, environment, session, log) {
  const deadline = Date.now() + 10 * 60_000;
  let pod = null;
  let pullFailingSince = null;
  let failing = null;
  // The kubelet's message for a failed pull says why; once it backs off, its
  // message says only that it is backing off.
  let pullError = "";
  while (Date.now() < deadline) {
    const pods = await cluster.kubectlJson(["-n", environment, "get", "pods", "-l", `training.educates.dev/session.name=${session}`]);
    const ready = pods.items.find((p) => p.status?.conditions?.some((c) => c.type === "Ready" && c.status === "True"));
    if (ready) {
      pod = ready.metadata.name;
      break;
    }
    const now = pods.items.map((p) => ({ pod: p.metadata.name, failure: imagePullFailure(p) })).find((f) => f.failure);
    if (now) {
      if (now.failure.reason === "ErrImagePull") pullError = now.failure.message;
      if (pullFailingSince === null) {
        pullFailingSince = Date.now();
        log("session-pod-pull-failing", { pod: now.pod, ...now.failure });
      }
      failing = now;
    } else if (pods.items.length) {
      pullFailingSince = null;
      failing = null;
    }
    if (failing && Date.now() - pullFailingSince >= PULL_FAILURE_GRACE_MS) {
      const f = failing.failure;
      throw new Error(`the pod ${failing.pod} of session ${session} could not pull ${f.image}: ${pullError || `${f.reason}: ${f.message}`}`);
    }
    await sleep(3000);
  }
  if (!pod) throw new Error(`the pod of session ${session} was not ready within 10 minutes`);
  log("session-pod-ready", { pod });
  while (Date.now() < deadline) {
    const test = await cluster.kubectl(["-n", environment, "exec", pod, "-c", "workshop", "--", "test", "-f", "/home/eduk8s/.kube/config"]);
    if (test.code === 0) return pod;
    await sleep(3000);
  }
  throw new Error(`the workshop container of session ${session} has no kubeconfig after 10 minutes`);
}

// The kubectl commands run in the workshop container, as an attendee runs
// them. Only an actual request is evidence: 'kubectl auth can-i' answers
// yes for namespace list where the list itself is forbidden.
export const KUBECTL_CHECKS = [
  { id: "getOwnNamespace", title: "kubectl get ns <session namespace>", script: 'kubectl get ns "$SESSION_NAMESPACE"' },
  { id: "listNamespaces", title: "kubectl get ns", script: "kubectl get ns" },
  { id: "listPods", title: "kubectl get pods", script: "kubectl get pods" },
];

export function classifyKubectl(code, output) {
  if (code === 0) return "allowed";
  if (/forbidden/i.test(output)) return "forbidden";
  if (/NotFound|not found/i.test(output)) return "not found";
  return "error";
}

// The workshop image's shell profile runs 'ps -p $PPID', which prints its
// usage on stderr in a shell kubectl exec starts. It is no output of the
// command checked.
const PS_USAGE = /error: process ID out of range\n\nUsage:\n ps \[options\][\s\S]*?For more details see ps\(1\)\.\n?/g;

export async function kubectlChecks(cluster, environment, pod) {
  const exec = async (script) => {
    const r = await cluster.kubectl(["-n", environment, "exec", pod, "-c", "workshop", "--", "bash", "-lc", script], { timeout: 120_000 });
    return { ...r, stderr: r.stderr.replace(PS_USAGE, "") };
  };
  const results = {};
  const context = await exec("kubectl config view --minify -o jsonpath='{..namespace}'");
  results.contextNamespace = context.code === 0 ? context.stdout.trim() || "(none)" : `(error: ${context.stderr.trim()})`;
  for (const check of KUBECTL_CHECKS) {
    const r = await exec(check.script);
    const output = `${r.stdout}${r.stderr}`.trim();
    results[check.id] = {
      title: check.title,
      outcome: classifyKubectl(r.code, output),
      exitCode: r.code,
      output: output.split("\n").slice(0, 3).join("\n"),
    };
  }
  return results;
}

// consoleLog reads, from the workshop container's log, how often the
// console's dashboard started, whether it panicked, and the namespace it
// was started on.
export async function consoleLog(cluster, environment, pod) {
  const r = await cluster.kubectl(["-n", environment, "logs", pod, "-c", "workshop"], { timeout: 120_000 });
  return analyseConsoleLog(r.stdout);
}

export function analyseConsoleLog(text) {
  // The dashboard prefixes its own lines with a date and time.
  const lines = text.split("\n").map((l) => l.replace(/^[0-9/:. ]+(?=\D)/, ""));
  const starts = lines.filter((l) => /Starting overwatch/.test(l)).length;
  const panics = lines.filter((l) => /^panic: /.test(l));
  let namespace = null;
  let mode = null;
  for (const l of lines) {
    const exec = l.match(/exec \/opt\/console\/dashboard .*?--(namespace|kubeconfig) (\S+)/);
    if (exec) {
      mode = exec[1];
      namespace = exec[1] === "namespace" ? exec[2] : null;
    }
  }
  return {
    starts,
    restarts: Math.max(0, starts - 1),
    panics: panics.length,
    firstPanic: panics[0]?.slice(0, 300) ?? null,
    startedWith: mode === "kubeconfig" ? "--kubeconfig" : namespace ? `--namespace ${namespace}` : null,
    accessErrors: lines.filter((l) => /Non-critical error occurred during resource retrieval/.test(l)).length,
  };
}

// tunnelCheck connects to the session over its SSH tunnel from this host,
// with 'educates tunnel connect' from the CLI under test as the SSH proxy
// command, using the session's own key.
export async function tunnelCheck(cluster, environment, session, workDir) {
  const secret = await cluster.kubectlJson(["-n", environment, "get", "secret", `${session}-ssh-keys`]);
  const keyFile = path.join(workDir, "tunnel-id_rsa");
  fs.writeFileSync(keyFile, Buffer.from(secret.data.id_rsa, "base64"), { mode: 0o600 });
  const scheme = cluster.protocol === "https" ? "wss" : "ws";
  const proxy = `'${cluster.cli.replace(/'/g, "'\\''")}' tunnel connect --url ${scheme}://%h/tunnel/`;
  try {
    const r = await run(
      "ssh",
      [
        "-i", keyFile,
        "-o", "IdentitiesOnly=yes",
        "-o", "StrictHostKeyChecking=no",
        "-o", "UserKnownHostsFile=/dev/null",
        "-o", "LogLevel=ERROR",
        "-o", "ConnectTimeout=30",
        "-o", `ProxyCommand=${proxy}`,
        `eduk8s@${cluster.hostname(session)}`,
        'echo "Connected over the tunnel as $(whoami) on $(uname -n)"',
      ],
      { timeout: 120_000 },
    );
    const output = `${r.stdout}${r.stderr}`.trim();
    return {
      connected: r.code === 0 && /Connected over the tunnel as/.test(r.stdout),
      output: output.split("\n").slice(0, 3).join("\n"),
    };
  } finally {
    fs.rmSync(keyFile, { force: true });
  }
}
