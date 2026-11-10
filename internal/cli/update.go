package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate"
)

// noUpdateEnv turns the start-up update check off when set to any value.
const noUpdateEnv = "BBDXFG_NO_UPDATE_CHECK"

const (
	// checkTimeout bounds the background check; the wizard never waits
	// for it.
	checkTimeout    = 15 * time.Second
	downloadTimeout = 2 * time.Minute
)

// Hooks for updating; tests replace them.
var (
	// updater finds and installs releases of the tool.
	updater = selfupdate.Updater{ReleasesURL: "https://github.com/bromanodlx/bbdx_file_gen/releases", Name: name}
	// restart replaces the running program with the executable at exe; see
	// selfupdate.Restart.
	restart = selfupdate.Restart
)

// newerRelease returns the version of the latest release if it is newer
// than Version, and "" otherwise, including when the check is turned off or
// fails (offline, rate-limited, ...): a missing network must never get in
// the way of the tool. The wizard runs it in the background.
func newerRelease() string {
	if os.Getenv(noUpdateEnv) != "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	latest, err := updater.Latest(ctx)
	if err != nil || !selfupdate.Newer(latest, Version) {
		return ""
	}
	return latest
}

// applyUpdate installs version after the wizard has closed and restarts the
// tool. It returns the process exit code: the restarted program's where the
// platform keeps this process around (Windows), 1 if the update failed.
func applyUpdate(version string, stdout, stderr io.Writer) int {
	fmt.Fprintf(stdout, "Downloading %s %s ...\n", name, version)
	exe, err := installedExecutable()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
		err = updater.Install(ctx, version, exe)
		cancel()
	}
	if err != nil {
		fmt.Fprintf(stderr, "%s: update failed: %v\n%s %s is still installed; start it again to continue.\n",
			name, err, name, Version)
		return 1
	}
	fmt.Fprintf(stdout, "Updated to %s. Restarting ...\n", version)
	code, err := restart(exe)
	if err != nil {
		fmt.Fprintf(stderr, "%s: updated to %s, but could not restart: %v\nStart %s again.\n", name, version, err, name)
		return 1
	}
	return code
}
