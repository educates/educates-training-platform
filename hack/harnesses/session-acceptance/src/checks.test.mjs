import assert from "node:assert/strict";
import { test } from "node:test";
import { imagePullFailure } from "./checks.mjs";

const IMAGE = "localhost:31234/educates-base-environment:dev";
const UNAUTHORIZED =
  `failed to pull and unpack image "${IMAGE}": failed to resolve image: ` +
  `unexpected status from HEAD request to http://localhost:31234/v2/educates-base-environment/manifests/dev: 401 Unauthorized`;

function pod({ init = [], containers = [] }) {
  return { status: { initContainerStatuses: init, containerStatuses: containers } };
}

test("a pod whose containers are starting or running has no image pull failure", () => {
  assert.equal(imagePullFailure(pod({})), null);
  assert.equal(
    imagePullFailure(
      pod({
        containers: [
          { name: "workshop", image: IMAGE, state: { waiting: { reason: "ContainerCreating" } } },
          { name: "registry", image: "registry:3", state: { running: {} } },
        ],
      }),
    ),
    null,
  );
});

test("a container waiting on a refused pull is the failure, with the kubelet's message", () => {
  const failure = imagePullFailure(
    pod({
      containers: [
        { name: "registry", image: "registry:3", state: { running: {} } },
        { name: "workshop", image: IMAGE, state: { waiting: { reason: "ErrImagePull", message: UNAUTHORIZED } } },
      ],
    }),
  );
  assert.deepEqual(failure, { container: "workshop", image: IMAGE, reason: "ErrImagePull", message: UNAUTHORIZED });
});
