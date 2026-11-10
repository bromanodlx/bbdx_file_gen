// Package cli is the bbdxfg command line: flag parsing, the non-interactive
// actions and starting the TUI.
package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bromanodlx/bbdx_file_gen/internal/controller"
	"github.com/bromanodlx/bbdx_file_gen/internal/model"
	"github.com/bromanodlx/bbdx_file_gen/internal/view"
)

// Version is the release of the tool.
const Version = "1.5.0"

// startTUI runs the interactive wizard; tests replace it.
var startTUI = func(ctrl *controller.Controller) error {
	_, err := tea.NewProgram(ctrl, tea.WithAltScreen()).Run()
	return err
}

// checkUpdate looks for a newer release in the background; tests replace it.
var checkUpdate = newerRelease

// name prefixes usage and error messages.
const name = view.Command

// Run executes the command line and returns the process exit code: 0 on
// success, 1 on a runtime error, 2 on a usage error. stdin answers the
// deinstall confirmation.
func Run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage of %s:\n  %s [flags]      start the wizard\n  %s %s    remove %s and its configuration\nFlags:\n",
			name, name, name, deinstallCmd, name)
		fs.PrintDefaults()
	}
	list := fs.Bool("list", false, "list supported file types and exit")
	showVersion := fs.Bool("version", false, "print the version and exit")
	setDownloads := fs.String("download-dir", "", "set the standard download directory (absolute or relative path) and exit")
	plain := fs.Bool("plain", false, "ASCII-only, no-color interface for basic terminals")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	cmd, err := subcommand(fs)
	if err != nil {
		fmt.Fprintf(stderr, "%s: %v\n", name, err)
		fs.Usage()
		return 2
	}

	reg := model.Default()
	switch {
	case *showVersion:
		fmt.Fprintln(stdout, name, Version)
		return 0
	case *list:
		fmt.Fprint(stdout, view.RenderList(controller.ListRows(reg)))
		return 0
	}

	fail := func(err error) int {
		fmt.Fprintln(stderr, name+":", err)
		return 1
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fail(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return fail(err)
	}
	cfgPath, err := model.DefaultConfigPath()
	if err != nil {
		return fail(err)
	}
	if cmd == deinstallCmd {
		return deinstall(stdin, stdout, stderr, cfgPath)
	}
	store := model.ConfigStore{Path: cfgPath}

	if *setDownloads != "" {
		dir, err := model.SetDownloadDir(store, *setDownloads, cwd, home)
		if err != nil {
			return fail(err)
		}
		fmt.Fprintln(stdout, "standard download directory set to", dir)
		return 0
	}

	// Fall back to the plain interface on request, or for terminals that opt
	// out of color (NO_COLOR) or cannot render ANSI (TERM=dumb).
	plainMode := *plain || os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb"

	suggested, _ := model.SuggestDownloadDir() // only a proposal for the setup screen
	ctrl := controller.New(&model.Service{Registry: reg}, controller.Setup{
		Current: cwd, Home: home, Suggested: suggested, Store: store, Plain: plainMode,
		Version: Version, CheckUpdate: checkUpdate,
	})
	if err := startTUI(ctrl); err != nil {
		return fail(err)
	}
	// The update question in the wizard closes it on yes; install now that
	// the terminal is back to normal.
	if v := ctrl.UpdateRequested(); v != "" {
		return applyUpdate(v, stdout, stderr)
	}
	return 0
}

// subcommand returns the subcommand named on the command line, or "" for
// the wizard. deinstall stands alone; any other argument is an error, since
// the wizard asks for everything and a stray argument such as a file name
// would otherwise be ignored without notice.
func subcommand(fs *flag.FlagSet) (string, error) {
	switch {
	case fs.NArg() == 0:
		return "", nil
	case fs.Arg(0) != deinstallCmd:
		return "", fmt.Errorf("unexpected argument %q", fs.Arg(0))
	case fs.NArg() > 1 || fs.NFlag() > 0:
		return "", fmt.Errorf("%s takes no flags or arguments", deinstallCmd)
	}
	return deinstallCmd, nil
}
