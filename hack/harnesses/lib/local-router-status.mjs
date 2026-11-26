// Reading what 'educates local router status' prints, for the harnesses that
// reach a local cluster's names through the router every local cluster is
// served through.

import { cells, readTable } from "./cli-table.mjs";

// routerAddressFor returns the router address, the one host address where
// the router serves every local cluster, read from statusText, what
// 'educates local router status' prints. name is the named local cluster, or
// "" for the default local cluster, and domain its ingress domain. A request
// for a name under the domain, sent to the router address, reaches the
// cluster: the router sends it there by its Host header on port 80, and by
// the server name of its TLS handshake on port 443.
//
// It throws, saying why, when the status shows no router address, a router
// that does not run, with what the CLI advises, or no route of the domain to
// the cluster.
export function routerAddressFor(statusText, { name, domain }) {
  const lines = statusText.split("\n");
  const field = (key) => {
    const match = lines.map((line) => new RegExp(`^${key}:\\s+(.*?)\\s*$`).exec(line)).find(Boolean);
    return match?.[1] ?? "";
  };
  const description = name ? `local cluster ${name}` : "the default local cluster";

  const address = field("Router address");
  if (!address || address === "-") {
    throw new Error("'educates local router status' shows no router address; run it to see why");
  }

  const router = field("Router");
  if (!/^running\b/.test(router)) {
    // What the CLI says between the router's fields and the table of
    // routes, such as which command creates or re-creates the router.
    const heading = lines.findIndex((line) => cells(line).includes("CLUSTER"));
    const fieldsEnd = lines.findIndex((line) => line.trim() === "");
    const notes = lines
      .slice(fieldsEnd < 0 ? lines.length : fieldsEnd + 1, heading < 0 ? lines.length : heading)
      .map((line) => line.trim())
      .filter(Boolean);
    throw new Error(`the router is ${router || "unknown"}, not running${notes.length ? `: ${notes.join(" ")}` : ""}`);
  }

  const cluster = name || "(default)";
  const routed = readTable(statusText, ["NAME", "CLUSTER", "TLS", "STATE"]).some(
    (row) => row.CLUSTER === cluster && row.NAME.toLowerCase() === domain.toLowerCase(),
  );
  if (!routed) {
    throw new Error(
      `the router sends no ${domain} to ${description}; 'educates local router status' shows the names it sends to each local cluster`,
    );
  }
  return address;
}
