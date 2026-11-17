// The terminal copy and paste step: the session's dashboard, inside the
// training portal's page, is loaded once per platform with the browser's
// reported platform set before the page loads, and in a terminal each copy
// and paste shortcut is pressed and what a workshop user would observe is
// judged against the key behavior each platform's terminals follow.

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// The platforms the dashboard is loaded under: navigator.platform as each
// reports it, which is what the dashboard tells platforms apart by, and the
// operating system its user agent names.
export const PLATFORMS = [
  { id: "windows", label: "Windows", platform: "Win32", os: "Windows NT 10.0; Win64; x64" },
  { id: "linux", label: "Linux", platform: "Linux x86_64", os: "X11; Linux x86_64" },
  { id: "macos", label: "macOS", platform: "MacIntel", os: "Macintosh; Intel Mac OS X 10_15_7" },
];

// The shortcuts, with the editing command Chrome runs for each on Windows
// and Linux when the page leaves the key's default action alone. Chrome on
// macOS binds none of them to copy or paste. Ctrl+Shift+C opens Chrome's
// inspector on Windows and Linux, a browser action no page observes, so it
// is judged by whether the page cancels the key.
export const SHORTCUTS = Object.fromEntries(
  [
    { id: "ctrl-c", label: "Ctrl+C", modifiers: ["Control"], key: "c", command: "copy", event: "copy" },
    { id: "ctrl-v", label: "Ctrl+V", modifiers: ["Control"], key: "v", command: "paste", event: "paste" },
    { id: "ctrl-shift-c", label: "Ctrl+Shift+C", modifiers: ["Control", "Shift"], key: "c", command: null, event: null },
    { id: "ctrl-shift-v", label: "Ctrl+Shift+V", modifiers: ["Control", "Shift"], key: "v", command: "pasteAndMatchStyle", event: "paste" },
  ].map((s) => [s.id, s]),
);

// The cases each platform runs, in order. A copy case presses its key with
// text the terminal shows selected, or with nothing selected; a paste case
// presses it with known text on the clipboard; the interrupt case presses
// it with nothing selected while sleep runs.
export const CASES = [
  { id: "ctrl-c-selection", shortcut: "ctrl-c", label: "Ctrl+C, text selected", kind: "copy", selection: true },
  { id: "ctrl-c-running", shortcut: "ctrl-c", label: "Ctrl+C, nothing selected, sleep running", kind: "interrupt" },
  { id: "ctrl-v", shortcut: "ctrl-v", label: "Ctrl+V", kind: "paste" },
  { id: "ctrl-shift-c-selection", shortcut: "ctrl-shift-c", label: "Ctrl+Shift+C, text selected", kind: "copy", selection: true },
  { id: "ctrl-shift-c-none", shortcut: "ctrl-shift-c", label: "Ctrl+Shift+C, nothing selected", kind: "copy", selection: false },
  { id: "ctrl-shift-v", shortcut: "ctrl-shift-v", label: "Ctrl+Shift+V", kind: "paste" },
];

// The key behavior table, as what a workshop user observes in each case:
// copied, what a copy puts on the clipboard, the selected text or nothing;
// shell, what reaches the shell: nothing, ^C, ^V or the pasted text;
// selection, whether the selection is kept or cleared; cancelled, whether
// the key's default action is cancelled; interrupted, whether the running
// command is interrupted. A field left out is reported, not judged.
const INTERRUPTS = { copied: "nothing", interrupted: true };
const PASTES = { shell: "pasted" };
const COPIES_AND_KEEPS = { copied: "selection", shell: "nothing", selection: "kept", cancelled: true };
const DOES_NOTHING = { copied: "nothing", shell: "nothing", cancelled: true };
const LINUX_COPY_PASTE = {
  "ctrl-shift-c-selection": COPIES_AND_KEEPS,
  "ctrl-shift-c-none": DOES_NOTHING,
  "ctrl-shift-v": PASTES,
};
export const EXPECTED = {
  windows: {
    "ctrl-c-selection": { copied: "selection", shell: "nothing", selection: "cleared" },
    "ctrl-c-running": INTERRUPTS,
    "ctrl-v": PASTES,
    ...LINUX_COPY_PASTE,
  },
  linux: {
    "ctrl-c-selection": { copied: "nothing", shell: "^C" },
    "ctrl-c-running": INTERRUPTS,
    "ctrl-v": { shell: "^V" },
    ...LINUX_COPY_PASTE,
  },
  macos: {
    "ctrl-c-selection": { copied: "nothing", shell: "^C" },
    "ctrl-c-running": INTERRUPTS,
    "ctrl-v": { shell: "^V" },
    "ctrl-shift-c-selection": { copied: "nothing", shell: "nothing", cancelled: false },
    "ctrl-shift-c-none": { copied: "nothing", shell: "nothing", cancelled: false },
    "ctrl-shift-v": { shell: "nothing" },
  },
};

