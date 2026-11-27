import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { LocalCluster } from "./cluster.mjs";

// LIST is what 'educates local cluster list' prints with the default local
// cluster and the named local cluster test2 running.
const LIST = `Router address: 10.10.10.1

NAME        STATE     NODES   KUBERNETES   DOMAIN                CONTEXT
(default)   running   3       v1.36.4      educates.test         kind-educates
test2       running   1       v1.36.4      test2.educates.test   kind-educates-test2
`;

// STATUS is what 'educates local router status' prints for those clusters.
const STATUS = `Router address:   10.10.10.1
Router:           running (educates-router), publishing ports 80 and 443 on 10.10.10.1
Image:            haproxy:3.4.8-alpine@sha256:8aa6458a9d22a8364a2d2ee9ce93de0cc4a2789e3cf63f0b44adc972863724d7

NAME                  CLUSTER     TLS           STATE
educates.test         (default)   passthrough   running
test2.educates.test   test2       passthrough   running
`;

// fakeCli writes an executable that prints LIST for 'local cluster list'
// and status for 'local router status', whatever other arguments it gets,
// removed when the test ends, and returns its path.
function fakeCli(t, status = STATUS) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "session-acceptance-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const cli = path.join(dir, "educates");
  fs.writeFileSync(
    cli,
    `#!/bin/sh
case "$1 $2 $3" in
"local cluster list") cat <<'EOF'
${LIST}EOF
;;
"local router status") cat <<'EOF'
${status}EOF
;;
*) echo "unexpected educates $*" >&2; exit 1 ;;
esac
`,
    { mode: 0o755 },
  );
  return cli;
}

// cluster returns the local cluster name, whose EducatesClusterConfig
// publishes the ingress domain domain over HTTPS, driven by the CLI cli.
function cluster(name, domain, cli) {
  const c = new LocalCluster({ name, cli });
  c.kubectlJson = async () => ({ status: { ingress: { domain, protocol: "https" } } });
  return c;
}

test("the default local cluster resolves to the router address", async (t) => {
  const c = await cluster("", "educates.test", fakeCli(t)).resolve();
  assert.equal(c.routerAddress, "10.10.10.1");
  assert.equal(c.domain, "educates.test");
  assert.equal(c.protocol, "https");
});

test("a named local cluster resolves to the router address", async (t) => {
  const c = await cluster("test2", "test2.educates.test", fakeCli(t)).resolve();
  assert.equal(c.routerAddress, "10.10.10.1");
  assert.equal(c.domain, "test2.educates.test");
});

test("a local cluster does not resolve while the router does not run", async (t) => {
  const stopped = STATUS.replace(/^Router: .*$/m, "Router:           stopped (educates-router)");
  await assert.rejects(cluster("test2", "test2.educates.test", fakeCli(t, stopped)).resolve(), /router is stopped/);
});
