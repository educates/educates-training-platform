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

	// Print data rows
	for i, row := range data {
		if i < len(data) - 1 {
			fmt.Fprintln(w, strings.Join(row, "\t"))
		}else {
			fmt.Fprint(w, strings.Join(row, "\t"))
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
		if i < len(data) - 1 {
			fmt.Fprintf(w, "%s:\t%s\n", row[0], row[1])
		}else {
			fmt.Fprintf(w, "%s:\t%s", row[0], row[1])
		}
	}

	w.Flush()
	return buf.String()
}