// The probe the cases run in the terminal, written into the workshop
// container. 'read' clears the screen, shows the text given, and prints in
// hex every byte the terminal delivers until three seconds pass without
// one, with the terminal raw, so ^C and ^V arrive as bytes. 'sleep' clears
// the screen and sleeps ten seconds, saying on a line of its own, after the
// ^C the terminal echoes, whether an interrupt ended it.
export const PROBE_PATH = "/tmp/session-acceptance-keys";
export const PROBE_SCRIPT = `#!/bin/bash
case "$1" in
read)
    printf '\\033[H\\033[2J%s\\n' "$3"
    saved=$(stty -g)
    stty raw -echo min 0 time 30
    printf 'keys-ready-%s\\r\\n' "$2"
    bytes=$(od -An -tx1 -v | tr -d ' \\n')
    stty "$saved"
    printf 'keys-got-%s:%s.\\n' "$2" "$bytes"
    ;;
sleep)
    printf '\\033[H\\033[2J'
    trap 'printf "\\nkeys-interrupted-%s\\n" "$2"; exit 130' INT
    printf 'keys-sleeping-%s\\n' "$2"
    sleep 10
    printf 'keys-slept-%s\\n' "$2"
    ;;
esac
`;

const TERMINAL = "#terminal-1";

// browserAction says what the browser itself does for a shortcut under a
// platform, and how the step brings it about. Chrome's own editing
// commands follow the operating system it runs on, not the reported
// platform, so where the host's Chrome doesn't run the command the faked
// platform's Chrome would, the step sends the key with that command
// attached, which Chrome runs only when the page leaves the key's default
// action alone, as it does its own.
export function browserAction(platform, shortcut, hostNative) {
  const command = platform.id === "macos" ? null : shortcut.command;
  if (command) {
    return hostNative
      ? { mode: "native", command, text: `native ${command}` }
      : { mode: "simulated", command, text: `simulated ${command}` };
  }
  if (hostNative) {
    return { mode: "host", command: null, text: `host's ${shortcut.command}, which ${platform.label} lacks` };
  }
  if (shortcut.id === "ctrl-shift-c" && platform.id !== "macos") {
    return { mode: "not run", command: null, text: "inspector not run" };
  }
  return { mode: "none", command: null, text: "none" };
}

