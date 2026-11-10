package cli

import (
	"os"
	"strings"
	"testing"

	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate"
	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate/selfupdatetest"
)

// Regression: Version must be a plain MAJOR.MINOR.PATCH, or the update
// check can never find a newer release (and silently stops working).
func TestVersionIsComparable(t *testing.T) {
	if !selfupdate.Newer("9999.0.0", Version) || !selfupdate.Newer(Version, "0.0.0") {
		t.Fatalf("Version %q is not a comparable MAJOR.MINOR.PATCH version", Version)
	}
}

// Regression: the updater looks in this project's releases for exactly the
// archive names the releases publish (taken from the v1.4.0 assets).
func TestUpdaterMatchesPublishedReleases(t *testing.T) {
	if updater.ReleasesURL != "https://github.com/bromanodlx/bbdx_file_gen/releases" || updater.Name != "bbdxfg" {
		t.Fatalf("updater = %+v", updater)
	}
	published := map[[2]string]string{
		{"darwin", "amd64"}:  "bbdxfg_1.4.0_darwin_amd64.tar.gz",
		{"darwin", "arm64"}:  "bbdxfg_1.4.0_darwin_arm64.tar.gz",
		{"linux", "amd64"}:   "bbdxfg_1.4.0_linux_amd64.tar.gz",
		{"linux", "arm64"}:   "bbdxfg_1.4.0_linux_arm64.tar.gz",
		{"windows", "amd64"}: "bbdxfg_1.4.0_windows_amd64.zip",
		{"windows", "arm64"}: "bbdxfg_1.4.0_windows_arm64.zip",
	}
	for platform, asset := range published {
		u := updater
		u.GOOS, u.GOARCH = platform[0], platform[1]
		if got := u.AssetName("1.4.0"); got != asset {
			t.Errorf("%v: asset %q, published %q", platform, got, asset)
		}
		bin := "bbdxfg"
		if platform[0] == "windows" {
			bin += ".exe"
		}
		if got := u.BinaryName(); got != bin {
			t.Errorf("%v: binary %q, want %q", platform, got, bin)
		}
	}
}

// Regression: deinstall also removes the executable a Windows update left
// behind, and only when it is there.
func TestDeinstallRemovesUpdateBackup(t *testing.T) {
	in := fakeInstall(t)
	backup := selfupdate.BackupPath(in.exe)
	if err := os.WriteFile(backup, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	code, out, errOut := runDeinstall(t, "y\n")
	if code != 0 || errOut != "" {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	mustBeGone(t, backup, in.exe)
	mustExist(t, in.userFile)
	if !strings.Contains(out, "removed "+backup) {
		t.Errorf("backup removal not reported:\n%s", out)
	}

	in = fakeInstall(t)
	if code, out, _ := runDeinstall(t, "y\n"); code != 0 || strings.Contains(out, ".old") {
		t.Errorf("without backup: exit %d, output %q", code, out)
	}
}

// Regression: only the wizard checks for updates; the one-shot commands
// never touch the network or start the TUI.
func TestOneShotCommandsDoNotCheckForUpdates(t *testing.T) {
	hook(t, &checkUpdate, func() string { t.Error("update check ran"); return "" })
	hook(t, &updater, selfupdate.Updater{ReleasesURL: "http://127.0.0.1:1/never", Name: name})
	got := stubTUI(t)
	dir := t.TempDir()
	for _, args := range [][]string{{"-version"}, {"-list"}, {"-download-dir", dir}, {"-h"}} {
		if code, _, errOut, _ := cli(t, args...); code != 0 {
			t.Errorf("%v: exit %d, stderr %q", args, code, errOut)
		}
	}
	if code, _, _ := runDeinstall(t, "n\n"); code != 0 {
		t.Errorf("deinstall: exit %d", code)
	}
	if *got != nil {
		t.Error("a one-shot command started the wizard")
	}
}

// Regression: an update chosen in the wizard that cannot be installed ends
// with exit code 1 and no restart, never a crash or a half-replaced binary.
func TestRunReportsFailedUpdate(t *testing.T) {
	exe, restarted := updateEnv(t, selfupdatetest.Release{Version: "99.0.0", Binary: "evil", CorruptChecksum: true})
	hook(t, &startTUI, acceptUpdateInWizard(t, "99.0.0"))
	code, _, errOut, _ := cli(t)
	if code != 1 || *restarted != "" || readFile(t, exe) != "old binary" {
		t.Errorf("exit %d, restarted %q, stderr %q", code, *restarted, errOut)
	}
	if !strings.Contains(errOut, "update failed") {
		t.Errorf("stderr %q", errOut)
	}
}
