//go:build windows

package selfupdate

import (
	"errors"
	"os"
	"os/exec"
)

// Restart runs exe with this process's arguments and terminal, waits
// for it and returns its exit code. Windows has no exec, so the old process
// stays around, idle, until the new one ends.
func Restart(exe string) (int, error) {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode(), nil
	}
	if err != nil {
		return 1, err
	}
	return 0, nil
}
