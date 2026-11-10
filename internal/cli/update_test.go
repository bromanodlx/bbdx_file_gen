package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bromanodlx/bbdx_file_gen/internal/controller"
)

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
		if got := newer(tc.a, tc.b); got != tc.want {
			t.Errorf("newer(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

func tarGz(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for n, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: n, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(data))
	}
	tw.Close()
	gz.Close()
	return buf.Bytes()
}

func zipped(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for n, data := range files {
		w, err := zw.Create(n)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(data))
	}
	zw.Close()
	return buf.Bytes()
}

// fakeRelease serves a GitHub-like releases page whose latest release is
// version, with an archive for this platform containing newBinary.
// corrupt breaks the archive's checksum.
func fakeRelease(t *testing.T, version, newBinary string, corrupt bool) {
	t.Helper()
	asset := assetName(version, runtime.GOOS, runtime.GOARCH)
	dir := strings.TrimSuffix(strings.TrimSuffix(asset, ".zip"), ".tar.gz")
	files := map[string]string{dir + "/README.md": "readme", dir + "/" + binaryName(runtime.GOOS): newBinary}
	archive := tarGz(t, files)
	if runtime.GOOS == "windows" {
		archive = zipped(t, files)
	}
	sum := sha256.Sum256(archive)
	if corrupt {
		sum[0] ^= 0xff
	}
	sums := fmt.Sprintf("%s  %s\n%s  other.tar.gz\n", hex.EncodeToString(sum[:]), asset, strings.Repeat("0", 64))

	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v"+version, http.StatusFound)
	})
	mux.HandleFunc("/releases/download/v"+version+"/checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, sums)
	})
	mux.HandleFunc("/releases/download/v"+version+"/"+asset, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	hook(t, &releasesURL, srv.URL+"/releases")
}

// updateEnv fakes an installed executable and records the restart instead
// of performing it.
func updateEnv(t *testing.T) (exe string, restarted *string) {
	t.Helper()
	exe = filepath.Join(t.TempDir(), binaryName(runtime.GOOS))
	if err := os.WriteFile(exe, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	restarted = new(string)
	t.Setenv(noUpdateEnv, "")
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
	updateEnv(t)
	fakeRelease(t, "99.0.0", "new binary", false)
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
		updateEnv(t)
		fakeRelease(t, v, "new binary", false)
		if got := newerRelease(); got != "" {
			t.Errorf("latest %s: newer release = %q", v, got)
		}
	}
}

func TestNoNewerReleaseWhenOffline(t *testing.T) {
	updateEnv(t)
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close() // connection refused
	hook(t, &releasesURL, srv.URL+"/releases")
	if got := newerRelease(); got != "" {
		t.Errorf("newer release = %q", got)
	}
}

func TestApplyUpdateReplacesAndRestarts(t *testing.T) {
	exe, restarted := updateEnv(t)
	fakeRelease(t, "99.0.0", "new binary", false)
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
	if st, _ := os.Stat(exe); runtime.GOOS != "windows" && st.Mode().Perm()&0o100 == 0 {
		t.Errorf("new executable not executable: %v", st.Mode())
	}
	if leftovers, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), "*.tmp")); len(leftovers) != 0 {
		t.Errorf("temp files left: %v", leftovers)
	}
	if !strings.Contains(out.String(), "Updated to 99.0.0") {
		t.Errorf("stdout %q", out.String())
	}
}

func TestBadChecksumKeepsInstallation(t *testing.T) {
	exe, restarted := updateEnv(t)
	fakeRelease(t, "99.0.0", "evil binary", true)
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
	exe, restarted := updateEnv(t)
	fakeRelease(t, "99.0.0", "new binary", false)
	hook(t, &checkUpdate, func() string { return "99.0.0" })
	hook(t, &startTUI, func(c *controller.Controller) error {
		if st := c.State(); st.InstalledVersion != Version {
			t.Errorf("installed version = %q", st.InstalledVersion)
		}
		if c.Init() == nil {
			t.Error("no background update check")
		}
		c.Update(c.Init()()) // the check answers
		time.Sleep(time.Second)
		c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
		return nil
	})
	code, _, errOut, _ := cli(t)
	if code != 7 || errOut != "" || *restarted != exe || readFile(t, exe) != "new binary" {
		t.Errorf("exit %d, stderr %q, restarted %q", code, errOut, *restarted)
	}
}

func TestRunWithoutUpdateChoiceJustExits(t *testing.T) {
	_, restarted := updateEnv(t)
	got := stubTUI(t)
	if code, _, errOut, _ := cli(t); code != 0 || errOut != "" || *got == nil || *restarted != "" {
		t.Errorf("exit %d, stderr %q, restarted %q", code, errOut, *restarted)
	}
}

func TestChecksumFor(t *testing.T) {
	sums := []byte("AB12  a.tar.gz\ncd34 *b.zip\n")
	if got, err := checksumFor(sums, "a.tar.gz"); err != nil || got != "ab12" {
		t.Errorf("a: %q %v", got, err)
	}
	if got, err := checksumFor(sums, "b.zip"); err != nil || got != "cd34" {
		t.Errorf("b: %q %v", got, err)
	}
	if _, err := checksumFor(sums, "c.zip"); err == nil {
		t.Error("missing entry accepted")
	}
}

func TestExtractBinary(t *testing.T) {
	files := map[string]string{"d/README.md": "r", "d/bbdxfg": "unix", "d/bbdxfg.exe": "win"}
	if got, err := extractBinary(tarGz(t, files), false, "bbdxfg"); err != nil || string(got) != "unix" {
		t.Errorf("tar: %q %v", got, err)
	}
	if got, err := extractBinary(zipped(t, files), true, "bbdxfg.exe"); err != nil || string(got) != "win" {
		t.Errorf("zip: %q %v", got, err)
	}
	if _, err := extractBinary(tarGz(t, map[string]string{"d/README.md": "r"}), false, "bbdxfg"); err == nil {
		t.Error("archive without binary accepted")
	}
}

func TestAssetName(t *testing.T) {
	if got := assetName("1.4.0", "linux", "amd64"); got != "bbdxfg_1.4.0_linux_amd64.tar.gz" {
		t.Error(got)
	}
	if got := assetName("1.4.0", "windows", "arm64"); got != "bbdxfg_1.4.0_windows_arm64.zip" {
		t.Error(got)
	}
}
