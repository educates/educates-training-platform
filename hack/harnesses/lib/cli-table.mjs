// Reading the tables the educates CLI prints, for the modules that parse its
// output.

// cells splits a line of a table the CLI prints into its values. The CLI
// sets its columns apart by three spaces or more, and a heading or a value
// may hold a single space ("partly running"), so a line is split on runs of
// two spaces or more.
export function cells(line) {
  return line.trim().split(/\s{2,}/);
}

// readTable returns the rows of the table in text whose heading line holds
// every one of headings, each row an object keyed by the table's column
// headings. Each value is taken from the column under its heading, whatever
// other columns the CLI prints, and the lines around the table, such as the
// lines above its heading, are left out.
export function readTable(text, headings) {
  const lines = text.split("\n");
  const start = lines.findIndex((line) => {
    const values = cells(line);
    return headings.every((heading) => values.includes(heading));
  });
  if (start < 0) return [];
  const columns = cells(lines[start]);
  return lines
    .slice(start + 1)
    .map(cells)
    .filter((values) => values.length === columns.length)
    .map((values) => Object.fromEntries(columns.map((column, i) => [column, values[i]])));
}
