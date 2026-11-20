import assert from "node:assert/strict";
import { test } from "node:test";
import { SuspensionWatch } from "./suspension.mjs";

const START = Date.parse("2026-11-19T13:06:00Z");
const SECOND = 1000;
const MINUTE = 60 * SECOND;

test("a harness that ran all along was never suspended", () => {
  const watch = new SuspensionWatch();
  for (let t = START; t <= START + 15 * MINUTE; t += 5 * SECOND) watch.note(t);
  assert.deepEqual(watch.suspensions, []);
});

test("a gap between two moments the harness ran is a suspension, as when the host slept", () => {
  const watch = new SuspensionWatch();
  watch.note(START);
  watch.note(START + 5 * SECOND);
  const ended = watch.note(START + 5 * SECOND + 1006 * SECOND);
  watch.note(START + 1016 * SECOND);
  const expected = { from: "2026-11-19T13:06:05.000Z", to: "2026-11-19T13:22:51.000Z", seconds: 1006 };
  assert.deepEqual(ended, expected);
  assert.deepEqual(watch.suspensions, [expected]);
});

test("each sleep of a host that keeps waking briefly is a suspension of its own", () => {
  const watch = new SuspensionWatch();
  watch.note(START);
  watch.note(START + 17 * MINUTE);
  watch.note(START + 17 * MINUTE + 2 * SECOND);
  watch.note(START + 34 * MINUTE);
  assert.deepEqual(
    watch.suspensions.map((s) => s.seconds),
    [17 * 60, 17 * 60 - 2],
  );
});

test("a timer that runs late, as on a busy host, is not a suspension", () => {
  const watch = new SuspensionWatch();
  watch.note(START);
  assert.equal(watch.note(START + 45 * SECOND), undefined);
  assert.deepEqual(watch.suspensions, []);
});
