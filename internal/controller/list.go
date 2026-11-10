package controller

import (
	"sort"
	"strings"

	"github.com/bromanodlx/bbdx_file_gen/internal/model"
	"github.com/bromanodlx/bbdx_file_gen/internal/view"
)

// ListRows prepares the formats of reg for view.RenderList: grouped by
// category (alphabetically), sorted by extension within each category.
func ListRows(reg *model.Registry) []view.ListRow {
	formats := reg.All()
	sort.SliceStable(formats, func(i, j int) bool { return formats[i].Category < formats[j].Category })
	rows := make([]view.ListRow, len(formats))
	for i, f := range formats {
		rows[i] = view.ListRow{
			Ext: f.Ext, Aliases: strings.Join(f.Aliases, ","), Name: f.Name,
			Category: string(f.Category), MinSize: model.HumanSize(f.Rule.Min),
		}
	}
	return rows
}
