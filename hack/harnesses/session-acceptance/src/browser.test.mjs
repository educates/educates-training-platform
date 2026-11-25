import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import fs from "node:fs";
import os from "node:os";
import path from "node:path";
import { test } from "node:test";

const BROWSER = new URL("./browser.mjs", import.meta.url).href;
const INTERRUPT = new URL("./interrupt.mjs", import.meta.url).href;

// signalledWhileLaunched starts a process that registers a restore step and
// launches a browser, here an executable that only stays running, sends it
// the signal once the browser's process runs, and resolves with its output
// and how it ended.
function signalledWhileLaunched(signal) {
  const dir = fs.mkdtempSync(path.join(os.tmpdir(), "session-acceptance-browser-"));
  const started = path.join(dir, "started");
  const chrome = path.join(dir, "chrome");
  fs.writeFileSync(chrome, `#!/bin/sh\ntouch ${JSON.stringify(started)}\nexec sleep 20\n`, { mode: 0o755 });
  const script = `
    import fs from "node:fs";
    import { launchBrowser } from ${JSON.stringify(BROWSER)};
    import { onInterrupt } from ${JSON.stringify(INTERRUPT)};
    onInterrupt(async () => {
      await new Promise((r) => setTimeout(r, 50));
      console.log("restored");
    }, { log: () => {} });
    launchBrowser({
      chrome: ${JSON.stringify(chrome)},
      cluster: { domain: "example.test", listenAddress: "127.0.0.1" },
      profileDir: ${JSON.stringify(path.join(dir, "profile"))},
      headful: false,
    }).catch(() => {});
    const poll = setInterval(() => {
      if (!fs.existsSync(${JSON.stringify(started)})) return;
      clearInterval(poll);
      setInterval(() => {}, 1000);
      console.log("launched");
    }, 20);
  `;
  return new Promise((resolve, reject) => {
    const child = spawn(process.execPath, ["--input-type=module", "-e", script], { stdio: ["ignore", "pipe", "inherit"] });
    let stdout = "";
    let sent = false;
    child.stdout.on("data", (d) => {
      stdout += d;
      if (!sent && stdout.includes("launched")) {
        sent = true;
        child.kill(signal);
      }
    });
    // A process the signal does not end fails the test instead of holding it.
    const overdue = setTimeout(() => child.kill("SIGKILL"), 10_000);
    child.on("error", reject);
    child.on("close", (code, killedBy) => {
      clearTimeout(overdue);
      fs.rmSync(dir, { recursive: true, force: true });
      resolve({ code, killedBy, stdout });
    });
  });
}

for (const [signal, status] of [
  ["SIGINT", 130],
  ["SIGTERM", 143],
  ["SIGHUP", 129],
]) {
  test(`a run sent ${signal} after its browser launched restores what it changed before it ends`, async () => {
    const r = await signalledWhileLaunched(signal);
    assert.match(r.stdout, /restored/);
    assert.equal(r.code, status);
  });
}
