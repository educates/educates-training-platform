import assert from "node:assert/strict";
import { test } from "node:test";
import { judgeAlternation, judgeClusterSessions, judgeReturning } from "./verdict.mjs";

const CLUSTERS = ["lookup-a", "lookup-b"];

function placed(index, cluster, session = `env-w01-${index}`) {
  return {
    index,
    user: `user-${index}`,
    status: 200,
    clusterName: cluster,
    portalName: "portal",
    environmentName: "env-w01",
    sessionName: session,
  };
}

test("sessions split across the clusters in every pair of requests alternate", () => {
  const verdict = judgeAlternation(
    [placed(1, "lookup-b"), placed(2, "lookup-a"), placed(3, "lookup-a"), placed(4, "lookup-b")],
    CLUSTERS,
  );
  assert.equal(verdict.passed, true, verdict.detail);
});

test("every session on one cluster, as #975 placed them, does not alternate", () => {
  const verdict = judgeAlternation(
    [placed(1, "lookup-a"), placed(2, "lookup-a"), placed(3, "lookup-a"), placed(4, "lookup-a")],
    CLUSTERS,
  );
  assert.equal(verdict.passed, false);
  assert.match(verdict.detail, /requests 1 and 2 both went to lookup-a/);
});

test("a request the Lookup Service refused fails the series", () => {
  const refused = { index: 2, user: "user-2", status: 503, body: "Workshop not available" };
  const verdict = judgeAlternation([placed(1, "lookup-a"), refused], CLUSTERS);
  assert.equal(verdict.passed, false);
  assert.match(verdict.detail, /request 2 for user-2 answered 503: Workshop not available/);
});

test("a session on a cluster the run did not register fails the series", () => {
  const verdict = judgeAlternation([placed(1, "lookup-a"), placed(2, "elsewhere")], CLUSTERS);
  assert.equal(verdict.passed, false);
  assert.match(verdict.detail, /request 2 got a session on elsewhere/);
});

test("a new user given a session another request got fails the series", () => {
  const verdict = judgeAlternation(
    [placed(1, "lookup-a", "env-w01-abcd"), placed(2, "lookup-b"), placed(3, "lookup-a", "env-w01-abcd"), placed(4, "lookup-b")],
    CLUSTERS,
  );
  assert.equal(verdict.passed, false);
  assert.match(verdict.detail, /request 3 got session env-w01-abcd on lookup-a, which request 1 got/);
});

test("a later pair that lands on one cluster fails, even with equal totals", () => {
  const verdict = judgeAlternation(
    [placed(1, "lookup-a"), placed(2, "lookup-b"), placed(3, "lookup-b"), placed(4, "lookup-b"), placed(5, "lookup-a"), placed(6, "lookup-a")],
    CLUSTERS,
  );
  assert.equal(verdict.passed, false);
  assert.match(verdict.detail, /requests 3 and 4 both went to lookup-b/);
});

test("an alternating series says how its sessions split", () => {
  const verdict = judgeAlternation(
    [placed(1, "lookup-b"), placed(2, "lookup-a"), placed(3, "lookup-a"), placed(4, "lookup-b")],
    CLUSTERS,
  );
  assert.equal(verdict.detail, "4 new sessions, 2 on each cluster, one of each pair on each: lookup-b lookup-a | lookup-a lookup-b");
});

// held is a WorkshopSession as the cluster holds it.
function held(name, user, phase = "Allocated") {
  return { name, user, phase };
}

const SERIES = [placed(1, "lookup-b", "b-1"), placed(2, "lookup-a", "a-2")];

test("clusters holding exactly the sessions placed, each in use by its user, pass", () => {
  const verdict = judgeClusterSessions(SERIES, {
    "lookup-a": [held("a-2", "user-2")],
    "lookup-b": [held("b-1", "user-1")],
  });
  assert.equal(verdict.passed, true, verdict.detail);
  assert.equal(verdict.detail, "the clusters hold the 2 sessions placed, each Allocated to its user: lookup-a 1, lookup-b 1");
});

test("a placed session its cluster does not hold fails", () => {
  const verdict = judgeClusterSessions(SERIES, { "lookup-a": [held("a-2", "user-2")], "lookup-b": [] });
  assert.equal(verdict.passed, false);
  assert.equal(verdict.detail, "lookup-b holds no session b-1, which request 1 got");
});

test("a placed session in another phase fails, naming the phase", () => {
  const verdict = judgeClusterSessions(SERIES, {
    "lookup-a": [held("a-2", "user-2")],
    "lookup-b": [held("b-1", "user-1", "Pending")],
  });
  assert.equal(verdict.passed, false);
  assert.equal(verdict.detail, "session b-1 on lookup-b is in phase Pending, not Allocated");
});

test("a placed session held by another user fails", () => {
  const verdict = judgeClusterSessions(SERIES, {
    "lookup-a": [held("a-2", "user-9")],
    "lookup-b": [held("b-1", "user-1")],
  });
  assert.equal(verdict.passed, false);
  assert.equal(verdict.detail, "session a-2 on lookup-a belongs to user-9, not user-2");
});

test("a session no request got fails", () => {
  const verdict = judgeClusterSessions(SERIES, {
    "lookup-a": [held("a-2", "user-2"), held("a-3", "user-1")],
    "lookup-b": [held("b-1", "user-1")],
  });
  assert.equal(verdict.passed, false);
  assert.equal(verdict.detail, "lookup-a holds session a-3 of user-1, which no request got");
});

// returned is the answer to user-1 asking again, as request 7.
function returned(cluster, session) {
  return { ...placed(7, cluster, session), user: "user-1" };
}

test("a returning user given their session back passes", () => {
  const verdict = judgeReturning(placed(1, "lookup-b", "env-w01-abcd"), returned("lookup-b", "env-w01-abcd"));
  assert.equal(verdict.passed, true, verdict.detail);
  assert.equal(verdict.detail, "user-1 got session env-w01-abcd on lookup-b back");
});

test("a returning user given another session fails", () => {
  const verdict = judgeReturning(placed(1, "lookup-b", "env-w01-abcd"), returned("lookup-a", "env-w01-wxyz"));
  assert.equal(verdict.passed, false);
  assert.equal(verdict.detail, "user-1 got session env-w01-wxyz on lookup-a, not their session env-w01-abcd on lookup-b");
});

test("a returning user the Lookup Service refused fails", () => {
  const again = { index: 7, user: "user-1", status: 503, body: "Workshop not available" };
  const verdict = judgeReturning(placed(1, "lookup-b", "env-w01-abcd"), again);
  assert.equal(verdict.passed, false);
  assert.equal(verdict.detail, "user-1's return answered 503: Workshop not available");
});
