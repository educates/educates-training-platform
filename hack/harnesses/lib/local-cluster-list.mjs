// Reading the list 'educates local cluster list' prints, for the harnesses
// that find their clusters in it.

import { readTable } from "./cli-table.mjs";

// parseClusterList returns the local clusters 'educates local cluster list'
// prints, from the table below the router address that heads the list. A
// cluster has no address of its own: every local cluster is reached on the
// router address, which 'educates local router status' shows with the
// names the router sends to each cluster.
export function parseClusterList(text) {
  return readTable(text, ["NAME", "STATE", "DOMAIN", "CONTEXT"]).map((row) => ({
    name: row.NAME,
    state: row.STATE,
    domain: row.DOMAIN,
    context: row.CONTEXT,
  }));
}
