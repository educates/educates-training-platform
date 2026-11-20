// The authenticated registry mirror a run can pull the workshop image from.
// The mirror is a registry in a namespace of the cluster, serving the
// workshop image as a pull-through proxy of the registry the cluster takes
// it from, and refusing any request without its credentials. The run points
// the SessionManager's base-environment image at it and, unless the pull
// secret is withheld, names a pull Secret holding its credentials in the
// EducatesClusterConfig's image pull secrets. The operator copies that
// Secret into the session-manager's namespace, and the SecretCopier and
// SecretInjector the session-manager chart renders copy it on into the
// portal, environment and session namespaces and name it on their service
// accounts, so that sessions can pull the workshop image only through them.
//
// Nodes reach the mirror on its NodePort as localhost:<port>, which
// containerd pulls from over plain HTTP, so the mirror needs nothing on the
// host or in the nodes' containerd configuration.

import { randomBytes } from "node:crypto";
import { mustRun } from "./cluster.mjs";
import { removeNamedEntry, setNamedEntry } from "./patches.mjs";
import { operatorNamespace, patchResource, waitForReady, waitForSessionManagerConfig } from "./platform.mjs";

const USERNAME = "session-acceptance";
const REGISTRY_IMAGE = "docker.io/library/registry:3";
// The image of the SessionManager's image inventory a session's workshop
// container runs, unless its workshop names another.
const WORKSHOP_IMAGE = "base-environment";
const OVERRIDES = "/spec/images/overrides";
// The record, in the mirror's namespace, of the SessionManager's
// base-environment override before the run changed it.
const RESTORE = "session-acceptance-restore";

// pullSecretsPath returns where the EducatesClusterConfig lists its image
// pull secrets in its mode.
function pullSecretsPath(clusterConfig) {
  return clusterConfig.spec?.mode === "Inline" ? "/spec/inline/imageRegistry/pullSecrets" : "/spec/imageRegistry/pullSecrets";
}

function named(list, name) {
  return (list ?? []).find((e) => e?.name === name) ?? null;
}

// manifestPath returns the path, on a registry, of an image's manifest.
function manifestPath(image) {
  const path = image.slice(image.indexOf("/") + 1);
  const at = path.indexOf("@");
  if (at >= 0) return `/v2/${path.slice(0, at)}/manifests/${path.slice(at + 1)}`;
  const colon = path.lastIndexOf(":");
  return colon > path.lastIndexOf("/") ? `/v2/${path.slice(0, colon)}/manifests/${path.slice(colon + 1)}` : `/v2/${path}/manifests/latest`;
}

export class AuthenticatedMirror {
  constructor({ cluster, portal, withholdPullSecret = false, log }) {
    this.cluster = cluster;
    this.log = log;
    this.withholdPullSecret = withholdPullSecret;
    // The mirror's namespace, and its pull Secret in the operator's.
    this.namespace = `${portal}-mirror`;
    this.secretName = `${portal}-mirror`;
    this.image = null;
    this.upstream = null;
    this.operatorNamespace = null;
    this.checks = null;
  }

  describe() {
    const secret = this.withholdPullSecret
      ? `pull Secret withheld: the EducatesClusterConfig names none, so nothing copies or injects one`
      : `pull Secret ${this.operatorNamespace}/${this.secretName}, named in the EducatesClusterConfig's image pull secrets`;
    return `${this.image}, proxying ${this.upstream} from namespace ${this.namespace}; ${this.checks?.anonymous ?? "-"} without credentials, ${this.checks?.authenticated ?? "-"} with them; ${secret}`;
  }

