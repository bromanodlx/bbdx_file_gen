package view

import (
	"fmt"
	"strings"
	"text/tabwriter"
)

// ListRow is one line of the non-interactive format list.
type ListRow struct {
	Ext, Aliases, Name, Category, MinSize string
}

// RenderList prints rows grouped by category, in the order given.
func RenderList(rows []ListRow) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	cat := ""
	for _, r := range rows {
		if r.Category != cat {
			if cat != "" {
				fmt.Fprintln(tw)
			}
			cat = r.Category
			fmt.Fprintf(tw, "%s\n", cat)
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\tmin %s\n", r.Ext, r.Aliases, r.Name, r.MinSize)
	}
	tw.Flush()
	fmt.Fprintf(&b, "\n%d file types\n", len(rows))
	return b.String()
}
