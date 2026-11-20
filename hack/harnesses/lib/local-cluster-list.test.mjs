import assert from "node:assert/strict";
import { test } from "node:test";
import { parseClusterList } from "./local-cluster-list.mjs";

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
