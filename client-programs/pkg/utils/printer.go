package utils

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// PrintTable renders a simple aligned text table with the given headers and
// rows, returning it as a string (without a trailing newline). Empty cells
// are rendered as blank while keeping their column, so callers can leave a
// value unset without breaking alignment.
func PrintTable(headers []string, rows [][]string) string {
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 8, 8, 3, ' ', 0)

	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, row := range rows {
		fmt.Fprintln(w, strings.Join(row, "\t"))
	}

	w.Flush()

	return strings.TrimRight(buf.String(), "\n")
}

// PrintKeyValuesTable renders an aligned "Key: value" table (one row per
// key/value pair) as a string, without a trailing newline. Each row must be
// a two-element slice of {key, value}.
func PrintKeyValuesTable(rows [][]string) string {
	var buf strings.Builder
	w := tabwriter.NewWriter(&buf, 8, 8, 3, ' ', 0)

	for _, row := range rows {
		fmt.Fprintf(w, "%s:\t%s\n", row[0], row[1])
	}

	w.Flush()

	return strings.TrimRight(buf.String(), "\n")
}
