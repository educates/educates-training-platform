// The two named local clusters a run uses: creating and deleting them with
// local-cluster.sh, and reaching them with kubectl, through each cluster's
// own kube context, and with the educates CLI under test.

import { spawn } from "node:child_process";
import os from "node:os";
import path from "node:path";

// run runs a command and resolves with its exit code and output. It never
// rejects on a non-zero exit, so callers decide what a failure means. With
// onLine, each line the command prints is passed on as it comes.
export function run(command, args, { input, timeout = 300_000, onLine } = {}) {
  return new Promise((resolve) => {
    const child = spawn(command, args, { stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    const timer = setTimeout(() => child.kill("SIGKILL"), timeout);
    const lines = (stream) => {
      let partial = "";
      return (d) => {
        if (stream === "stdout") stdout += d;
        else stderr += d;
        if (!onLine) return;
        const parts = (partial + d).split("\n");
        partial = parts.pop();
        parts.forEach(onLine);
      };
    };
    child.stdout.on("data", lines("stdout"));
    child.stderr.on("data", lines("stderr"));
    child.on("error", (error) => {
      clearTimeout(timer);
      resolve({ code: -1, stdout, stderr: stderr + String(error) });
    });
    child.on("close", (code, signal) => {
      clearTimeout(timer);
      resolve({ code: code ?? -1, signal, stdout, stderr });
    });
    child.stdin.end(input ?? "");
  });
}

// mustRun runs a command and rejects, with its output, when it fails.
export async function mustRun(command, args, options) {
  const result = await run(command, args, options);
  if (result.code !== 0) {
    const output = (result.stderr || result.stdout).trim();
    throw new Error(`${path.basename(command)} ${args.join(" ")} failed (exit ${result.code}): ${output}`);
  }
  return result;
}

export class LocalCluster {
  constructor({ name, listenAddress, cli, helper }) {
    this.name = name;
    this.requestedAddress = listenAddress;
    this.cli = cli;
    this.helper = helper;
    this.context = `kind-educates-${name}`;
    // The CLI writes every local cluster's context into this file, whatever
    // KUBECONFIG says, so kubectl reads it from there too.
    this.kubeconfig = path.join(os.homedir(), ".kube", "config");
    this.listenAddress = null;
    this.domain = null;
    this.protocol = null;
    this.caCertificate = null;
  }

  // marker returns what local-cluster.sh recorded when it created the
  // cluster, as an object, or null when it did not create one of this name.
  async marker() {
    const result = await run(this.helper, ["show", this.name]);
    if (result.code !== 0) return null;
    return Object.fromEntries(
      result.stdout
        .split("\n")
        .map((line) => /^HARNESS_CLUSTER_([A-Z_]+)=(.*)$/.exec(line))
        .filter(Boolean)
        .map(([, key, value]) => [key, value]),
    );
  }

  // create creates the cluster with local-cluster.sh from the build tagged
  // tag, passing each line it prints to log.
  async create(tag, log) {
    const result = await run(
      this.helper,
      ["create", this.name, "--listen-address", this.requestedAddress, "--tag", tag, "--cli", this.cli],
      { timeout: 45 * 60_000, onLine: (line) => log(`  [${this.name}] ${line}`) },
    );
    if (result.code !== 0) throw new Error(`creating local cluster ${this.name} failed (exit ${result.code})`);
  }

  // delete deletes the cluster, its config and the CA key pair minted for
  // it, with local-cluster.sh, which refuses a cluster it did not create.
  async delete(log) {
    const result = await run(this.helper, ["delete", this.name, "--cli", this.cli], {
      timeout: 15 * 60_000,
      onLine: (line) => log(`  [${this.name}] ${line}`),
    });
    if (result.code !== 0) throw new Error(`deleting local cluster ${this.name} failed (exit ${result.code})`);
  }

  // listed returns the local clusters the CLI lists.
  async listed() {
    const list = await mustRun(this.cli, ["local", "cluster", "list"]);
    return parseClusterList(list.stdout);
  }

  // resolve reads the cluster's listen address and state from the CLI's
  // list of local clusters, its ingress domain and protocol from its
  // EducatesClusterConfig status, and, for a cluster served over HTTPS, its
  // ingress CA certificate from the CA trust Secret the operator publishes.
  // A cluster served over plain HTTP has no such Secret, and no CA
  // certificate.
  async resolve() {
    const row = (await this.listed()).find((r) => r.context === this.context);
    if (!row) throw new Error(`local cluster ${this.name} (kube context ${this.context}) is not in 'educates local cluster list'`);
    if (row.state !== "running") throw new Error(`local cluster ${this.name} is ${row.state}, not running`);
    this.listenAddress = row.listenAddress;
    const config = await this.kubectlJson(["get", "educatesclusterconfigs.config.educates.dev", "cluster"]);
    const ingress = config.status?.ingress ?? {};
    if (!ingress.domain || !ingress.protocol) {
      throw new Error(`the EducatesClusterConfig of ${this.name} publishes no ingress domain and protocol yet`);
    }
    this.domain = ingress.domain;
    this.protocol = ingress.protocol;
    this.caCertificate = null;
    if (this.protocol !== "https") return this;
    const secret = await this.kubectlJson(["-n", "educates-installer", "get", "secret", "educates-ingress-ca"]);
    this.caCertificate = Buffer.from(secret.data?.["ca.crt"] ?? "", "base64").toString("utf8");
    if (!this.caCertificate.includes("BEGIN CERTIFICATE")) {
      throw new Error(`the CA trust Secret educates-installer/educates-ingress-ca of ${this.name} holds no ca.crt`);
    }
    return this;
  }

  // serves reports whether a URL's host is one of this cluster's ingress
  // names, which resolve to its listen address.
  serves(url) {
    const host = new URL(url).hostname;
    return host === this.domain || host.endsWith(`.${this.domain}`);
  }

  kubectl(args, options) {
    return run("kubectl", ["--kubeconfig", this.kubeconfig, "--context", this.context, ...args], options);
  }

  async mustKubectl(args, options) {
    return mustRun("kubectl", ["--kubeconfig", this.kubeconfig, "--context", this.context, ...args], options);
  }

  async kubectlJson(args, options) {
    const result = await this.mustKubectl([...args, "-o", "json"], options);
    return JSON.parse(result.stdout);
  }

  // educates runs a CLI command that acts on a cluster, selecting this one.
  educates(args, options) {
    return mustRun(this.cli, [...args, `--local-cluster=${this.name}`], options);
  }
}

// parseClusterList parses the table 'educates local cluster list' prints.
// The state column may hold a space ("partly running"), so the columns are
// taken from the right.
export function parseClusterList(text) {
  return text
    .split("\n")
    .slice(1)
    .map((line) => line.trim().split(/\s+/))
    .filter((cols) => cols.length >= 5)
    .map((cols) => ({
      name: cols[0],
      state: cols.slice(1, cols.length - 3).join(" "),
      listenAddress: cols[cols.length - 3],
      domain: cols[cols.length - 2],
      context: cols[cols.length - 1],
    }));
}