  // install runs the mirror, points the SessionManager's workshop image at
  // it and, unless the pull secret is withheld, names its pull Secret in the
  // EducatesClusterConfig, then waits for the session-manager to run with
  // the change.
  async install() {
    const sm = await this.cluster.kubectlJson(["get", "sessionmanagers.platform.educates.dev", "cluster"]);
    const original = named(sm.spec?.images?.overrides, WORKSHOP_IMAGE);
    if (!original?.image) {
      throw new Error(
        `the SessionManager's spec.images.overrides names no ${WORKSHOP_IMAGE} image to mirror, as it does on a cluster deployed by the CLI of a local build`,
      );
    }

    this.log(`Starting the authenticated registry mirror in namespace ${this.namespace}`);
    const password = randomBytes(18).toString("base64url");
    const htpasswd = (await mustRun("htpasswd", ["-nbB", USERNAME, password]).catch((e) => {
      throw new Error(`creating the mirror's htpasswd file needs htpasswd: ${e.message}`);
    })).stdout.trim();

    await this.apply([
      { apiVersion: "v1", kind: "Namespace", metadata: { name: this.namespace, labels: { "training.educates.dev/harness": "session-acceptance" } } },
      {
        apiVersion: "v1",
        kind: "Service",
        metadata: { name: "registry", namespace: this.namespace },
        spec: { type: "NodePort", selector: { app: "registry" }, ports: [{ port: 5000, targetPort: 5000 }] },
      },
    ]);
    const service = await this.cluster.kubectlJson(["-n", this.namespace, "get", "service", "registry"]);
    const host = `localhost:${service.spec.ports[0].nodePort}`;
    ({ image: this.image, upstream: this.upstream } = mirrorOf(original.image, host));

    await this.apply([
      {
        apiVersion: "v1",
        kind: "ConfigMap",
        metadata: { name: RESTORE, namespace: this.namespace },
        data: { original: JSON.stringify(original), mirrored: this.image },
      },
      { apiVersion: "v1", kind: "Secret", metadata: { name: "htpasswd", namespace: this.namespace }, stringData: { htpasswd: `${htpasswd}\n` } },
      {
        apiVersion: "apps/v1",
        kind: "Deployment",
        metadata: { name: "registry", namespace: this.namespace },
        spec: {
          replicas: 1,
          selector: { matchLabels: { app: "registry" } },
          template: {
            metadata: { labels: { app: "registry" } },
            spec: {
              containers: [
                {
                  name: "registry",
                  image: REGISTRY_IMAGE,
                  env: [
                    { name: "REGISTRY_AUTH", value: "htpasswd" },
                    { name: "REGISTRY_AUTH_HTPASSWD_REALM", value: "session-acceptance" },
                    { name: "REGISTRY_AUTH_HTPASSWD_PATH", value: "/auth/htpasswd" },
                    { name: "REGISTRY_PROXY_REMOTEURL", value: this.upstream },
                    { name: "OTEL_TRACES_EXPORTER", value: "none" },
                  ],
                  ports: [{ containerPort: 5000 }],
                  readinessProbe: { tcpSocket: { port: 5000 } },
                  volumeMounts: [{ name: "auth", mountPath: "/auth", readOnly: true }],
                },
              ],
              volumes: [{ name: "auth", secret: { secretName: "htpasswd" } }],
            },
          },
        },
      },
    ]);
    const rollout = await this.cluster.kubectl(["-n", this.namespace, "rollout", "status", "deployment/registry", "--timeout=300s"], { timeout: 310_000 });
    if (rollout.code !== 0) throw new Error(`the mirror did not start: ${rollout.stderr.trim()}`);

    this.checks = await this.probe(password);
    this.log(`Mirror ${this.image}: ${this.checks.anonymous} without credentials, ${this.checks.authenticated} with them`);
    if (!/\b401\b/.test(this.checks.anonymous) || !/\b200\b/.test(this.checks.authenticated)) {
      throw new Error(`the mirror should refuse ${this.image} without credentials and serve it with them: ${this.checks.anonymous}; ${this.checks.authenticated}`);
    }

    this.operatorNamespace = await operatorNamespace(this.cluster);
    if (!this.withholdPullSecret) {
      this.log(`Naming the mirror's pull Secret ${this.operatorNamespace}/${this.secretName} in the EducatesClusterConfig`);
      await this.apply([pullSecret({ name: this.secretName, namespace: this.operatorNamespace, host, username: USERNAME, password })]);
      await patchResource(this.cluster, ["educatesclusterconfigs.config.educates.dev", "cluster"], (cfg) =>
        setNamedEntry(cfg, pullSecretsPath(cfg), { name: this.secretName }),
      );
      // The operator copies the pull Secret into the namespaces it installs
      // charts into, and upgrades the cluster services' charts with it.
      await waitForReady(this.cluster, ["educatesclusterconfigs.config.educates.dev", "cluster"], { log: this.log });
    } else {
      this.log("Withholding the mirror's pull Secret: the EducatesClusterConfig names none");
    }
    this.log(`Pointing the SessionManager's ${WORKSHOP_IMAGE} image at the mirror`);
    await patchResource(this.cluster, ["sessionmanagers.platform.educates.dev", "cluster"], (now) =>
      setNamedEntry(now, OVERRIDES, { ...original, image: this.image }),
    );

    await waitForSessionManagerConfig(
      this.cluster,
      (config) => config.includes(this.image) && (this.withholdPullSecret || config.includes(this.secretName)),
      { what: this.withholdPullSecret ? "the mirror's workshop image" : "the mirror's workshop image and pull Secret", log: this.log },
    );
  }

