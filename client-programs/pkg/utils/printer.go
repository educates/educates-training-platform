package utils

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

func PrintTable(headers []string, data [][]string) string {
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 8, 8, 3, ' ', 0)

	// Print headers
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	// Print data rows (empty string cells are rendered as tab to preserve alignment)
	for i, row := range data {
		parts := replaceEmptyStringsWithTabs(row)
		if i < len(data)-1 {
			fmt.Fprintln(w, strings.Join(parts, "\t"))
		} else {
			fmt.Fprint(w, strings.Join(parts, "\t"))
		}
	}

	w.Flush()
	return buf.String()
}

func PrintKeyValuesTable(data [][]string) string {
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 8, 8, 3, ' ', 0)

	// Print data rows
	for i, row := range data {
		if i < len(data)-1 {
			fmt.Fprintf(w, "%s:\t%s\n", row[0], row[1])
		} else {
			fmt.Fprintf(w, "%s:\t%s", row[0], row[1])
		}
	}

	w.Flush()
	return buf.String()
}

func replaceEmptyStringsWithTabs(data []string) []string {
	parts := make([]string, len(data))
	for j, cell := range data {
		if cell == "" {
			parts[j] = "\t"
		} else {
			parts[j] = cell
		}
	}
	return parts
}
