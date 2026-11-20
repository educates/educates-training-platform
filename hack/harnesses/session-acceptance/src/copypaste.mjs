// The terminal copy and paste step: the session's dashboard, inside the
// training portal's page, is loaded once per platform with the browser's
// reported platform set before the page loads, and in a terminal each copy
// and paste shortcut is pressed and what a workshop user would observe is
// judged against the key behaviour each platform's terminals follow.

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// The platforms the dashboard is loaded under: navigator.platform as each
// reports it, which is what the dashboard tells platforms apart by, the
// operating system its user agent names, and whether Chrome on it binds the
// Ctrl shortcuts of its own: its copy and paste commands and its inspector.
const PLATFORMS = [
  { id: "windows", label: "Windows", platform: "Win32", os: "Windows NT 10.0; Win64; x64", chromeBindsCtrlShortcuts: true },
  { id: "linux", label: "Linux", platform: "Linux x86_64", os: "X11; Linux x86_64", chromeBindsCtrlShortcuts: true },
  { id: "macos", label: "macOS", platform: "MacIntel", os: "Macintosh; Intel Mac OS X 10_15_7", chromeBindsCtrlShortcuts: false },
];

// The shortcuts, with the editing command Chrome runs for each where it
// binds the Ctrl shortcuts, when the page leaves the key's default action
// alone, and whether Chrome opens its inspector for it there instead.
// Opening the inspector is a browser action no page observes, so such a
// shortcut is judged by whether the page cancels the key.
export const SHORTCUTS = Object.fromEntries(
  [
    { id: "ctrl-c", label: "Ctrl+C", modifiers: ["Control"], key: "c", command: "copy", event: "copy", opensInspector: false },
    { id: "ctrl-v", label: "Ctrl+V", modifiers: ["Control"], key: "v", command: "paste", event: "paste", opensInspector: false },
    { id: "ctrl-shift-c", label: "Ctrl+Shift+C", modifiers: ["Control", "Shift"], key: "c", command: null, event: null, opensInspector: true },
    { id: "ctrl-shift-v", label: "Ctrl+Shift+V", modifiers: ["Control", "Shift"], key: "v", command: "pasteAndMatchStyle", event: "paste", opensInspector: false },
  ].map((s) => [s.id, s]),
);

// The cases each platform runs, in order. A copy case presses its key with
// text the terminal shows selected, or with nothing selected, and a case
// that presses it again does so once the first press is over, with nothing
// else between; a paste case presses it with known text on the clipboard;
// the interrupt case presses it with nothing selected while sleep runs.
export const CASES = [
  { id: "ctrl-c-selection", shortcut: "ctrl-c", label: "Ctrl+C, text selected, then again", kind: "copy", selection: true, pressAgain: true },
  { id: "ctrl-c-running", shortcut: "ctrl-c", label: "Ctrl+C, nothing selected, sleep running", kind: "interrupt" },
  { id: "ctrl-v", shortcut: "ctrl-v", label: "Ctrl+V", kind: "paste" },
  { id: "ctrl-shift-c-selection", shortcut: "ctrl-shift-c", label: "Ctrl+Shift+C, text selected", kind: "copy", selection: true },
  { id: "ctrl-shift-c-none", shortcut: "ctrl-shift-c", label: "Ctrl+Shift+C, nothing selected", kind: "copy", selection: false },
  { id: "ctrl-shift-v", shortcut: "ctrl-shift-v", label: "Ctrl+Shift+V", kind: "paste" },
];