  async apply(items) {
    const r = await this.cluster.kubectl(["apply", "-f", "-"], { input: JSON.stringify({ apiVersion: "v1", kind: "List", items }) });
    if (r.code !== 0) throw new Error(`applying the mirror's resources failed: ${r.stderr.trim()}`);
  }

  // probe asks the mirror, from inside its own pod, for the workshop image's
  // manifest without credentials and for it with them.
  async probe(password) {
    const pods = await this.cluster.kubectlJson(["-n", this.namespace, "get", "pods", "-l", "app=registry"]);
    const pod = pods.items.find((p) => !p.metadata.deletionTimestamp)?.metadata.name;
    const url = `http://localhost:5000${manifestPath(this.image)}`;
    const accept = [
      "application/vnd.oci.image.index.v1+json",
      "application/vnd.oci.image.manifest.v1+json",
      "application/vnd.docker.distribution.manifest.list.v2+json",
      "application/vnd.docker.distribution.manifest.v2+json",
    ].join(", ");
    const ask = async (headers) => {
      const args = ["-n", this.namespace, "exec", pod, "--", "wget", "-S", "-O", "/dev/null", "--header", `Accept: ${accept}`];
      for (const h of headers) args.push("--header", h);
      const r = await this.cluster.kubectl([...args, url], { timeout: 180_000 });
      const status = /HTTP\/[\d.]+ (\d{3}[^\r\n]*)/.exec(r.stderr);
      return status ? `HTTP ${status[1].trim()}` : `no answer: ${r.stderr.trim().split("\n").pop()}`;
    };
    const basic = Buffer.from(`${USERNAME}:${password}`).toString("base64");
    return { anonymous: await ask([]), authenticated: await ask([`Authorization: Basic ${basic}`]) };
  }

  // remove puts back the SessionManager's workshop image as the run found
  // it, takes the pull Secret out of the EducatesClusterConfig, waits for
  // the session-manager to run without them, and deletes the pull Secret and
  // the mirror. It also removes what an interrupted run left.
  async remove() {
    const restore = await this.cluster.kubectl(["-n", this.namespace, "get", "configmap", RESTORE, "-o", "json"]);
    let mirrored = null;
    if (restore.code === 0) {
      const data = JSON.parse(restore.stdout).data ?? {};
      const original = JSON.parse(data.original);
      mirrored = data.mirrored;
      const sm = await this.cluster.kubectlJson(["get", "sessionmanagers.platform.educates.dev", "cluster"]);
      if (named(sm.spec?.images?.overrides, WORKSHOP_IMAGE)?.image === mirrored) {
        this.log(`Putting back the SessionManager's ${WORKSHOP_IMAGE} image, ${original.image}`);
        await patchResource(this.cluster, ["sessionmanagers.platform.educates.dev", "cluster"], (now) =>
          named(now.spec?.images?.overrides, WORKSHOP_IMAGE)?.image === mirrored ? setNamedEntry(now, OVERRIDES, original) : [],
        );
      } else {
        mirrored = null;
      }
    }

    const cfg = await this.cluster.kubectlJson(["get", "educatesclusterconfigs.config.educates.dev", "cluster"]);
    const declared = removeNamedEntry(cfg, pullSecretsPath(cfg), this.secretName).length > 0;
    if (declared) {
      this.log(`Taking the mirror's pull Secret ${this.secretName} out of the EducatesClusterConfig`);
      await patchResource(this.cluster, ["educatesclusterconfigs.config.educates.dev", "cluster"], (now) =>
        removeNamedEntry(now, pullSecretsPath(now), this.secretName),
      );
      await waitForReady(this.cluster, ["educatesclusterconfigs.config.educates.dev", "cluster"], { log: this.log });
    }
    if (mirrored || declared) {
      await waitForSessionManagerConfig(
        this.cluster,
        (config) => !(mirrored && config.includes(mirrored)) && !(declared && config.includes(this.secretName)),
        { what: "the workshop image and pull Secrets it had before the run", log: this.log },
      );
    }

    const operatorNs = await operatorNamespace(this.cluster);
    await this.cluster.kubectl(["-n", operatorNs, "delete", "secret", this.secretName, "--ignore-not-found"]);
    const ns = await this.cluster.kubectl(["get", "namespace", this.namespace, "--ignore-not-found", "-o", "name"]);
    if (ns.stdout.trim()) {
      this.log(`Deleting the mirror's namespace ${this.namespace}`);
      const r = await this.cluster.kubectl(["delete", "namespace", this.namespace, "--wait=true", "--timeout=300s"], { timeout: 310_000 });
      if (r.code !== 0) throw new Error(`deleting namespace ${this.namespace} failed: ${r.stderr.trim()}`);
    }
  }

