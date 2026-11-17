// The run's report: a table to read at a glance and, per fixture, what was
// observed, current behaviour included.

import { CASES, SHORTCUTS, describeExpected, describeObserved } from "./copypaste.mjs";

function kubectlCell(check) {
  return check ? check.outcome : "-";
}

function consoleCell(r) {
  if (!r.features.console) return "-";
  const c = r.console;
  if (!c) return "not checked";
  const parts = [`${c.state} ${c.answered}/${c.probes}`];
  if (c.log?.panics) parts.push(`panicked x${c.log.panics}`);
  if (c.log?.restarts) parts.push(`restarted x${c.log.restarts}`);
  if (c.selector) parts.push(`selector ${c.selector}`);
  if (c.maxNotifications) parts.push(`${c.maxNotifications} notifications`);
  return parts.join(", ");
}

function tunnelCell(r) {
  if (!r.features.tunnel) return "-";
  if (!r.tunnel) return "not checked";
  return r.tunnel.connected ? "connected" : "failed";
}

function copyPasteCell(r) {
  if (!r.features.copyPaste) return "-";
  if (!r.copyPaste) return "not checked";
  const cases = r.copyPaste.platforms.flatMap((p) => p.cases);
  const matched = cases.filter((c) => c.matches).length;
  const notRun = r.copyPaste.platforms.filter((p) => p.error).length;
  return `${matched} of ${CASES.length * r.copyPaste.platforms.length} match${notRun ? `, ${notRun} platform(s) not run` : ""}`;
}

// A case's cell in the copy and paste table: whether it matched, what was
// observed of what the table judges, and the browser's own action.
function copyPasteCaseCell(c) {
  if (!c) return "-";
  if (c.observed.error) return `FAIL: not observed [${c.action.text}]`;
  const o = c.observed;
  const parts = [];
  if (c.expected.interrupted !== undefined) parts.push(o.interrupted ? "interrupted" : "not interrupted");
  if (c.expected.copied !== undefined) {
    parts.push(o.copied === "" ? "no copy" : o.copied === o.selectedText ? "copied" : `copied ${JSON.stringify(o.copied)}`);
  }
  if (c.expected.selection !== undefined) parts.push(`selection ${o.selection}`);
  if (c.expected.shell !== undefined) parts.push(`shell ${o.shell === "pasted" ? "got the paste" : `got ${o.shell}`}`);
  if (c.expected.cancelled !== undefined) parts.push(o.cancelled ? "cancelled" : "not cancelled");
  return `${c.matches ? "ok" : "FAIL"}: ${parts.join(", ")} [${c.action.text}]`;
}

function markdownTable(rows) {
  return [
    `| ${rows[0].join(" | ")} |`,
    `|${rows[0].map(() => "---").join("|")}|`,
    ...rows.slice(1).map((row) => `| ${row.join(" | ")} |`),
  ].join("\n");
}

// copyPasteTable shows, per fixture and platform, each copy and paste case.
function copyPasteTable(results) {
  const rows = [["Fixture", "Platform", ...CASES.map((c) => c.label)]];
  for (const r of results) {
    for (const p of r.copyPaste?.platforms ?? []) {
      const byCase = Object.fromEntries(p.cases.map((c) => [c.case, c]));
      rows.push([r.fixture, p.error ? `${p.label}: not run` : p.label, ...CASES.map((c) => copyPasteCaseCell(byCase[c.id]))]);
    }
  }
  return rows.length > 1 ? markdownTable(rows) : null;
}

function walkCell(r) {
  if (!r.walk) return "not walked";
  return `${r.walk.pages} pages, ${r.walk.clicked} actions, ${r.walk.terminalErrors.length} terminal errors`;
}

export function summaryTable(results) {
  const rows = [
    ["Fixture", "Result", "Walk", "get ns <own>", "get ns", "get pods", "Context namespace", "Console", "Tunnel", "Copy and paste"],
    ...results.map((r) => [
      r.fixture,
      r.passed ? "PASS" : "FAIL",
      walkCell(r),
      kubectlCell(r.kubectl?.getOwnNamespace),
      kubectlCell(r.kubectl?.listNamespaces),
      kubectlCell(r.kubectl?.listPods),
      r.kubectl?.contextNamespace ?? "-",
      consoleCell(r),
      tunnelCell(r),
      copyPasteCell(r),
    ]),
  ];
  const copyPaste = copyPasteTable(results);
  return copyPaste ? `${markdownTable(rows)}\n\n${copyPaste}` : markdownTable(rows);
}

function fence(text) {
  return ["```", text, "```"].join("\n");
}

