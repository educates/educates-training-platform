import assert from "node:assert/strict";
import { test } from "node:test";
import { LocalCluster, parseClusterList } from "./clusters.mjs";

test("the CLI's list of local clusters is read by its column headings", () => {
  const rows = parseClusterList(
    [
      "NAME        STATE            NODES   KUBERNETES   LISTEN IP     DOMAIN                   CONTEXT",
      "(default)   running          3       v1.36.4      10.10.10.1    educates.test            kind-educates",
      "lookup-a    partly running   1       v1.36.4      10.10.10.18   lookup-a.educates.test   kind-educates-lookup-a",
      "",
    ].join("\n"),
  );
  assert.deepEqual(rows, [
    { name: "(default)", state: "running", listenAddress: "10.10.10.1", domain: "educates.test", context: "kind-educates" },
    {
      name: "lookup-a",
      state: "partly running",
      listenAddress: "10.10.10.18",
      domain: "lookup-a.educates.test",
      context: "kind-educates-lookup-a",
    },
  ]);
});

test("a list of local clusters without the node and version columns is read too", () => {
  const rows = parseClusterList(
    [
      "NAME        STATE     LISTEN IP    DOMAIN          CONTEXT",
      "(default)   running   10.10.10.1   educates.test   kind-educates",
    ].join("\n"),
  );
  assert.deepEqual(rows, [
    { name: "(default)", state: "running", listenAddress: "10.10.10.1", domain: "educates.test", context: "kind-educates" },
  ]);
});

const CA ="-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n";

// cluster returns a local cluster that reads the given ingress status and
// CA trust Secret in place of asking the CLI and kubectl. A null secret
// stands for a cluster without the Secret.
function cluster({ ingress, secret }) {
  const c = new LocalCluster({ name: "lookup-a", listenAddress: "10.10.10.18", cli: "educates", helper: "helper" });
  c.listed = async () => [
    { name: "lookup-a", state: "running", listenAddress: "10.10.10.18", domain: ingress.domain, context: c.context },
  ];
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

test("a cluster served over HTTPS resolves with its ingress CA certificate", async () => {
  const c = await cluster({ ingress: { domain: "a.educates.test", protocol: "https" }, secret: caSecret }).resolve();
  assert.equal(c.protocol, "https");
  assert.equal(c.domain, "a.educates.test");
  assert.equal(c.listenAddress, "10.10.10.18");
  assert.equal(c.caCertificate, CA);
});

test("a cluster served over HTTPS without the CA trust Secret does not resolve", async () => {
  await assert.rejects(
    cluster({ ingress: { domain: "a.educates.test", protocol: "https" }, secret: null }).resolve(),
    /educates-ingress-ca/,
  );
});

test("a cluster served over plain HTTP resolves without a CA trust Secret", async () => {
  const c = await cluster({ ingress: { domain: "a.educates.test", protocol: "http" }, secret: null }).resolve();
  assert.equal(c.protocol, "http");
  assert.equal(c.caCertificate, null);
});
