import assert from "node:assert/strict";
import { test } from "node:test";
import { parseClusterList } from "./local-cluster-list.mjs";

test("the CLI's list of local clusters is read below the router address, by its column headings", () => {
  const rows = parseClusterList(
    [
      "Router address: 10.10.10.1",
      "",
      "NAME        STATE            NODES   KUBERNETES   DOMAIN                    CONTEXT",
      "(default)   running          3       v1.36.4      educates.test             kind-educates",
      "lookup-a    partly running   1       v1.36.4      lookup-a.educates.test    kind-educates-lookup-a",
      "test2       stopped          1       -            test2.10-10-10-1.nip.io   kind-educates-test2",
      "",
    ].join("\n"),
  );
  assert.deepEqual(rows, [
    { name: "(default)", state: "running", domain: "educates.test", context: "kind-educates" },
    { name: "lookup-a", state: "partly running", domain: "lookup-a.educates.test", context: "kind-educates-lookup-a" },
    { name: "test2", state: "stopped", domain: "test2.10-10-10-1.nip.io", context: "kind-educates-test2" },
  ]);
});

test("a list without a router address still has its clusters read", () => {
  const rows = parseClusterList(
    [
      "Router address: -",
      "",
      "NAME        STATE     NODES   KUBERNETES   DOMAIN          CONTEXT",
      "(default)   running   1       v1.36.4      educates.test   kind-educates",
    ].join("\n"),
  );
  assert.deepEqual(rows, [{ name: "(default)", state: "running", domain: "educates.test", context: "kind-educates" }]);
});

test("a list with no local cluster has no rows", () => {
  assert.deepEqual(parseClusterList("No local clusters found.\n"), []);
});
