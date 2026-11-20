import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { LocalCluster } from "./cluster.mjs";

// LIST is what 'educates local cluster list' prints with only the default
// local cluster running.
const LIST = [
  "NAME        STATE     NODES   KUBERNETES   LISTEN IP    DOMAIN          CONTEXT",
  "(default)   running   3       v1.36.4      10.10.10.1   educates.test   kind-educates",
  "",
].join("\n");

// fakeCli writes an executable that prints output whatever its arguments,
// removed when the test ends, and returns its path.
function fakeCli(t, output) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "session-acceptance-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const cli = path.join(dir, "educates");
  fs.writeFileSync(cli, `#!/bin/sh\ncat <<'EOF'\n${output}EOF\n`, { mode: 0o755 });
  return cli;
}

test("the default local cluster resolves from the CLI's list", async (t) => {
  const c = new LocalCluster({ name: "", cli: fakeCli(t, LIST) });
  c.kubectlJson = async () => ({ status: { ingress: { domain: "educates.test", protocol: "https" } } });
  await c.resolve();
  assert.equal(c.listenAddress, "10.10.10.1");
  assert.equal(c.domain, "educates.test");
  assert.equal(c.protocol, "https");
});
