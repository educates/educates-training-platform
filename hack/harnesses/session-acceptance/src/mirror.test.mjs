import assert from "node:assert/strict";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";
import { htpasswdEntry, mirrorOf, pullSecret } from "./mirror.mjs";

// fakeHtpasswd writes an executable standing for htpasswd, removed when the
// test ends, and returns its path.
function fakeHtpasswd(t, script) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "session-acceptance-"));
  t.after(() => fs.rmSync(dir, { recursive: true, force: true }));
  const command = path.join(dir, "htpasswd");
  fs.writeFileSync(command, `#!/bin/sh\n${script}\n`, { mode: 0o755 });
  return command;
}

test("the mirror's password reaches htpasswd on its standard input, never as an argument", async (t) => {
  const command = fakeHtpasswd(t, 'echo "args=$*"; echo "stdin=$(cat)"');
  const out = await htpasswdEntry("session-acceptance", "p4ss", { command });
  assert.match(out, /^args=-niB session-acceptance$/m);
  assert.match(out, /^stdin=p4ss$/m);
});

test("a failing htpasswd is reported without the mirror's password", async (t) => {
  const command = fakeHtpasswd(t, 'echo "htpasswd: $* $(cat)" >&2; exit 3');
  await assert.rejects(htpasswdEntry("session-acceptance", "p4ss", { command }), (e) => {
    assert.match(e.message, /htpasswd/);
    assert.match(e.message, /exit 3/);
    assert.doesNotMatch(e.message, /p4ss/);
    return true;
  });
});

test("an image of the local registry is mirrored from the local registry's in-cluster address", () => {
  assert.deepEqual(mirrorOf("localhost:5001/educates-base-environment:4.0.0-dev", "localhost:31234"), {
    image: "localhost:31234/educates-base-environment:4.0.0-dev",
    upstream: "http://registry.default.svc.cluster.local",
  });
});

test("an image of another registry is mirrored from that registry over HTTPS", () => {
  assert.deepEqual(mirrorOf("ghcr.io/educates/educates-base-environment:4.0.0", "localhost:31234"), {
    image: "localhost:31234/educates/educates-base-environment:4.0.0",
    upstream: "https://ghcr.io",
  });
});

test("the mirror's pull secret holds its credentials for its host as a docker config", () => {
  const secret = pullSecret({ name: "session-acceptance-mirror", namespace: "educates-installer", host: "localhost:31234", username: "session-acceptance", password: "p4ss" });
  assert.equal(secret.type, "kubernetes.io/dockerconfigjson");
  assert.deepEqual(secret.metadata, { name: "session-acceptance-mirror", namespace: "educates-installer" });
  assert.deepEqual(JSON.parse(secret.stringData[".dockerconfigjson"]), {
    auths: {
      "localhost:31234": { username: "session-acceptance", password: "p4ss", auth: "c2Vzc2lvbi1hY2NlcHRhbmNlOnA0c3M=" },
    },
  });
});

test("an image naming no registry is mirrored from Docker Hub", () => {
  assert.deepEqual(mirrorOf("debian:sid-20230502-slim", "localhost:31234"), {
    image: "localhost:31234/library/debian:sid-20230502-slim",
    upstream: "https://registry-1.docker.io",
  });
  assert.deepEqual(mirrorOf("docker.io/envoyproxy/envoy:v1.31.5", "localhost:31234"), {
    image: "localhost:31234/envoyproxy/envoy:v1.31.5",
    upstream: "https://registry-1.docker.io",
  });
});
