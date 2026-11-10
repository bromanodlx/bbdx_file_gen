package selfupdate_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate"
	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate/selfupdatetest"
)

var base = selfupdate.Updater{Name: "tool"}

// installed writes a fake executable and returns its path.
func installed(t *testing.T, name string) string {
	t.Helper()
	exe := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	return exe
}

func content(t *testing.T, p string) string {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestNewer(t *testing.T) {
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"1.4.1", "1.4.0", true},
		{"1.10.0", "1.9.9", true},
		{"2.0.0", "1.99.99", true},
		{"v1.5.0", "1.4.1", true},
		{"1.4.0", "1.4.0", false},
		{"1.4.0", "1.4.1", false},
		{"1.4", "1.3.0", false},
		{"1.4.0-rc1", "1.3.0", false},
		{"garbage", "1.0.0", false},
	} {
		if got := selfupdate.Newer(tc.a, tc.b); got != tc.want {
			t.Errorf("Newer(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

func TestNames(t *testing.T) {
	for _, tc := range []struct{ goos, goarch, asset, bin string }{
		{"linux", "amd64", "tool_1.4.0_linux_amd64.tar.gz", "tool"},
		{"darwin", "arm64", "tool_1.4.0_darwin_arm64.tar.gz", "tool"},
		{"windows", "arm64", "tool_1.4.0_windows_arm64.zip", "tool.exe"},
	} {
		u := selfupdate.Updater{Name: "tool", GOOS: tc.goos, GOARCH: tc.goarch}
		if got := u.AssetName("1.4.0"); got != tc.asset {
			t.Errorf("%s: asset %q", tc.goos, got)
		}
		if got := u.BinaryName(); got != tc.bin {
			t.Errorf("%s: binary %q", tc.goos, got)
		}
	}
}

func TestLatest(t *testing.T) {
	u := selfupdatetest.Serve(t, base, selfupdatetest.Release{Version: "2.3.4"})
	if got, err := u.Latest(context.Background()); err != nil || got != "2.3.4" {
		t.Errorf("Latest = %q, %v", got, err)
	}
	if _, err := selfupdatetest.Offline(base).Latest(context.Background()); err == nil {
		t.Error("offline: no error")
	}
}

// Every platform's archive installs: tar.gz in place, zip with the old
// executable moved aside as on Windows.
func TestInstall(t *testing.T) {
	for _, goos := range []string{"linux", "darwin", "windows"} {
		u := selfupdate.Updater{Name: "tool", GOOS: goos, GOARCH: "amd64"}
		u = selfupdatetest.Serve(t, u, selfupdatetest.Release{Version: "2.0.0", Binary: "new binary"})
		exe := installed(t, u.BinaryName())
		if err := u.Install(context.Background(), "2.0.0", exe); err != nil {
			t.Fatalf("%s: %v", goos, err)
		}
		if got := content(t, exe); got != "new binary" {
			t.Errorf("%s: executable = %q", goos, got)
		}
		if st, _ := os.Stat(exe); st.Mode().Perm()&0o100 == 0 {
			t.Errorf("%s: not executable: %v", goos, st.Mode())
		}
		backup := selfupdate.BackupPath(exe)
		if _, err := os.Stat(backup); (err == nil) != (goos == "windows") {
			t.Errorf("%s: backup present = %v", goos, err == nil)
		}
		if goos == "windows" && content(t, backup) != "old binary" {
			t.Errorf("windows: backup = %q", content(t, backup))
		}
		if tmp, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".*.tmp")); len(tmp) != 0 {
			t.Errorf("%s: temp files left: %v", goos, tmp)
		}
	}
}

func TestInstallKeepsExecutableOnError(t *testing.T) {
	for name, rel := range map[string]selfupdatetest.Release{
		"bad checksum":    {Version: "2.0.0", Binary: "evil binary", CorruptChecksum: true},
		"missing version": {Version: "3.0.0", Binary: "new binary"}, // 2.0.0 is not served
	} {
		u := selfupdatetest.Serve(t, base, rel)
		exe := installed(t, u.BinaryName())
		err := u.Install(context.Background(), "2.0.0", exe)
		if err == nil {
			t.Fatalf("%s: no error", name)
		}
		if name == "bad checksum" && !strings.Contains(err.Error(), "checksum") {
			t.Errorf("%s: error %v", name, err)
		}
		if got := content(t, exe); got != "old binary" {
			t.Errorf("%s: executable changed to %q", name, got)
		}
	}
}
