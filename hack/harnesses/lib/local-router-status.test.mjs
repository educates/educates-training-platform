import assert from "node:assert/strict";
import { test } from "node:test";
import { routerAddressFor } from "./local-router-status.mjs";

const IMAGE = "Image:            haproxy:3.4.8-alpine@sha256:8aa6458a9d22a8364a2d2ee9ce93de0cc4a2789e3cf63f0b44adc972863724d7";

const ROUTES = [
  "NAME                 CLUSTER     TLS           STATE",
  "educates.test        (default)   passthrough   running",
  "example.test         (default)   passthrough   running",
  "tls1.educates.test   tls1        terminated    partly running",
  "extra.test           tls1        none          partly running",
  "-                    empty       passthrough   stopped",
];

// status is what 'educates local router status' prints with the router
// line router, the advice paragraph advice, if any, and the routes table.
function status({ address = "10.10.10.1", router, advice, routes = ROUTES }) {
  return [
    `Router address:   ${address}`,
    `Router:           ${router ?? `running (educates-router), publishing ports 80 and 443 on ${address}`}`,
    IMAGE,
    "",
    ...(advice ? [advice, ""] : []),
    ...routes,
    "",
  ].join("\n");
}

test("the default cluster is reached on the router address when the router sends its domain to it", () => {
  assert.equal(routerAddressFor(status({}), { name: "", domain: "educates.test" }), "10.10.10.1");
});

test("a named cluster is reached on the router address, whatever the router's TLS for its domain", () => {
  assert.equal(routerAddressFor(status({}), { name: "tls1", domain: "TLS1.educates.test" }), "10.10.10.1");
});

test("a status without a router address reaches no cluster", () => {
  assert.throws(() => routerAddressFor(status({ address: "-" }), { name: "", domain: "educates.test" }), /no router address/);
});

test("a router that does not run reaches no cluster, and the CLI's advice says why", () => {
  const advice = "The router has to be re-created, since it is not running: 'educates local router update' re-creates it.";
  assert.throws(
    () => routerAddressFor(status({ router: "stopped (educates-router)", advice }), { name: "", domain: "educates.test" }),
    (e) => /router is stopped/.test(e.message) && e.message.includes(advice),
  );
});

test("a cluster whose domain the router does not send to it is not reached", () => {
  assert.throws(
    () => routerAddressFor(status({}), { name: "test2", domain: "test2.educates.test" }),
    /sends no test2\.educates\.test to local cluster test2/,
  );
  assert.throws(
    () => routerAddressFor(status({}), { name: "tls1", domain: "educates.test" }),
    /sends no educates\.test to local cluster tls1/,
  );
});

test("a router that routes no cluster reaches none", () => {
  assert.throws(
    () => routerAddressFor(status({ routes: ["The router routes no local cluster."] }), { name: "", domain: "educates.test" }),
    /sends no educates\.test to the default local cluster/,
  );
});