function fixtureSection(r) {
  const out = [`## ${r.fixture}: ${r.passed ? "PASS" : "FAIL"}`, "", r.summary, ""];
  out.push(`- Session: ${r.session ?? "-"} in environment ${r.environment ?? "-"}, pod ${r.pod ?? "-"}, ${r.seconds}s`);
  if (r.workshopContainerRestarts !== undefined) out.push(`- Workshop container restarts: ${r.workshopContainerRestarts}`);
  if (r.walk) {
    out.push(
      `- Walk: ${r.walk.completed ? "reached the last page" : "did not finish"}, ${r.walk.pages} pages, ` +
        `${r.walk.clicked} actions clicked, ${r.walk.skipped} skipped (dashboard:open-url)`,
    );
  }
  if (r.kubectl) {
    out.push(`- Context namespace in the workshop container: ${r.kubectl.contextNamespace}`);
    for (const id of ["getOwnNamespace", "listNamespaces", "listPods"]) {
      const c = r.kubectl[id];
      out.push(`- \`${c.title}\`: ${c.outcome}`, "", fence(c.output || "(no output)"), "");
    }
  }
  if (r.console) {
    const c = r.console;
    out.push(`- Console: ${c.state}, ${c.answered} of ${c.probes} probes answered, pod lists in at most ${c.maxPodListMs} ms`);
    if (c.log) {
      out.push(
        `- Console process: started ${c.log.starts} time(s) with ${c.log.startedWith ?? "unknown arguments"}, ` +
          `${c.log.panics} panic(s), ${c.log.accessErrors} access errors logged`,
      );
      if (c.log.firstPanic) out.push("", fence(c.log.firstPanic), "");
    }
    out.push(`- Console namespace selector: ${c.selector ?? "(none seen)"}`);
    if (c.namespaceList) {
      const nl = c.namespaceList;
      out.push(
        nl.error
          ? `- Console namespace list: ${nl.error}`
          : `- Console namespace list: HTTP ${nl.status}, ${nl.names?.length ?? 0} namespaces [${(nl.names ?? []).join(", ")}], errors [${(nl.errors ?? []).join(", ")}]`,
      );
    }
    if (c.settings && !c.settings.error) {
      out.push(
        `- Console settings: defaultNamespace ${c.settings.defaultNamespace ?? "-"}, ` +
          `namespaceFallbackList [${(c.settings.namespaceFallbackList ?? []).join(", ")}], ` +
          `disableAccessDeniedNotifications ${c.settings.disableAccessDeniedNotifications ?? "-"}`,
      );
    }
    out.push(`- Console unread notifications: at most ${c.maxNotifications}`);
  }
  if (r.tunnel) {
    out.push(`- Tunnel from this host with \`educates tunnel connect\`: ${r.tunnel.connected ? "connected" : "failed"}`, "", fence(r.tunnel.output || "(no output)"), "");
  }
  if (r.copyPaste) out.push(...copyPasteSection(r.copyPaste));
  for (const n of r.notes) out.push(`- Note: ${n}`);
  if (r.problems.length) {
    out.push("", "Problems:", "");
    for (const p of r.problems) out.push(`- ${p}`);
  }
  return out.join("\n");
}

// copyPasteSection reports each platform's cases against the table, with
// the browser's own action and whether it ran natively or was simulated.
function copyPasteSection(copyPaste) {
  const out = ["", "Terminal copy and paste, in terminal 1 of the dashboard inside the portal's page:", ""];
  for (const p of copyPaste.platforms) {
    const host = p.hostBindings
      ? Object.entries(p.hostBindings)
          .map(([id, native]) => `${SHORTCUTS[id].label} ${native ? "fires a" : "fires no"} ${SHORTCUTS[id].event} event`)
          .join(", ")
      : "not checked";
    out.push(`- ${p.label}: navigator.platform ${p.reported ?? "not read"} in the dashboard's frame; in this host's Chrome, unaided, ${host}`);
    if (p.error) out.push(`  - Not run: ${p.error}`);
  }
  for (const p of copyPaste.platforms) {
    if (!p.cases.length) continue;
    out.push("", `${p.label}:`, "");
    const rows = [["Case", "The table says", "Observed", "Matches", "Browser's own action"]];
    for (const c of p.cases) {
      rows.push([c.label, describeExpected(c.expected), describeObserved(c.observed), c.matches ? "yes" : "no", c.action.text]);
    }
    out.push(markdownTable(rows));
  }
  out.push("");
  return out;
}

export function markdownReport(run, results) {
  const passed = results.filter((r) => r.passed).length;
  const lines = [
    "# Session acceptance report",
    "",
    `- Cluster: ${run.cluster.description}, kube context ${run.cluster.context}, ` +
      `${run.cluster.protocol}://*.${run.cluster.domain} on ${run.cluster.listenAddress}`,
    `- CLI: ${run.cli} (${run.cliVersion})`,
    `- Profile: ${run.profileName}, console held ${Math.round(run.profile.holdMs / 1000)}s, probed every ${Math.round(run.profile.probeEvery / 1000)}s`,
    `- Test portal: ${run.portal}; robot credentials from ${run.credentialsSource}`,
    `- Started ${run.started}, finished ${run.finished}`,
    `- Result: ${passed === results.length ? "PASS" : "FAIL"}, ${passed} of ${results.length} fixtures passed`,
    "",
    summaryTable(results),
    "",
    ...results.map(fixtureSection),
    "",
  ];
  return lines.join("\n");
}
