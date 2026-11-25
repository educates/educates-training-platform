import assert from "node:assert/strict";
import { test } from "node:test";
import { removeNamedEntry, setNamedEntry } from "./patches.mjs";

const theme = { name: "session-acceptance", source: { type: "Secret", secretRef: { name: "t", namespace: "n" } } };

test("setting an entry a list lacks appends it, guarded by the list as read", () => {
  const sessionManager = { spec: { themes: [{ name: "corporate" }] } };
  assert.deepEqual(setNamedEntry(sessionManager, "/spec/themes", theme), [
    { op: "test", path: "/spec/themes", value: [{ name: "corporate" }] },
    { op: "add", path: "/spec/themes/-", value: theme },
  ]);
});

test("setting an entry the list holds by name replaces it", () => {
  const overrides = [
    { name: "training-portal", image: "localhost:5001/educates-training-portal:dev" },
    { name: "base-environment", image: "localhost:5001/educates-base-environment:dev" },
  ];
  const mirrored = { name: "base-environment", image: "localhost:30500/educates-base-environment:dev" };
  assert.deepEqual(setNamedEntry({ spec: { images: { overrides } } }, "/spec/images/overrides", mirrored), [
    { op: "test", path: "/spec/images/overrides", value: overrides },
    { op: "replace", path: "/spec/images/overrides/1", value: mirrored },
  ]);
});

test("setting an entry where the list and its parents are missing creates them, guarded by the nearest parent there", () => {
  const clusterConfig = { spec: { mode: "Managed", ingress: { domain: "x.educates.test" } } };
  assert.deepEqual(setNamedEntry(clusterConfig, "/spec/imageRegistry/pullSecrets", { name: "mirror" }), [
    { op: "test", path: "/spec", value: { mode: "Managed", ingress: { domain: "x.educates.test" } } },
    { op: "add", path: "/spec/imageRegistry", value: { pullSecrets: [{ name: "mirror" }] } },
  ]);
});

test("removing an entry takes it out by its index, guarded by the list as read", () => {
  const themes = [{ name: "corporate" }, theme, { name: "dark" }];
  assert.deepEqual(removeNamedEntry({ spec: { themes } }, "/spec/themes", "session-acceptance"), [
    { op: "test", path: "/spec/themes", value: themes },
    { op: "remove", path: "/spec/themes/1" },
  ]);
});

test("removing a list's only entry removes the list, as a run that added the list found the resource", () => {
  assert.deepEqual(removeNamedEntry({ spec: { themes: [theme] } }, "/spec/themes", "session-acceptance"), [
    { op: "test", path: "/spec/themes", value: [theme] },
    { op: "remove", path: "/spec/themes" },
  ]);
});

test("removing a list's only entry removes the parents it leaves empty, guarded by the outermost, as a run that added them found the resource", () => {
  const clusterConfig = { spec: { mode: "Managed", imageRegistry: { pullSecrets: [{ name: "mirror" }] } } };
  assert.deepEqual(removeNamedEntry(clusterConfig, "/spec/imageRegistry/pullSecrets", "mirror"), [
    { op: "test", path: "/spec/imageRegistry", value: { pullSecrets: [{ name: "mirror" }] } },
    { op: "remove", path: "/spec/imageRegistry" },
  ]);
});

test("removing an entry that is not there changes nothing", () => {
  assert.deepEqual(removeNamedEntry({ spec: { themes: [{ name: "corporate" }] } }, "/spec/themes", "session-acceptance"), []);
  assert.deepEqual(removeNamedEntry({ spec: {} }, "/spec/imageRegistry/pullSecrets", "mirror"), []);
});
