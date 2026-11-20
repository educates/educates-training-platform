// Reading the table 'educates local cluster list' prints, for the harnesses
// that find their clusters in it.

// parseClusterList parses the table 'educates local cluster list' prints.
// Its columns are set apart by two spaces or more, and a heading or a value
// may hold a single space ("LISTEN IP", "partly running"), so each value is
// taken from the column under its heading, whatever other columns the CLI
// prints.
export function parseClusterList(text) {
  const cells = (line) => line.trim().split(/\s{2,}/);
  const [heading, ...lines] = text.split("\n");
  const headings = cells(heading ?? "");
  return lines
    .map(cells)
    .filter((cols) => cols.length === headings.length && cols.length >= 5)
    .map((cols) => {
      const value = (name) => cols[headings.indexOf(name)];
      return {
        name: value("NAME"),
        state: value("STATE"),
        listenAddress: value("LISTEN IP"),
        domain: value("DOMAIN"),
        context: value("CONTEXT"),
      };
    });
}
