// The run's report: report.md for a reader, and the summary printed at the
// end of the run.

export function summary(run) {
  const lines = run.checks.map((c) => `${c.passed ? "PASS" : "FAIL"}  ${c.name}: ${c.detail}`);
  if (run.error) lines.push(`FAIL  The run stopped: ${run.error}`);
  if (run.teardownError) lines.push(`FAIL  Teardown: ${run.teardownError}`);
  return lines.join("\n");
}

const cell = (v) => String(v ?? "").replace(/\|/g, "\\|").replace(/\n/g, " ");

function table(head, rows) {
  return [
    `| ${head.join(" | ")} |`,
    `|${head.map(() => "---").join("|")}|`,
    ...rows.map((r) => `| ${r.map(cell).join(" | ")} |`),
  ].join("\n");
}

function placementRows(placements) {
  return placements.map((p) => [
    p.index,
    p.user,
    p.status ?? "no answer",
    p.clusterName ?? p.body ?? p.error ?? "",
    p.sessionName ?? "",
    p.activation ? `${p.activation.status} ${p.activation.location ?? ""}`.trim() : "",
    p.seconds?.toFixed(1) ?? "",
  ]);
}

export function markdownReport(run) {
  const out = [];
  out.push(`# Lookup acceptance: ${run.passed ? "PASS" : "FAIL"}`);
  out.push("");
  out.push(`- Started ${run.started}, finished ${run.finished}`);
  out.push(`- Build tag \`${run.tag}\`, CLI \`${run.cli}\` (${run.cliVersion})`);
  out.push(`- Lookup Service: ${run.lookupUrl ?? "not reached"}`);
  out.push(`- New sessions requested: ${run.sessions}, each portal and environment with room for ${run.capacity}`);
  out.push("");
  out.push(
    table(
      ["Cluster", "Role", "Listen address", "Domain", "Created by this run", "Kept"],
      run.clusters.map((c) => [c.name, c.role, c.listenAddress, c.domain, c.created ? "yes" : "no", run.keep ? "yes" : "no"]),
    ),
  );
  out.push("");
  out.push("## Checks");
  out.push("");
  out.push(table(["Result", "Check", "Detail"], run.checks.map((c) => [c.passed ? "PASS" : "FAIL", c.name, c.detail])));
  if (run.error) out.push("", `The run stopped: ${run.error}`);
  if (run.teardownError) out.push("", `Teardown failed: ${run.teardownError}`);
  out.push("");
  out.push("## New session requests");
  out.push("");
  out.push(table(["#", "User", "Status", "Cluster", "Session", "Activation", "Seconds"], placementRows(run.placements)));
  out.push("");
  out.push("## Returning users");
  out.push("");
  out.push(table(["#", "User", "Status", "Cluster", "Session", "Activation", "Seconds"], placementRows(run.returns)));
  out.push("");
  out.push("## Sessions the clusters hold");
  out.push("");
  for (const [cluster, sessions] of Object.entries(run.held ?? {})) {
    out.push(`- ${cluster}: ${sessions.length ? sessions.map((s) => `${s.name} (${s.phase}, ${s.user})`).join(", ") : "none"}`);
  }
  out.push("");
  out.push("The Lookup Service's log is in `lookup-service.log` beside this report.");
  out.push("");
  return out.join("\n");
}
