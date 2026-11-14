// The run's report: a table to read at a glance and, per fixture, what was
// observed, current behaviour included.

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

function walkCell(r) {
  if (!r.walk) return "not walked";
  return `${r.walk.pages} pages, ${r.walk.clicked} actions, ${r.walk.terminalErrors.length} terminal errors`;
}

export function summaryTable(results) {
  const rows = [
    ["Fixture", "Result", "Walk", "get ns <own>", "get ns", "get pods", "Context namespace", "Console", "Tunnel"],
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
    ]),
  ];
  return [
    `| ${rows[0].join(" | ")} |`,
    `|${rows[0].map(() => "---").join("|")}|`,
    ...rows.slice(1).map((row) => `| ${row.join(" | ")} |`),
  ].join("\n");
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
  for (const n of r.notes) out.push(`- Note: ${n}`);
  if (r.problems.length) {
    out.push("", "Problems:", "");
    for (const p of r.problems) out.push(`- ${p}`);
  }
  return out.join("\n");
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
