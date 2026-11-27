// The run's report: a table to read at a glance and, per fixture, what was
// observed, current behaviour included.

import { CASES, SHORTCUTS, copyPasteCaseCell, describeExpected, describeObserved } from "./copypaste.mjs";
import { themeOutcome } from "./theme.mjs";

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
  if (!r.features.terminalCopyPaste) return "-";
  const copyPaste = r.terminalCopyPaste;
  if (!copyPaste) return "not checked";
  const cases = copyPaste.platforms.flatMap((p) => p.cases);
  const matched = cases.filter((c) => c.matches).length;
  const notRun = copyPaste.platforms.filter((p) => p.error).length;
  return `${matched} of ${CASES.length * copyPaste.platforms.length} match${notRun ? `, ${notRun} platform(s) not run` : ""}`;
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
    for (const p of r.terminalCopyPaste?.platforms ?? []) {
      const byCase = Object.fromEntries(p.cases.map((c) => [c.case, c]));
      rows.push([r.fixture, p.error ? `${p.label}: not run` : p.label, ...CASES.map((c) => copyPasteCaseCell(byCase[c.id]))]);
    }
  }
  return rows.length > 1 ? markdownTable(rows) : null;
}

function themeCell(r) {
  if (!r.theme) return "not checked";
  const where = ["portal", "dashboard"];
  if (where.every((w) => r.theme[w]?.served && r.theme[w]?.applied)) return "served";
  return where.map((w) => `${w} ${r.theme[w] ? (r.theme[w].served && r.theme[w].applied ? "served" : "failed") : "not checked"}`).join(", ");
}

function mirrorCell(r) {
  if (!r.features.mirror) return "-";
  if (!r.mirror) return "not pulled";
  return r.mirror.fromMirror ? "pulled from the mirror" : "not from the mirror";
}

function walkCell(r) {
  if (!r.walk) return "not walked";
  return `${r.walk.pages} pages, ${r.walk.clicked} actions, ${r.walk.terminalErrors.length} terminal errors`;
}

export function summaryTable(results) {
  const rows = [
    ["Fixture", "Result", "Workshop image", "Theme", "Walk", "get ns <own>", "get ns", "get pods", "Context namespace", "Console", "Tunnel", "Copy and paste"],
    ...results.map((r) => [
      r.fixture,
      r.passed ? "PASS" : "FAIL",
      mirrorCell(r),
      themeCell(r),
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
  if (r.mirror) {
    const m = r.mirror;
    const copy = m.copy ? `present${Object.keys(m.copy).length ? `, ${Object.entries(m.copy).map(([k, v]) => `${k}: ${v}`).join(", ")}` : ""}` : "absent";
    out.push(
      `- Workshop image: ${m.image}, ${m.fromMirror ? "the mirror's" : "not the mirror's"}; pod's pull secrets [${m.podPullSecrets.join(", ")}], ` +
        `service account ${m.serviceAccount}'s [${(m.serviceAccountPullSecrets ?? []).join(", ")}]; the mirror's pull Secret in the namespace: ${copy}`,
    );
  }
  if (r.theme) {
    for (const where of ["portal", "dashboard"]) out.push(`- Website theme: the ${where} ${themeOutcome(r.theme[where])}`);
  }
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
  if (r.terminalCopyPaste) out.push(...copyPasteSection(r.terminalCopyPaste));
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
          .map(([id, hostBindsShortcut]) => `${SHORTCUTS[id].label} ${hostBindsShortcut ? "fires a" : "fires no"} ${SHORTCUTS[id].event} event`)
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
      `${run.cluster.protocol}://*.${run.cluster.domain} through the router on ${run.cluster.routerAddress}`,
    `- CLI: ${run.cli} (${run.cliVersion})`,
    `- Profile: ${run.profileName}, console held ${Math.round(run.profile.holdMs / 1000)}s, probed every ${Math.round(run.profile.probeEvery / 1000)}s`,
    `- Test portal: ${run.portal}; robot credentials from ${run.credentialsSource}`,
    `- Website theme: ${run.theme ?? "not installed"}`,
    `- Authenticated mirror: ${run.mirror ?? "not used"}`,
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