// The key behaviour table, as what a workshop user observes in each case,
// in the fields OBSERVATIONS describes. A field left out is reported, not
// judged.
const INTERRUPTS = { copied: "nothing", interrupted: true };
const PASTES = { shell: "pasted" };
const COPIES_AND_KEEPS = { copied: "selection", shell: "nothing", selection: "kept", cancelled: true };
const DOES_NOTHING = { copied: "nothing", shell: "nothing", cancelled: true };
// The Ctrl+Shift+C and Ctrl+Shift+V cases, alike on Windows and Linux.
const CTRL_SHIFT_COPY_PASTE = {
  "ctrl-shift-c-selection": COPIES_AND_KEEPS,
  "ctrl-shift-c-none": DOES_NOTHING,
  "ctrl-shift-v": PASTES,
};
const EXPECTED = {
  windows: {
    "ctrl-c-selection": { copied: "selection", shell: "nothing", secondPressShell: "^C", selection: "cleared" },
    "ctrl-c-running": INTERRUPTS,
    "ctrl-v": PASTES,
    ...CTRL_SHIFT_COPY_PASTE,
  },
  linux: {
    "ctrl-c-selection": { copied: "nothing", shell: "^C", secondPressShell: "^C" },
    "ctrl-c-running": INTERRUPTS,
    "ctrl-v": { shell: "^V" },
    ...CTRL_SHIFT_COPY_PASTE,
  },
  macos: {
    "ctrl-c-selection": { copied: "nothing", shell: "^C", secondPressShell: "^C" },
    "ctrl-c-running": INTERRUPTS,
    "ctrl-v": { shell: "^V" },
    "ctrl-shift-c-selection": { copied: "nothing", shell: "nothing", cancelled: false },
    "ctrl-shift-c-none": { copied: "nothing", shell: "nothing", cancelled: false },
    "ctrl-shift-v": { shell: "nothing" },
  },
};

// The probe the cases run in the terminal, written into the workshop
// container. 'read' clears the screen, shows the text given, and, with the
// terminal raw, so that ^C and ^V arrive as bytes, prints in hex every byte
// the terminal delivers until three seconds pass without one, as many times
// as the presses given, one by default. 'sleep' clears the screen and sleeps
// ten seconds, saying on a line of its own, after the ^C the terminal
// echoes, whether an interrupt ended it.
const PROBE_PATH = "/tmp/session-acceptance-keys";
const PROBE_SCRIPT = `#!/bin/bash
case "$1" in
read)
    printf '\\033[H\\033[2J%s\\n' "$3"
    presses=\${4:-1}
    saved=$(stty -g)
    stty raw -echo min 0 time 30
    for ((press = 1; press <= presses; press++)); do
        printf 'keys-ready-%s-press%s\\r\\n' "$2" "$press"
        bytes=$(od -An -tx1 -v | tr -d ' \\n')
        if ((press == presses)); then stty "$saved"; fi
        printf 'keys-got-%s-press%s:%s.\\r\\n' "$2" "$press" "$bytes"
    done
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

const TERMINAL_1_SELECTOR = "#terminal-1";
const TERMINAL_1_ROWS_SELECTOR = `${TERMINAL_1_SELECTOR} .xterm-rows > div`;

// browserAction says what the browser itself does for a shortcut under a
// platform, and how the step brings it about. Chrome's own editing
// commands follow the operating system it runs on, not the reported
// platform, so where the host's Chrome doesn't run the command the faked
// platform's Chrome would, the step sends the key with that command
// attached, which Chrome runs only when the page leaves the key's default
// action alone, as it does its own.
function browserAction(platform, shortcut, hostBindsShortcut) {
  const command = platform.chromeBindsCtrlShortcuts ? shortcut.command : null;
  if (command) {
    return hostBindsShortcut
      ? { mode: "native", command, text: `native ${command}` }
      : { mode: "simulated", command, text: `simulated ${command}` };
  }
  if (hostBindsShortcut) {
    return { mode: "host", command: null, text: `host's ${shortcut.command}, which ${platform.label} lacks` };
  }
  if (shortcut.opensInspector && platform.chromeBindsCtrlShortcuts) {
    return { mode: "not run", command: null, text: "inspector not run" };
  }
  return { mode: "none", command: null, text: "none" };
}

