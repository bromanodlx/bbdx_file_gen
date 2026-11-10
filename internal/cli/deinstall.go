package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/bromanodlx/bbdx_file_gen/internal/model"
	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate"
)

// deinstallCmd is the subcommand that removes the tool from the system.
const deinstallCmd = "deinstall"

// Hooks for locating the installed program; tests replace them.
var (
	// executable returns the path of the running program.
	executable = os.Executable
	// invokedAs returns the name or path the program was started under,
	// which may be a symlink to the executable.
	invokedAs = func() string { return os.Args[0] }
)

// deinstall asks for confirmation and then removes everything the tool
// installed or created for itself: its configuration directory, the
// executable and a symlink it was started through. Files the user generated
// are left alone. It returns the process exit code.
func deinstall(stdin io.Reader, stdout, stderr io.Writer, cfgPath string) int {
	fmt.Fprint(stdout, "Are you sure?? [y/N] ")
	answer, _ := bufio.NewReader(stdin).ReadString('\n')
	if a := strings.ToLower(strings.TrimSpace(answer)); a != "y" && a != "yes" {
		fmt.Fprintln(stdout, "Nothing was removed.")
		return 0
	}

	targets, err := deinstallTargets(cfgPath)
	if err != nil {
		fmt.Fprintln(stderr, name+":", err)
		return 1
	}
	code := 0
	for _, path := range targets {
		// RemoveAll deletes a symlink itself, never what it points to, and
		// reports success for a path that is already gone.
		if err := os.RemoveAll(path); err != nil {
			fmt.Fprintf(stderr, "%s: could not remove %s: %v\n", name, path, err)
			code = 1
			continue
		}
		fmt.Fprintln(stdout, "removed", path)
	}
	if code == 0 {
		fmt.Fprintln(stdout, name, "has been deinstalled.")
	}
	return code
}

// deinstallTargets lists the paths to remove, in order: the configuration
// directory (if present), a symlink the program was started through (if it
// points to this executable), an executable left over from an update (if
// present) and the executable itself.
func deinstallTargets(cfgPath string) ([]string, error) {
	var targets []string
	if dir := filepath.Dir(cfgPath); model.Exists(dir) {
		targets = append(targets, dir)
	}
	exe, err := installedExecutable()
	if err != nil {
		return nil, err
	}
	if link := invokedLink(exe); link != "" {
		targets = append(targets, link)
	}
	// An update on Windows leaves the replaced executable behind as exe.old.
	if old := selfupdate.BackupPath(exe); model.Exists(old) {
		targets = append(targets, old)
	}
	return append(targets, exe), nil
}

// installedExecutable returns the path of the running program's executable
// with symlinks resolved: the file to replace on update or remove on
// deinstall.
func installedExecutable() (string, error) {
	exe, err := executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate the %s executable: %w", name, err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return exe, nil
}

// invokedLink returns the absolute path the program was started under when
// that path is a symlink resolving to exe, and "" otherwise.
func invokedLink(exe string) string {
	path, err := exec.LookPath(invokedAs())
	if err != nil {
		return ""
	}
	if path, err = filepath.Abs(path); err != nil {
		return ""
	}
	st, err := os.Lstat(path)
	if err != nil || st.Mode()&os.ModeSymlink == 0 {
		return ""
	}
	if real, err := filepath.EvalSymlinks(path); err != nil || real != exe {
		return ""
	}
	return path
}