export function userAgent(platform, browserVersion) {
  const version = browserVersion.replace(/^[A-Za-z]*Chrome\//, "");
  return `Mozilla/5.0 (${platform.os}) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${version} Safari/537.36`;
}

// classifyShell names what reached the shell: nothing, ^C, ^V, the pasted
// text with or without bracketed paste around it, or the bytes themselves.
export function classifyShell(bytes, pasted) {
  if (bytes === "") return "nothing";
  if (bytes === "\x03") return "^C";
  if (bytes === "\x16") return "^V";
  const unbracketed = bytes.replace(/^\x1b\[200~/, "").replace(/\x1b\[201~$/, "");
  if (pasted && unbracketed === pasted) return "pasted";
  return JSON.stringify(bytes);
}

function hexToText(hex) {
  return Buffer.from(hex, "hex").toString("latin1");
}

// judge compares what a case observed with what the table says, and
// returns the differences.
export function judge(expected, observed) {
  const differences = [];
  if (expected.copied !== undefined) {
    const copied = observed.copied === "" ? "nothing" : observed.copied === observed.selectedText ? "selection" : "other";
    if (copied !== expected.copied) differences.push("copied");
  }
  if (expected.shell !== undefined && observed.shell !== expected.shell) differences.push("shell");
  if (expected.selection !== undefined && observed.selection !== expected.selection) differences.push("selection");
  if (expected.cancelled !== undefined && observed.cancelled !== expected.cancelled) differences.push("cancelled");
  if (expected.interrupted !== undefined && observed.interrupted !== expected.interrupted) differences.push("interrupted");
  return differences;
}

export function describeExpected(e) {
  const parts = [];
  if (e.interrupted) parts.push("interrupts sleep");
  if (e.copied === "selection") parts.push("copies the selection");
  if (e.copied === "nothing") parts.push("copies nothing");
  if (e.selection) parts.push(`${e.selection === "kept" ? "keeps" : "clears"} the selection`);
  if (e.shell === "nothing") parts.push("nothing reaches the shell");
  if (e.shell === "^C" || e.shell === "^V") parts.push(`${e.shell} reaches the shell`);
  if (e.shell === "pasted") parts.push("pastes");
  if (e.cancelled !== undefined) parts.push(e.cancelled ? "cancels the key" : "leaves the key to the browser");
  return parts.join(", ");
}

export function describeObserved(o) {
  if (o.error) return `not observed: ${o.error}`;
  const parts = [];
  if (o.interrupted !== undefined) parts.push(o.interrupted ? "interrupted sleep" : "sleep ran on");
  if (o.copied !== undefined) parts.push(o.copied === "" ? "no copy" : `copied ${JSON.stringify(o.copied)}`);
  if (o.pasteEvent !== undefined) parts.push(`paste event ${o.pasteEvent}`);
  if (o.selection && o.selection !== "none") parts.push(`selection ${o.selection}`);
  if (o.shell !== undefined) parts.push(o.shell === "pasted" ? "pasted text reached the shell" : `shell got ${o.shell}`);
  if (o.cancelled !== undefined) parts.push(o.cancelled ? "cancelled" : "not cancelled");
  return parts.join(", ");
}

// The observer installed in the dashboard's frame, which leaves every
// event to behave as before. It keeps the keydown events, to read whether
// each was cancelled; notes, by passing each copy event's setData calls
// on, what the event put on the clipboard; and notes what each paste
// event carried.
function installObserver(frame) {
  return frame.evaluate(() => {
    const observer = { keydowns: [], copies: [], pastes: [] };
    window.sessionAcceptanceKeys = observer;
    window.addEventListener("keydown", (e) => observer.keydowns.push(e), true);
    window.addEventListener(
      "copy",
      (e) => {
        const el = document.activeElement;
        const record = {
          event: e,
          data: null,
          // What the browser copies when no listener cancels the event.
          selected:
            el && typeof el.selectionStart === "number"
              ? el.value.slice(el.selectionStart, el.selectionEnd)
              : String(document.getSelection() ?? ""),
        };
        const data = e.clipboardData;
        if (data) {
          const setData = data.setData.bind(data);
          data.setData = (type, value) => {
            if (/^text(\/plain)?$/i.test(type)) record.data = String(value);
            return setData(type, value);
          };
        }
        observer.copies.push(record);
      },
      true,
    );
    window.addEventListener("paste", (e) => observer.pastes.push(e.clipboardData?.getData("text/plain") ?? null), true);
  });
}

function resetObserver(frame) {
  return frame.evaluate(() => {
    const o = window.sessionAcceptanceKeys;
    o.keydowns = [];
    o.copies = [];
    o.pastes = [];
  });
}

function readObserver(frame, key) {
  return frame.evaluate((k) => {
    const o = window.sessionAcceptanceKeys;
    const keydown = o.keydowns.filter((e) => e.key.toLowerCase() === k).pop();
    return {
      keydown: !!keydown,
      cancelled: keydown ? keydown.defaultPrevented : null,
      copies: o.copies.map((c) => (c.event.defaultPrevented ? (c.data ?? "") : c.selected)),
      pastes: o.pastes,
    };
  }, key);
}

function terminalRows(frame) {
  return frame
    .evaluate((t) => [...document.querySelectorAll(`${t} .xterm-rows > div`)].map((r) => r.textContent.replace(/\s+$/, "")), TERMINAL)
    .catch(() => []);
}

async function waitForRow(frame, test, timeoutMs, what) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    for (const row of await terminalRows(frame)) {
      const m = test(row);
      if (m) return m;
    }
    if (Date.now() > deadline) throw new Error(`the terminal never showed ${what}`);
    await sleep(250);
  }
}

// hasSelection reports whether the terminal shows text highlighted as
// selected. The terminal leaves empty highlight boxes behind after a
// click, so only a box with an area counts.
function hasSelection(frame) {
  return frame.evaluate(
    (t) =>
      [...document.querySelectorAll(`${t} .xterm-selection > div`)].some((box) => {
        const r = box.getBoundingClientRect();
        return r.width > 0 && r.height > 0;
      }),
    TERMINAL,
  );
}

// focusTerminal clicks the terminal's last row, which focuses it and
// clears any selection, as a click does for a user.
async function focusTerminal(page, frame) {
  const screen = await frame.$(`${TERMINAL} .xterm-screen`);
  if (!screen) throw new Error("the dashboard has no terminal 1");
  const box = await screen.boundingBox();
  await page.mouse.click(box.x + box.width / 2, box.y + box.height - 6);
  await sleep(300);
  const focus = await frame.evaluate(
    (t) => ({
      document: document.hasFocus(),
      terminal: !!document.activeElement?.closest(t) && document.activeElement.tagName === "TEXTAREA",
      element: `${document.activeElement?.tagName.toLowerCase()}.${document.activeElement?.className}`,
    }),
    TERMINAL,
  );
  if (!focus.document || !focus.terminal) {
    throw new Error(`clicking terminal 1 did not focus it: the frame ${focus.document ? "has" : "lacks"} focus, on ${focus.element}`);
  }
}

// connectTerminal waits until what is typed in the terminal reaches its
// shell: the dashboard may still cover the terminal at first, and drops
// input until the terminal has caught up with its session.
async function connectTerminal(page, frame, marker) {
  const deadline = Date.now() + 60_000;
  for (;;) {
    try {
      await focusTerminal(page, frame);
      await typeLine(page, `clear; echo ${marker}`);
      return await waitForRow(frame, (r) => r === marker, 5000, `${marker} after typing`);
    } catch (e) {
      if (Date.now() > deadline) throw new Error(`typing in terminal 1 never reached its shell: ${e.message}`);
      await sleep(2000);
    }
  }
}

// selectWord double-clicks text the terminal shows, which selects the word
// it is, as a user selects it.
async function selectWord(page, frame, text) {
  for (const row of await frame.$$(`${TERMINAL} .xterm-rows > div`)) {
    const at = await row.evaluate((el, t) => {
      const index = el.textContent.indexOf(t);
      if (index < 0) return null;
      const walker = document.createTreeWalker(el, NodeFilter.SHOW_TEXT);
      const target = index + Math.floor(t.length / 2);
      let pos = 0;
      for (let node = walker.nextNode(); node; node = walker.nextNode()) {
        const length = node.textContent.length;
        if (pos + length > target) {
          const range = document.createRange();
          range.setStart(node, target - pos);
          range.setEnd(node, target - pos + 1);
          const r = range.getBoundingClientRect();
          const rowRect = el.getBoundingClientRect();
          return { x: r.left - rowRect.left + r.width / 2, y: rowRect.height / 2 };
        }
        pos += length;
      }
      return null;
    }, text);
    if (at) {
      const box = await row.boundingBox();
      await page.mouse.click(box.x + at.x, box.y + at.y, { count: 2 });
      await sleep(300);
      return;
    }
  }
  throw new Error(`the terminal does not show ${text} to select`);
}

// setClipboard puts text on the clipboard from the portal's own page, as a
// copy in another application would, leaving the dashboard's frame alone.
async function setClipboard(page, text) {
  const copied = await page.evaluate((t) => {
    const area = document.createElement("textarea");
    area.value = t;
    area.style.position = "fixed";
    area.style.left = "-9999px";
    document.body.appendChild(area);
    area.focus();
    area.select();
    const ok = document.execCommand("copy");
    area.remove();
    return ok;
  }, text);
  if (!copied) throw new Error("could not put the text to paste on the clipboard");
}

async function pressShortcut(page, shortcut, command) {
  for (const m of shortcut.modifiers) await page.keyboard.down(m);
  await page.keyboard.press(shortcut.key, command ? { commands: [command] } : {});
  for (const m of [...shortcut.modifiers].reverse()) await page.keyboard.up(m);
}

async function typeLine(page, line) {
  await page.keyboard.type(line);
  await page.keyboard.press("Enter");
}

// hostBindings finds which shortcuts the host's Chrome turns into its own
// copy or paste under a platform: in a page of its own reporting that
// platform, it presses each in a textarea with text selected and watches
// for the copy or paste event.
export async function hostBindings(newPage, platform, agent) {
  const page = await newPage();
  try {
    await page.setUserAgent({ userAgent: agent, platform: platform.platform });
    await page.setContent(
      `<textarea id="probe"></textarea><script>
        window.seen = [];
        for (const t of ["copy", "paste"]) document.addEventListener(t, () => window.seen.push(t), true);
      </script>`,
    );
    const native = {};
    for (const shortcut of Object.values(SHORTCUTS)) {
      if (!shortcut.event) continue;
      await page.evaluate(() => {
        const area = document.getElementById("probe");
        area.value = "probe";
        area.focus();
        area.select();
        window.seen = [];
      });
      await pressShortcut(page, shortcut, null);
      await sleep(200);
      native[shortcut.id] = (await page.evaluate(() => window.seen)).includes(shortcut.event);
    }
    return native;
  } finally {
    await page.close().catch(() => {});
  }
}

// dashboardFrame waits for the portal's page to show the session's
// dashboard with terminal 1 connected, and returns the dashboard's frame.
async function dashboardFrame(page, host) {
  const deadline = Date.now() + 3 * 60_000;
  while (Date.now() < deadline) {
    for (const frame of page.frames()) {
      let hostname = null;
      try {
        hostname = new URL(frame.url()).hostname;
      } catch {
        continue;
      }
      if (hostname !== host) continue;
      const connected = await frame
        .evaluate((t) => [...document.querySelectorAll(`${t} .xterm-rows > div`)].some((r) => r.textContent.trim() !== ""), TERMINAL)
        .catch(() => false);
      if (connected) return frame;
    }
    await sleep(1000);
  }
  throw new Error("the portal's page never showed the dashboard with terminal 1 connected");
}

// runCase runs one case in the terminal and returns what it observed.
async function runCase(page, frame, platform, testCase, n, action) {
  const shortcut = SHORTCUTS[testCase.shortcut];
  const id = `${platform.id}-${n}`;
  const suffix = Math.random().toString(36).slice(2, 6);
  const observed = {};
  // The line the probe ends with: the bytes it read, or how its sleep ended.
  const probeEnd = (r) => {
    if (r === `keys-interrupted-${id}`) return { interrupted: true };
    if (r === `keys-slept-${id}`) return { interrupted: false };
    const got = r.match(new RegExp(`^keys-got-${id}:([0-9a-f]*)\\.$`));
    return got ? { bytes: hexToText(got[1]) } : null;
  };

  if (testCase.kind === "paste") {
    observed.pasteText = `pasteme-${id}-${suffix}`;
    await setClipboard(page, observed.pasteText);
  }
  await focusTerminal(page, frame);

  let started = false;
  try {
    if (testCase.kind === "interrupt") {
      await typeLine(page, `bash ${PROBE_PATH} sleep ${id}`);
      started = true;
      await waitForRow(frame, (r) => r === `keys-sleeping-${id}`, 30_000, "the probe sleeping");
    } else {
      const shown = `copyme-${id}-${suffix}`;
      await typeLine(page, `bash ${PROBE_PATH} read ${id} ${shown}`);
      started = true;
      await waitForRow(frame, (r) => r === `keys-ready-${id}`, 30_000, "the probe ready");
      if (testCase.selection) {
        await selectWord(page, frame, shown);
        observed.selectedText = shown;
      }
    }
    const selectedBefore = await hasSelection(frame);
    if (testCase.selection && !selectedBefore) throw new Error("double-clicking the text selected nothing");
    if (!testCase.selection && selectedBefore) throw new Error("the terminal kept a selection after a click");

    await resetObserver(frame);
    await pressShortcut(page, shortcut, action.mode === "simulated" ? action.command : null);
    await sleep(700);
    const seen = await readObserver(frame, shortcut.key);
    if (!seen.keydown) throw new Error(`the dashboard's frame never saw the ${shortcut.label} keydown`);
    observed.cancelled = seen.cancelled;
    if (testCase.kind === "paste") {
      // What the page's paste event carried, which tells a paste the
      // terminal dropped from one the browser never made.
      const text = seen.pastes.pop();
      observed.pasteEvent = text === undefined ? "none" : text === observed.pasteText ? "carried the text" : `carried ${JSON.stringify(text)}`;
    } else {
      observed.copied = seen.copies.filter((c) => c !== "").pop() ?? "";
    }
    observed.selection = selectedBefore ? ((await hasSelection(frame)) ? "kept" : "cleared") : "none";

    const end = await waitForRow(frame, probeEnd, 20_000, "the probe's last line");
    started = false;
    if (testCase.kind === "interrupt") {
      observed.interrupted = end.interrupted;
    } else {
      observed.bytes = end.bytes;
      observed.shell = classifyShell(end.bytes, observed.pasteText);
    }
    return observed;
  } finally {
    // A case that failed leaves its probe to end before the next one types.
    if (started) await waitForRow(frame, probeEnd, 20_000, "the probe's last line").catch(() => {});
  }
}

// runCopyPaste runs every case under every platform in the session's
// dashboard inside the portal's page, and returns the results by platform.
// newPage opens a page in the fixture's browser context, which runs Chrome
// browserVersion.
export async function runCopyPaste({ newPage, browserVersion, cluster, environment, pod, sessionPageUrl, sessionHost, log, screenshot }) {
  const install = await cluster.kubectl(
    ["-n", environment, "exec", "-i", pod, "-c", "workshop", "--", "sh", "-c", `cat > ${PROBE_PATH} && chmod +x ${PROBE_PATH}`],
    { input: PROBE_SCRIPT },
  );
  if (install.code !== 0) throw new Error(`writing the probe into the workshop container failed: ${install.stderr.trim()}`);

  const platforms = [];
  for (const platform of PLATFORMS) {
    const agent = userAgent(platform, browserVersion);
    const result = { platform: platform.id, label: platform.label, reported: null, cases: [], error: null };
    platforms.push(result);
    let page = null;
    try {
      const native = await hostBindings(newPage, platform, agent);
      result.hostBindings = native;
      page = await newPage();
      // Only the browser's front page has focus, which keys need.
      await page.bringToFront();
      await page.setUserAgent({ userAgent: agent, platform: platform.platform });
      await page.goto(sessionPageUrl, { waitUntil: "domcontentloaded", timeout: 120_000 });
      const frame = await dashboardFrame(page, sessionHost);
      result.reported = await frame.evaluate(() => navigator.platform);
      log("copy-paste-platform", { platform: platform.id, reported: result.reported, hostBindings: native });
      if (result.reported !== platform.platform) {
        throw new Error(`the dashboard's frame reports navigator.platform ${result.reported}, not ${platform.platform}`);
      }
      await connectTerminal(page, frame, `keys-connected-${platform.id}`);
      await installObserver(frame);
      for (const [index, testCase] of CASES.entries()) {
        const shortcut = SHORTCUTS[testCase.shortcut];
        const action = browserAction(platform, shortcut, !!native[shortcut.id]);
        const expected = EXPECTED[platform.id][testCase.id];
        const entry = { case: testCase.id, label: testCase.label, expected, action };
        try {
          entry.observed = await runCase(page, frame, platform, testCase, index + 1, action);
          entry.differences = judge(expected, entry.observed);
        } catch (e) {
          entry.observed = { error: String(e.message || e).slice(0, 300) };
          entry.differences = ["not observed"];
          await screenshot(page, `copy-paste-${platform.id}-${testCase.id}`);
        }
        entry.matches = entry.differences.length === 0;
        log("copy-paste-case", { platform: platform.id, ...entry, observed: { ...entry.observed, bytes: undefined } });
        result.cases.push(entry);
      }
      await screenshot(page, `copy-paste-${platform.id}`);
    } catch (e) {
      result.error = String(e.message || e).slice(0, 300);
      log("copy-paste-failed", { platform: platform.id, error: String(e.stack || e).slice(0, 2000) });
      if (page) await screenshot(page, `copy-paste-${platform.id}-failed`);
    } finally {
      if (page) await page.close().catch(() => {});
    }
  }
  return { platforms };
}

// copyPasteProblems lists, for the fixture's problems, every case that
// didn't match the table and every platform that couldn't run.
export function copyPasteProblems(copyPaste) {
  const problems = [];
  for (const p of copyPaste.platforms) {
    if (p.error) problems.push(`copy and paste, ${p.label}: ${p.error}`);
    for (const c of p.cases) {
      if (!c.matches) {
        problems.push(`copy and paste, ${p.label}, ${c.label}: the table says ${describeExpected(c.expected)}; observed ${describeObserved(c.observed)}`);
      }
    }
  }
  return problems;
}
