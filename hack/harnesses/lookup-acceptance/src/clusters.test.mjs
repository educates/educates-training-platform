import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { LocalCluster } from "./clusters.mjs";

const CA ="-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n";

// LIST is what 'educates local cluster list' prints with the named local
// cluster lookup-a running.
const LIST = `Router address: 10.10.10.1

NAME       STATE     NODES   KUBERNETES   DOMAIN                   CONTEXT
lookup-a   running   1       v1.36.4      lookup-a.educates.test   kind-educates-lookup-a
`;

// STATUS is what 'educates local router status' prints for it.
const STATUS = `Router address:   10.10.10.1
Router:           running (educates-router), publishing ports 80 and 443 on 10.10.10.1
Image:            haproxy:3.4.8-alpine@sha256:8aa6458a9d22a8364a2d2ee9ce93de0cc4a2789e3cf63f0b44adc972863724d7

NAME                     CLUSTER    TLS           STATE
lookup-a.educates.test   lookup-a   passthrough   running
`;

// scratchDir makes a directory removed when the test ends.
function scratchDir(t) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "lookup-acceptance-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  return dir;
}

// fakeCli writes an executable that prints LIST for 'local cluster list'
// and STATUS for 'local router status', and returns its path.
function fakeCli(t) {
  const cli = path.join(scratchDir(t), "educates");
  fs.writeFileSync(
    cli,
    `#!/bin/sh
case "$1 $2 $3" in
"local cluster list") cat <<'EOF'
${LIST}EOF
;;
"local router status") cat <<'EOF'
${STATUS}EOF
;;
*) echo "unexpected educates $*" >&2; exit 1 ;;
esac
`,
    { mode: 0o755 },
  );
  return cli;
}

// cluster returns the local cluster lookup-a, which reads the given ingress
// status and CA trust Secret in place of asking kubectl. A null secret
// stands for a cluster without the Secret.
function cluster(t, { ingress, secret }) {
  const c = new LocalCluster({ name: "lookup-a", cli: fakeCli(t), helper: "helper" });
  c.kubectlJson = async (args) => {
    if (args.includes("educatesclusterconfigs.config.educates.dev")) return { status: { ingress } };
    if (args.includes("educates-ingress-ca")) {
      if (secret === null) throw new Error('secrets "educates-ingress-ca" not found');
      return secret;
    }
    throw new Error(`unexpected kubectl ${args.join(" ")}`);
  };
  return c;
}

const caSecret = { data: { "ca.crt": Buffer.from(CA).toString("base64") } };

test("a cluster served over HTTPS resolves to the router address, with its ingress CA certificate", async (t) => {
  const c = await cluster(t, { ingress: { domain: "lookup-a.educates.test", protocol: "https" }, secret: caSecret }).resolve();
  assert.equal(c.protocol, "https");
  assert.equal(c.domain, "lookup-a.educates.test");
  assert.equal(c.routerAddress, "10.10.10.1");
  assert.equal(c.caCertificate, CA);
});

test("a cluster served over HTTPS without the CA trust Secret does not resolve", async (t) => {
  await assert.rejects(
    cluster(t, { ingress: { domain: "lookup-a.educates.test", protocol: "https" }, secret: null }).resolve(),
    /educates-ingress-ca/,
  );
});

test("a cluster served over plain HTTP resolves to the router address without a CA trust Secret", async (t) => {
  const c = await cluster(t, { ingress: { domain: "lookup-a.educates.test", protocol: "http" }, secret: null }).resolve();
  assert.equal(c.protocol, "http");
  assert.equal(c.routerAddress, "10.10.10.1");
  assert.equal(c.caCertificate, null);
});

test("a cluster whose domain the router does not send to it does not resolve", async (t) => {
  await assert.rejects(
    cluster(t, { ingress: { domain: "elsewhere.educates.test", protocol: "https" }, secret: caSecret }).resolve(),
    /router sends no elsewhere\.educates\.test to local cluster lookup-a/,
  );
});

test("a cluster is created with its name and the build tag alone", async (t) => {
  const dir = scratchDir(t);
  const helper = path.join(dir, "local-cluster.sh");
  const argsFile = path.join(dir, "args");
  fs.writeFileSync(helper, `#!/bin/sh\nprintf '%s\\n' "$@" > '${argsFile}'\n`, { mode: 0o755 });
  const c = new LocalCluster({ name: "lookup-a", cli: "/path/to/educates", helper });
  await c.create("mytag", () => {});
  assert.deepEqual(fs.readFileSync(argsFile, "utf8").trim().split("\n"), [
    "create",
    "lookup-a",
    "--tag",
    "mytag",
    "--cli",
    "/path/to/educates",
  ]);
});
