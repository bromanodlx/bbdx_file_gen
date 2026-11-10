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

const version = "1.1.0"

func main() {
	list := flag.Bool("list", false, "list supported file types and exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	setDownloads := flag.String("download-dir", "", "set the standard download directory (absolute or relative path) and exit")
	plain := flag.Bool("plain", false, "ASCII-only, no-color interface for basic terminals")
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
	home, err := os.UserHomeDir()
	if err != nil {
		fail(err)
	}
	cfgPath, err := model.DefaultConfigPath()
	if err != nil {
		fail(err)
	}
	store := model.ConfigStore{Path: cfgPath}

	if *setDownloads != "" {
		dir, err := model.SetDownloadDir(store, *setDownloads, cwd, home)
		if err != nil {
			fail(err)
		}
		fmt.Println("standard download directory set to", dir)
		return
	}

	// Fall back to the plain interface on request, or for terminals that opt
	// out of color (NO_COLOR) or cannot render ANSI (TERM=dumb).
	plainMode := *plain || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"

	suggested, _ := model.SuggestDownloadDir() // only a proposal for the setup screen
	ctrl := controller.New(&model.Service{Registry: reg}, controller.Setup{
		Current: cwd, Home: home, Suggested: suggested, Store: store, Plain: plainMode,
	})
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
