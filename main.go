// Command bbdx_file_gen is a TUI for creating standard-compliant test files.
package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"bbdx_file_gen/internal/controller"
	"bbdx_file_gen/internal/model"
	"bbdx_file_gen/internal/view"
)

const version = "1.0.0"

func main() {
	list := flag.Bool("list", false, "list supported file types and exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	reg := model.Default()
	switch {
	case *showVersion:
		fmt.Println("bbdx_file_gen", version)
		return
	case *list:
		fmt.Print(view.RenderList(listRows(reg)))
		return
	}

	cwd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	downloads, err := model.DownloadDir()
	if err != nil {
		fail(err)
	}
	ctrl := controller.New(&model.Service{Registry: reg}, controller.Locations{Current: cwd, Downloads: downloads})
	if _, err := tea.NewProgram(ctrl, tea.WithAltScreen()).Run(); err != nil {
		fail(err)
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "bbdx_file_gen:", err)
	os.Exit(1)
}

func listRows(reg *model.Registry) []view.ListRow {
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
