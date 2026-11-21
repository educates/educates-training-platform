import assert from "node:assert/strict";
import { test } from "node:test";
import { mirrorOf, pullSecret } from "./mirror.mjs";

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
