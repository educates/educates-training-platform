// The checks a run is judged by. Each takes what the run recorded and
// returns { passed, detail }, so a run's pass or fail is decided here and
// nowhere else.

// judgeAlternation judges the series of new session requests. With the same
// workshop at equal capacity on two clusters, every request counts the
// sessions the one before it placed, so each pair of requests, the first
// and second, the third and fourth, and so on, puts one session on each
// cluster. Which cluster of a pair goes first is random, since equally
// ranked candidates are ordered at random.
export function judgeAlternation(placements, clusters) {
  const seen = new Map();
  for (const p of placements) {
    if (p.status !== 200) {
      return fail(`request ${p.index} for ${p.user} answered ${p.status ?? "nothing"}: ${p.body ?? p.error ?? ""}`.trim());
    }
    if (!clusters.includes(p.clusterName)) {
      return fail(`request ${p.index} got a session on ${p.clusterName}, not on ${clusters.join(" or ")}`);
    }
    const key = `${p.clusterName}/${p.sessionName}`;
    if (seen.has(key)) {
      return fail(`request ${p.index} got session ${p.sessionName} on ${p.clusterName}, which request ${seen.get(key)} got`);
    }
    seen.set(key, p.index);
  }
  for (let i = 0; i + 1 < placements.length; i += 2) {
    const [first, second] = [placements[i], placements[i + 1]];
    if (first.clusterName === second.clusterName) {
      return fail(`requests ${first.index} and ${second.index} both went to ${first.clusterName}`);
    }
  }
  const pairs = [];
  for (let i = 0; i < placements.length; i += 2) {
    pairs.push(placements.slice(i, i + 2).map((p) => p.clusterName).join(" "));
  }
  return pass(
    `${placements.length} new sessions, ${placements.length / clusters.length} on each cluster, one of each pair on each: ${pairs.join(" | ")}`,
  );
}

// judgeReturning judges a returning user's request: the user asks for the
// workshop again and must be given the session their first request got,
// on the same cluster, portal and environment.
export function judgeReturning(first, again) {
  if (again.status !== 200) {
    return fail(`${first.user}'s return answered ${again.status ?? "nothing"}: ${again.body ?? again.error ?? ""}`.trim());
  }
  const where = (p) => `session ${p.sessionName} on ${p.clusterName}`;
  const same = ["clusterName", "portalName", "environmentName", "sessionName"].every((k) => again[k] === first[k]);
  if (!same) {
    return fail(`${first.user} got ${where(again)}, not their ${where(first)}`);
  }
  return pass(`${first.user} got ${where(first)} back`);
}

// judgeClusterSessions checks the placements against what the clusters
// themselves hold, read with kubectl rather than through the Lookup
// Service: each placed session exists on the cluster it was placed on, in
// use by the user it was requested for, and the portal holds no session
// that no request got. heldByCluster maps each cluster's name to its
// sessions of the portal, as { name, phase, user }.
export function judgeClusterSessions(placements, heldByCluster) {
  const placedOn = new Map(placements.map((p) => [`${p.clusterName}/${p.sessionName}`, p]));
  for (const p of placements) {
    const session = (heldByCluster[p.clusterName] ?? []).find((s) => s.name === p.sessionName);
    if (!session) return fail(`${p.clusterName} holds no session ${p.sessionName}, which request ${p.index} got`);
    if (session.phase !== "Allocated") {
      return fail(`session ${p.sessionName} on ${p.clusterName} is in phase ${session.phase ?? "none"}, not Allocated`);
    }
    if (session.user !== p.user) {
      return fail(`session ${p.sessionName} on ${p.clusterName} belongs to ${session.user ?? "no user"}, not ${p.user}`);
    }
  }
  const clusters = Object.keys(heldByCluster).sort();
  for (const cluster of clusters) {
    for (const s of heldByCluster[cluster]) {
      if (!placedOn.has(`${cluster}/${s.name}`)) {
        return fail(`${cluster} holds session ${s.name} of ${s.user ?? "no user"}, which no request got`);
      }
    }
  }
  const counts = clusters.map((c) => `${c} ${heldByCluster[c].length}`).join(", ");
  return pass(`the clusters hold the ${placements.length} sessions placed, each Allocated to its user: ${counts}`);
}

function pass(detail) {
  return { passed: true, detail };
}

function fail(detail) {
  return { passed: false, detail };
}
