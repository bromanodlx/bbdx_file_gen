package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bromanodlx/bbdx_file_gen/internal/controller"
	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate/selfupdatetest"
)

// updateEnv fakes an installed executable and a releases page whose latest
// release is rel, and records the restart instead of performing it.
func updateEnv(t *testing.T, rel selfupdatetest.Release) (exe string, restarted *string) {
	t.Helper()
	t.Setenv(noUpdateEnv, "")
	hook(t, &updater, selfupdatetest.Serve(t, updater, rel))
	exe = filepath.Join(t.TempDir(), updater.BinaryName())
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	restarted = new(string)
	hook(t, &executable, func() (string, error) { return exe, nil })
	hook(t, &restart, func(path string) (int, error) { *restarted = path; return 7, nil })
	return exe, restarted
}

func readFile(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNewerRelease(t *testing.T) {
	updateEnv(t, selfupdatetest.Release{Version: "99.0.0"})
	if got := newerRelease(); got != "99.0.0" {
		t.Errorf("newer release = %q", got)
	}
	t.Setenv(noUpdateEnv, "1")
	if got := newerRelease(); got != "" {
		t.Errorf("%s set: newer release = %q", noUpdateEnv, got)
	}
}

func TestNoNewerReleaseWhenUpToDate(t *testing.T) {
	for _, v := range []string{Version, "0.0.1"} {
		updateEnv(t, selfupdatetest.Release{Version: v})
		if got := newerRelease(); got != "" {
			t.Errorf("latest %s: newer release = %q", v, got)
		}
	}
}

func TestNoNewerReleaseWhenOffline(t *testing.T) {
	updateEnv(t, selfupdatetest.Release{Version: "99.0.0"})
	hook(t, &updater, selfupdatetest.Offline(updater))
	if got := newerRelease(); got != "" {
		t.Errorf("newer release = %q", got)
	}
}

func TestApplyUpdateReplacesAndRestarts(t *testing.T) {
	exe, restarted := updateEnv(t, selfupdatetest.Release{Version: "99.0.0", Binary: "new binary"})
	var out, errb bytes.Buffer
	if code := applyUpdate("99.0.0", &out, &errb); code != 7 || errb.Len() != 0 {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
	if got := readFile(t, exe); got != "new binary" {
		t.Errorf("executable = %q", got)
	}
	if *restarted != exe {
		t.Errorf("restarted %q, want %q", *restarted, exe)
	}
	if !strings.Contains(out.String(), "Updated to 99.0.0") {
		t.Errorf("stdout %q", out.String())
	}
}

// The executable is found through its symlink, and the real file replaced.
func TestApplyUpdateFollowsSymlink(t *testing.T) {
	exe, restarted := updateEnv(t, selfupdatetest.Release{Version: "99.0.0", Binary: "new binary"})
	link := filepath.Join(t.TempDir(), "bbdxfg")
	if err := os.Symlink(exe, link); err != nil {
		t.Skip("no symlinks:", err)
	}
	hook(t, &executable, func() (string, error) { return link, nil })
	var out, errb bytes.Buffer
	if code := applyUpdate("99.0.0", &out, &errb); code != 7 {
		t.Fatalf("code %d, stderr %q", code, errb.String())
	}
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink == 0 {
		t.Errorf("symlink replaced: %v", err)
	}
	if got := readFile(t, exe); got != "new binary" || *restarted != exe {
		t.Errorf("executable = %q, restarted %q", got, *restarted)
	}
}

func TestFailedUpdateKeepsInstallation(t *testing.T) {
	exe, restarted := updateEnv(t, selfupdatetest.Release{Version: "99.0.0", Binary: "evil binary", CorruptChecksum: true})
	var out, errb bytes.Buffer
	if code := applyUpdate("99.0.0", &out, &errb); code != 1 || *restarted != "" {
		t.Fatalf("code %d, restarted %q", code, *restarted)
	}
	if !strings.Contains(errb.String(), "checksum") || !strings.Contains(errb.String(), Version+" is still installed") {
		t.Errorf("stderr %q", errb.String())
	}
	if got := readFile(t, exe); got != "old binary" {
		t.Errorf("executable changed to %q", got)
	}
}

// The wizard gets the background check and the installed version; an update
// chosen there is installed after it closes.
func TestRunInstallsUpdateChosenInWizard(t *testing.T) {
	exe, restarted := updateEnv(t, selfupdatetest.Release{Version: "99.0.0", Binary: "new binary"})
	hook(t, &startTUI, acceptUpdateInWizard(t, "99.0.0"))
	code, _, errOut, _ := cli(t)
	if code != 7 || errOut != "" || *restarted != exe || readFile(t, exe) != "new binary" {
		t.Errorf("exit %d, stderr %q, restarted %q", code, errOut, *restarted)
	}
}

func TestRunWithoutUpdateChoiceJustExits(t *testing.T) {
	_, restarted := updateEnv(t, selfupdatetest.Release{Version: "99.0.0"})
	got := stubTUI(t)
	if code, _, errOut, _ := cli(t); code != 0 || errOut != "" || *got == nil || *restarted != "" {
		t.Errorf("exit %d, stderr %q, restarted %q", code, errOut, *restarted)
	}
}

// acceptUpdateInWizard stands in for the TUI: the background check finds
// version and the user answers y once the grace period is over.
func acceptUpdateInWizard(t *testing.T, version string) func(*controller.Controller) error {
	hook(t, &checkUpdate, func() string { return version })
	return func(c *controller.Controller) error {
		if st := c.State(); st.InstalledVersion != Version {
			t.Errorf("installed version = %q", st.InstalledVersion)
		}
		check := c.Init()
		if check == nil {
			t.Fatal("no background update check")
		}
		c.Update(check()) // the check answers
		time.Sleep(time.Second)
		c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
		return nil
	}
}
