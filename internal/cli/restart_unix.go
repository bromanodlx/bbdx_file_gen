//go:build !windows

package cli

import (
	"os"
	"syscall"
)

// restartProcess replaces this process with exe, keeping its arguments,
// environment and terminal. It only returns on failure.
func restartProcess(exe string) (int, error) {
	return 1, syscall.Exec(exe, os.Args, os.Environ())
}
