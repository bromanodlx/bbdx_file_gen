//go:build !windows

package selfupdate

import (
	"os"
	"syscall"
)

// Restart replaces this process with exe, keeping its arguments,
// environment and terminal. It only returns on failure.
func Restart(exe string) (int, error) {
	return 1, syscall.Exec(exe, os.Args, os.Environ())
}
