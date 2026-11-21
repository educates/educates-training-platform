import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { EventEmitter } from "node:events";
import { test } from "node:test";
import { onInterrupt, once } from "./interrupt.mjs";

const MODULE = new URL("./interrupt.mjs", import.meta.url).href;

// interrupted starts a process that registers a restore step, sends it the
// signal once it is running, and resolves with its output and how it ended.
function interrupted(signal) {
  const script = `
    import { onInterrupt } from ${JSON.stringify(MODULE)};
    onInterrupt(async () => {
      await new Promise((r) => setTimeout(r, 50));
      console.log("restored");
    }, { log: () => {} });
    setInterval(() => {}, 1000);
    console.log("running");
  `;
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ["--input-type=module", "-e", script], { stdio: ["ignore", "pipe", "inherit"] });
    let stdout = "";
    let sent = false;
    child.stdout.on("data", (d) => {
      stdout += d;
      if (!sent && stdout.includes("running")) {
        sent = true;
        child.kill(signal);
      }
    });
    child.on("error", reject);
    child.on("close", (code, killedBy) => resolve({ code, killedBy, stdout }));
  });
}

test("an interrupted run restores what it changed before it ends, with the signal's exit status", async () => {
  const r = await interrupted("SIGINT");
  assert.match(r.stdout, /restored/);
  assert.equal(r.code, 130);
});

test("a terminated run restores what it changed before it ends, with the signal's exit status", async () => {
  const r = await interrupted("SIGTERM");
  assert.match(r.stdout, /restored/);
  assert.equal(r.code, 143);
});

// fakeProcess stands for the process: it takes signals as events and
// records the exit status asked of it.
function fakeProcess() {
  const proc = new EventEmitter();
  proc.exits = [];
  proc.exit = (code) => proc.exits.push(code);
  return proc;
}

test("a second signal ends the run at once, without waiting for the restore", async () => {
  const proc = fakeProcess();
  let finish;
  let runs = 0;
  const state = onInterrupt(
    () =>
      new Promise((r) => {
        runs++;
        finish = r;
      }),
    { proc, log: () => {} },
  );
  proc.emit("SIGINT");
  await new Promise((r) => setImmediate(r));
  assert.equal(state.signal, "SIGINT");
  assert.equal(runs, 1);
  assert.deepEqual(proc.exits, []);
  proc.emit("SIGINT");
  assert.deepEqual(proc.exits, [130]);
  assert.equal(runs, 1);
  finish();
});

test("a restore that fails still ends the run with the signal's exit status", async () => {
  const proc = fakeProcess();
  const said = [];
  const state = onInterrupt(
    async () => {
      throw new Error("no cluster");
    },
    { proc, log: (m) => said.push(m) },
  );
  proc.emit("SIGTERM");
  await state.exiting;
  assert.deepEqual(proc.exits, [143]);
  assert.match(said.join("\n"), /no cluster/);
});

test("a step run once runs a single time, however many ask for it", async () => {
  let runs = 0;
  const step = once(async () => ++runs);
  assert.deepEqual(await Promise.all([step(), step()]), [1, 1]);
});
