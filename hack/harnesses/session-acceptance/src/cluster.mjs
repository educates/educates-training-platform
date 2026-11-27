// The local cluster a run drives, and the commands that reach it: kubectl
// through the cluster's own kube context, and the educates CLI with the
// cluster selected the way the CLI selects it.

import { spawn } from "node:child_process";
import os from "node:os";
import path from "node:path";
import { parseClusterList } from "../../lib/local-cluster-list.mjs";
import { routerAddressFor } from "../../lib/local-router-status.mjs";

// run runs a command and resolves with its exit code and output. It never
// rejects on a non-zero exit, so callers decide what a failure means.
export function run(command, args, { input, timeout = 300_000 } = {}) {
  return new Promise((resolve) => {
    const child = spawn(command, args, { stdio: ["pipe", "pipe", "pipe"] });
    let stdout = "";
    let stderr = "";
    const timer = setTimeout(() => child.kill("SIGKILL"), timeout);
    child.stdout.on("data", (d) => (stdout += d));
    child.stderr.on("data", (d) => (stderr += d));
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
  // name is the named local cluster, or "" for the default local cluster.
  constructor({ name, cli }) {
    this.name = name;
    this.cli = cli;
    this.context = name ? `kind-educates-${name}` : "kind-educates";
    // The CLI writes every local cluster's context into this file, whatever
    // KUBECONFIG says, so kubectl reads it from there too.
    this.kubeconfig = path.join(os.homedir(), ".kube", "config");
    this.routerAddress = null;
    this.domain = null;
    this.protocol = null;
  }

  get description() {
    return this.name ? `local cluster ${this.name}` : "the default local cluster";
  }

  // resolve reads the cluster's state from the CLI's list of local clusters,
  // its ingress domain and protocol from the cluster's EducatesClusterConfig
  // status, and the router address, where the router serves every local
  // cluster, from the CLI's router status, which has to show the router
  // running and sending the domain to this cluster.
  async resolve() {
    const list = await mustRun(this.cli, ["local", "cluster", "list"]);
    const row = parseClusterList(list.stdout).find((r) => r.context === this.context);
    if (!row) {
      throw new Error(`${this.description} (kube context ${this.context}) is not in 'educates local cluster list'`);
    }
    if (row.state !== "running") {
      throw new Error(`${this.description} is ${row.state}, not running`);
    }
    const config = await this.kubectlJson(["get", "educatesclusterconfigs.config.educates.dev", "cluster"]);
    const ingress = config.status?.ingress;
    if (!ingress?.domain || !ingress?.protocol) {
      throw new Error(`the EducatesClusterConfig of ${this.description} publishes no ingress domain and protocol yet`);
    }
    this.domain = ingress.domain;
    this.protocol = ingress.protocol;
    const status = await mustRun(this.cli, ["local", "router", "status"]);
    this.routerAddress = routerAddressFor(status.stdout, { name: this.name, domain: this.domain });
    return this;
  }

  kubectl(args, options) {
    return run("kubectl", ["--kubeconfig", this.kubeconfig, "--context", this.context, ...args], options);
  }

  async kubectlJson(args, options) {
    const result = await mustRun("kubectl", ["--kubeconfig", this.kubeconfig, "--context", this.context, ...args, "-o", "json"], options);
    return JSON.parse(result.stdout);
  }

  // educates runs a CLI command that acts on a cluster, selecting this one.
  educates(args, options) {
    return mustRun(this.cli, [...args, `--local-cluster=${this.name}`], options);
  }

  // runCli runs a CLI command that acts on no cluster.
  runCli(args, options) {
    return mustRun(this.cli, args, options);
  }

  hostname(prefix) {
    return `${prefix}.${this.domain}`;
  }
}