// reportPlatform makes a page report a platform from its next load on:
// navigator.platform reads the platform's own, and the user agent names its
// operating system.
function reportPlatform(page, platform, browserVersion) {
  const version = browserVersion.replace(/^[A-Za-z]*Chrome\//, "");
  return page.setUserAgent({
    userAgent: `Mozilla/5.0 (${platform.os}) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/${version} Safari/537.36`,
    platform: platform.platform,
  });
}

// classifyShell names what reached the shell: nothing, ^C, ^V, the pasted
// text with or without bracketed paste around it, or the bytes themselves.
function classifyShell(bytes, pasted) {
  if (bytes === "") return "nothing";
  if (bytes === "\x03") return "^C";
  if (bytes === "\x16") return "^V";
  const unbracketed = bytes.replace(/^\x1b\[200~/, "").replace(/\x1b\[201~$/, "");
  if (pasted && unbracketed === pasted) return "pasted";
  return JSON.stringify(bytes);
}

// classifyCopied names what a copy put on the clipboard: nothing, the
// selected text, or the text itself.
function classifyCopied(text, selected) {
  if (text === "") return "nothing";
  if (text === selected) return "selection";
  return JSON.stringify(text);
}

function hexToText(hex) {
  return Buffer.from(hex, "hex").toString("latin1");
}

function shellExpects(value) {
  if (value === "pasted") return "pastes";
  if (value === "nothing") return "nothing reaches the shell";
  return `${value} reaches the shell`;
}

function shellObserved(value) {
  return value === "pasted" ? "shell got the paste" : `shell got ${value}`;
}

// What a case observes, field by field in the order it is reported, with
// how the key behaviour table's value for a field reads and how an
// observed value reads, or null when there is nothing to say. interrupted:
// whether the running command was interrupted. copied: what a copy put on
// the clipboard, as classifyCopied names it. pasteEvent: what the page's
// paste event carried, which is reported, never judged. selection: whether
// the selection was kept or cleared, or none was made. shell: what reached
// the shell, as classifyShell names it. secondPressShell: the same, for the
// key pressed a second time. cancelled: whether the key's default action
// was cancelled.
const OBSERVATIONS = [
  {
    field: "interrupted",
    expects: (v) => (v ? "interrupts sleep" : "lets sleep run on"),
    observed: (v) => (v ? "interrupted sleep" : "sleep ran on"),
  },
  {
    field: "copied",
    expects: (v) => (v === "selection" ? "copies the selection" : "copies nothing"),
    observed: (v) => (v === "selection" ? "copied the selection" : v === "nothing" ? "no copy" : `copied ${v}`),
  },
  { field: "pasteEvent", observed: (v) => `paste event ${v}` },
  {
    field: "selection",
    expects: (v) => `${v === "kept" ? "keeps" : "clears"} the selection`,
    observed: (v) => (v === "none" ? null : `selection ${v}`),
  },
  { field: "shell", expects: shellExpects, observed: shellObserved },
  {
    field: "secondPressShell",
    expects: (v) => `on a second press ${shellExpects(v)}`,
    observed: (v) => `on a second press ${shellObserved(v)}`,
  },
  {
    field: "cancelled",
    expects: (v) => (v ? "cancels the key" : "leaves the key to the browser"),
    observed: (v) => (v ? "cancelled" : "not cancelled"),
  },
];

// judge compares what a case observed with what the table says, and
// returns the fields that differ.
function judge(expected, observed) {
  return Object.keys(expected).filter((field) => observed[field] !== expected[field]);
}

export function describeExpected(expected) {
  return OBSERVATIONS.filter((o) => expected[o.field] !== undefined)
    .map((o) => o.expects(expected[o.field]))
    .join(", ");
}

// describeFields reads out the fields of an observation that are also
// fields of selecting, in the order they are reported.
function describeFields(observed, selecting) {
  return OBSERVATIONS.filter((o) => selecting[o.field] !== undefined && observed[o.field] !== undefined)
    .map((o) => o.observed(observed[o.field]))
    .filter(Boolean)
    .join(", ");
}

export function describeObserved(observed) {
  if (observed.error) return `not observed: ${observed.error}`;
  return describeFields(observed, observed);
}

// copyPasteCaseCell reads out a case for a table cell: whether it matched,
// what was observed of what the table judges, and the browser's own action.
export function copyPasteCaseCell(c) {
  if (!c) return "-";
  if (c.observed.error) return `FAIL: not observed [${c.action.text}]`;
  return `${c.matches ? "ok" : "FAIL"}: ${describeFields(c.observed, c.expected)} [${c.action.text}]`;
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
    .evaluate((rows) => [...document.querySelectorAll(rows)].map((r) => r.textContent.replace(/\s+$/, "")), TERMINAL_1_ROWS_SELECTOR)
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
    TERMINAL_1_SELECTOR,
  );
}

// focusTerminal clicks the terminal's last row, which focuses it and
// clears any selection, as a click does for a user.
async function focusTerminal(page, frame) {
  const screen = await frame.$(`${TERMINAL_1_SELECTOR} .xterm-screen`);
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
    TERMINAL_1_SELECTOR,
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
  for (const row of await frame.$$(TERMINAL_1_ROWS_SELECTOR)) {
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

// findHostBindings finds which shortcuts the host's Chrome turns into its
// own copy or paste under a platform, by shortcut: in a page of its own
// reporting that platform, it presses each in a textarea with text
// selected and watches for the copy or paste event.
async function findHostBindings(newPage, platform, browserVersion) {
  const page = await newPage();
  try {
    await reportPlatform(page, platform, browserVersion);
    await page.setContent(
      `<textarea id="probe"></textarea><script>
        window.seen = [];
        for (const t of ["copy", "paste"]) document.addEventListener(t, () => window.seen.push(t), true);
      </script>`,
    );
    const hostBindings = {};
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
      hostBindings[shortcut.id] = (await page.evaluate(() => window.seen)).includes(shortcut.event);
    }
    return hostBindings;
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
      if ((await terminalRows(frame)).some((r) => r !== "")) return frame;
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
  const presses = testCase.pressAgain ? 2 : 1;
  const observed = {};
  // The line the probe prints after a press: the bytes it read, or how its
  // sleep ended.
  const probeEnd = (press) => (r) => {
    if (r === `keys-interrupted-${id}`) return { interrupted: true };
    if (r === `keys-slept-${id}`) return { interrupted: false };
    const got = r.match(new RegExp(`^keys-got-${id}-press${press}:([0-9a-f]*)\\.$`));
    return got ? { bytes: hexToText(got[1]) } : null;
  };
  const waitForProbeEnd = (press) => waitForRow(frame, probeEnd(press), 20_000, `the probe's line after press ${press}`);
  const waitForProbeReady = (press) => waitForRow(frame, (r) => r === `keys-ready-${id}-press${press}`, 30_000, `the probe ready for press ${press}`);
  const pressKey = () => pressShortcut(page, shortcut, action.mode === "simulated" ? action.command : null);

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
      await typeLine(page, `bash ${PROBE_PATH} read ${id} ${shown} ${presses}`);
      started = true;
      await waitForProbeReady(1);
      if (testCase.selection) {
        await selectWord(page, frame, shown);
        observed.selectedText = shown;
      }
    }
    const selectedBefore = await hasSelection(frame);
    if (testCase.selection && !selectedBefore) throw new Error("double-clicking the text selected nothing");
    if (!testCase.selection && selectedBefore) throw new Error("the terminal kept a selection after a click");

    await resetObserver(frame);
    await pressKey();
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
      observed.copied = classifyCopied(seen.copies.filter((c) => c !== "").pop() ?? "", observed.selectedText);
    }
    observed.selection = selectedBefore ? ((await hasSelection(frame)) ? "kept" : "cleared") : "none";

    const end = await waitForProbeEnd(1);
    if (testCase.kind === "interrupt") {
      observed.interrupted = end.interrupted;
    } else {
      observed.bytes = end.bytes;
      observed.shell = classifyShell(end.bytes, observed.pasteText);
    }
    if (testCase.pressAgain) {
      await waitForProbeReady(2);
      await pressKey();
      observed.secondPressShell = classifyShell((await waitForProbeEnd(2)).bytes, observed.pasteText);
    }
    started = false;
    return observed;
  } finally {
    // A case that failed leaves its probe to end before the next one types.
    if (started) await waitForProbeEnd(presses).catch(() => {});
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
    const result = { platform: platform.id, label: platform.label, reported: null, cases: [], error: null };
    platforms.push(result);
    let page = null;
    try {
      const hostBindings = await findHostBindings(newPage, platform, browserVersion);
      result.hostBindings = hostBindings;
      page = await newPage();
      // Only the browser's front page has focus, which keys need.
      await page.bringToFront();
      await reportPlatform(page, platform, browserVersion);
      await page.goto(sessionPageUrl, { waitUntil: "domcontentloaded", timeout: 120_000 });
      const frame = await dashboardFrame(page, sessionHost);
      result.reported = await frame.evaluate(() => navigator.platform);
      log("copy-paste-platform", { platform: platform.id, reported: result.reported, hostBindings });
      if (result.reported !== platform.platform) {
        throw new Error(`the dashboard's frame reports navigator.platform ${result.reported}, not ${platform.platform}`);
      }
      await connectTerminal(page, frame, `keys-connected-${platform.id}`);
      await installObserver(frame);
      for (const [index, testCase] of CASES.entries()) {
        const shortcut = SHORTCUTS[testCase.shortcut];
        const action = browserAction(platform, shortcut, !!hostBindings[shortcut.id]);
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
