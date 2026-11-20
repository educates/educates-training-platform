import assert from "node:assert/strict";
import { test } from "node:test";
import { LocalCluster } from "./clusters.mjs";

const CA = "-----BEGIN CERTIFICATE-----\nMIIB\n-----END CERTIFICATE-----\n";

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