  // sessionEvidence reports how the session's pod got its workshop image:
  // the image, whether it is the mirror's, the pull Secrets the pod and its
  // service account name, and the copy of the mirror's pull Secret in the
  // pod's namespace, with the secrets-manager's annotations on it.
  async sessionEvidence(namespace, podName) {
    const pod = await this.cluster.kubectlJson(["-n", namespace, "get", "pod", podName]);
    const image = pod.spec.containers.find((c) => c.name === "workshop")?.image ?? null;
    const serviceAccount = pod.spec.serviceAccountName ?? "default";
    const sa = await this.cluster.kubectl(["-n", namespace, "get", "serviceaccount", serviceAccount, "-o", "json"]);
    const copy = await this.cluster.kubectl(["-n", namespace, "get", "secret", this.secretName, "-o", "json"]);
    const annotations = copy.code === 0 ? JSON.parse(copy.stdout).metadata.annotations ?? {} : null;
    return {
      image,
      fromMirror: image === this.image,
      podPullSecrets: (pod.spec.imagePullSecrets ?? []).map((s) => s.name),
      serviceAccount,
      serviceAccountPullSecrets: sa.code === 0 ? (JSON.parse(sa.stdout).imagePullSecrets ?? []).map((s) => s.name) : null,
      copy: annotations
        ? Object.fromEntries(Object.entries(annotations).filter(([k]) => k.startsWith("secrets.educates.dev/")))
        : null,
    };
  }
}

// The local registry, as images name it, and as pods reach it: the CLI
// points the registry Service of the default namespace at it.
const LOCAL_REGISTRY = "localhost:5001";
const LOCAL_REGISTRY_IN_CLUSTER = "http://registry.default.svc.cluster.local";

// pullSecret returns the image pull Secret holding the credentials for the
// mirror at host.
export function pullSecret({ name, namespace, host, username, password }) {
  const auth = Buffer.from(`${username}:${password}`).toString("base64");
  return {
    apiVersion: "v1",
    kind: "Secret",
    type: "kubernetes.io/dockerconfigjson",
    metadata: { name, namespace },
    stringData: { ".dockerconfigjson": JSON.stringify({ auths: { [host]: { username, password, auth } } }) },
  };
}

const DOCKER_HUB = "docker.io";
const DOCKER_HUB_API = "https://registry-1.docker.io";

// mirrorOf returns, for an image, the same image on the mirror at host, and
// the registry the mirror proxies to serve it. An image's first path
// component names its registry when it holds a dot or a colon or is
// localhost, as for docker; otherwise the image is on Docker Hub, where a
// name of one component is under library/.
export function mirrorOf(image, host) {
  const parts = image.split("/");
  const named = parts.length > 1 && (/[.:]/.test(parts[0]) || parts[0] === "localhost");
  const registry = named ? parts.shift() : DOCKER_HUB;
  if (registry === DOCKER_HUB && parts.length === 1) parts.unshift("library");
  const upstream = registry === LOCAL_REGISTRY ? LOCAL_REGISTRY_IN_CLUSTER : registry === DOCKER_HUB ? DOCKER_HUB_API : `https://${registry}`;
  return { image: `${host}/${parts.join("/")}`, upstream };
}
