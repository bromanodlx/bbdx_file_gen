package selfupdate_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate"
	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate/selfupdatetest"
)

var linux = selfupdate.Updater{Name: "tool", GOOS: "linux", GOARCH: "amd64"}

// release serves files under /releases/download/v2.0.0/ and a latest
// redirect to v2.0.0; extra handlers override or add paths.
func release(t *testing.T, files map[string][]byte, extra map[string]http.HandlerFunc) selfupdate.Updater {
	t.Helper()
	mux := http.NewServeMux()
	if _, ok := extra["/releases/latest"]; !ok {
		mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/releases/tag/v2.0.0", http.StatusFound)
		})
	}
	for name, data := range files {
		mux.HandleFunc("/releases/download/v2.0.0/"+name, func(w http.ResponseWriter, _ *http.Request) { w.Write(data) })
	}
	for p, h := range extra {
		mux.HandleFunc(p, h)
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u := linux
	u.ReleasesURL = srv.URL + "/releases"
	return u
}

func sums(files map[string][]byte) []byte {
	var b strings.Builder
	for name, data := range files {
		sum := sha256.Sum256(data)
		fmt.Fprintf(&b, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return []byte(b.String())
}

// withSums adds a matching checksums.txt to files.
func withSums(files map[string][]byte) map[string][]byte {
	files["checksums.txt"] = sums(files)
	return files
}

// Regression: GitHub answers release downloads with a redirect to its CDN;
// the download must follow it, also after Latest, which must not follow
// redirects itself, has used the same (caller-supplied) client.
func TestDownloadFollowsCDNRedirect(t *testing.T) {
	asset := linux.AssetName("2.0.0")
	archive := selfupdatetest.TarGz(t, map[string]string{"d/tool": "new binary"})
	cdn := map[string]http.HandlerFunc{
		"/releases/download/v2.0.0/" + asset: func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/cdn/blob", http.StatusFound)
		},
		"/cdn/blob": func(w http.ResponseWriter, _ *http.Request) { w.Write(archive) },
	}
	u := release(t, map[string][]byte{"checksums.txt": sums(map[string][]byte{asset: archive})}, cdn)
	client := &http.Client{}
	u.Client = client

	if v, err := u.Latest(context.Background()); err != nil || v != "2.0.0" {
		t.Fatalf("Latest = %q, %v", v, err)
	}
	if client.CheckRedirect != nil {
		t.Fatal("Latest changed the caller's client")
	}
	exe := installed(t, "tool")
	if err := u.Install(context.Background(), "2.0.0", exe); err != nil {
		t.Fatal(err)
	}
	if got := content(t, exe); got != "new binary" {
		t.Errorf("executable = %q", got)
	}
}

// Regression: answers that are not a released, plain version are errors,
// never an offer: no release yet (GitHub redirects to /releases), a
// pre-release tag, a non-redirect, or a tag that is no version.
func TestLatestRejectsNonReleases(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"no release": func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "/releases", http.StatusFound) },
		"prerelease": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/releases/tag/v2.0.0-rc1", http.StatusFound)
		},
		"not a version": func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "/releases/tag/nightly", http.StatusFound)
		},
		"escape in tag": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/releases/tag/v1.0.0\x1b[2J")
			w.WriteHeader(http.StatusFound)
		},
		"200":          func(w http.ResponseWriter, _ *http.Request) {},
		"server error": func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
	} {
		u := release(t, nil, map[string]http.HandlerFunc{"/releases/latest": h})
		if v, err := u.Latest(context.Background()); err == nil {
			t.Errorf("%s: Latest = %q, want error", name, v)
		}
	}
}

// Regression: only a regular file named like the executable is taken from
// an archive; a symlink or directory of that name is skipped.
func TestArchiveSymlinkOrDirectoryIsNotTheBinary(t *testing.T) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	tw.WriteHeader(&tar.Header{Name: "d/tool", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	tw.WriteHeader(&tar.Header{Name: "x/tool/", Typeflag: tar.TypeDir, Mode: 0o755})
	tw.Close()
	gz.Close()
	if _, err := selfupdate.Extract(buf.Bytes(), false, "tool"); err == nil {
		t.Error("tar: symlink or directory accepted as the executable")
	}

	// An empty directory entry would otherwise install an empty .exe.
	var zbuf bytes.Buffer
	zw := zip.NewWriter(&zbuf)
	if _, err := zw.Create("d/tool.exe/"); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	if got, err := selfupdate.Extract(zbuf.Bytes(), true, "tool.exe"); err == nil {
		t.Errorf("zip: directory accepted as the executable (%d bytes)", len(got))
	}
}

// Regression: missing or failing release files abort the update with the
// executable untouched.
func TestInstallFailsCleanlyOnServerErrors(t *testing.T) {
	asset := linux.AssetName("2.0.0")
	archive := selfupdatetest.TarGz(t, map[string]string{"d/tool": "new"})
	for _, tc := range []struct {
		name    string
		files   map[string][]byte
		wantErr string // the error names the actual cause
	}{
		{"no checksums.txt", map[string][]byte{asset: archive}, "checksums.txt: 404"},
		{"no archive", map[string][]byte{"checksums.txt": sums(map[string][]byte{asset: archive})}, asset + ": 404"},
		{"platform not listed", map[string][]byte{"checksums.txt": sums(map[string][]byte{"other.tar.gz": archive}), asset: archive}, "no checksum for " + asset},
		{"archive without binary", withSums(map[string][]byte{asset: selfupdatetest.TarGz(t, map[string]string{"d/README": "r"})}), "no tool in the archive"},
		{"not an archive", withSums(map[string][]byte{asset: []byte("<html>")}), asset + ": "},
	} {
		name := tc.name
		u := release(t, tc.files, nil)
		exe := installed(t, "tool")
		if err := u.Install(context.Background(), "2.0.0", exe); err == nil || !strings.Contains(err.Error(), tc.wantErr) {
			t.Errorf("%s: error %v, want it to mention %q", name, err, tc.wantErr)
		}
		if got := content(t, exe); got != "old binary" {
			t.Errorf("%s: executable changed to %q", name, got)
		}
		if tmp, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".*")); len(tmp) != 0 {
			t.Errorf("%s: files left behind: %v", name, tmp)
		}
	}
}

// Regression: a cancelled context (the download timeout) aborts the update.
func TestInstallHonoursContext(t *testing.T) {
	u := selfupdatetest.Serve(t, linux, selfupdatetest.Release{Version: "2.0.0", Binary: "new"})
	exe := installed(t, "tool")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := u.Install(ctx, "2.0.0", exe); err == nil {
		t.Error("no error with a cancelled context")
	}
	if got := content(t, exe); got != "old binary" {
		t.Errorf("executable changed to %q", got)
	}
}

// Regression: an executable in a directory the user cannot write to (e.g.
// /usr/local/bin) is reported, not half-replaced.
func TestInstallIntoReadOnlyDirectory(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	u := selfupdatetest.Serve(t, linux, selfupdatetest.Release{Version: "2.0.0", Binary: "new"})
	exe := installed(t, "tool")
	dir := filepath.Dir(exe)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
	err := u.Install(context.Background(), "2.0.0", exe)
	if err == nil || !strings.Contains(err.Error(), "cannot write to "+dir) {
		t.Errorf("error = %v", err)
	}
	if got := content(t, exe); got != "old binary" {
		t.Errorf("executable changed to %q", got)
	}
}

// Regression: the replaced executable keeps its permissions (plus the
// owner's execute bit), e.g. a private 0700 install stays private.
func TestInstallKeepsPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no POSIX permissions")
	}
	for _, mode := range []os.FileMode{0o700, 0o755, 0o750, 0o644} {
		u := selfupdatetest.Serve(t, linux, selfupdatetest.Release{Version: "2.0.0", Binary: "new"})
		exe := installed(t, "tool")
		if err := os.Chmod(exe, mode); err != nil {
			t.Fatal(err)
		}
		if err := u.Install(context.Background(), "2.0.0", exe); err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(exe)
		if want := mode | 0o100; st.Mode().Perm() != want {
			t.Errorf("mode %v became %v, want %v", mode, st.Mode().Perm(), want)
		}
	}
}

// Regression: on Windows a backup left by an earlier update is replaced,
// not a reason to fail.
func TestWindowsReplacesStaleBackup(t *testing.T) {
	win := selfupdate.Updater{Name: "tool", GOOS: "windows", GOARCH: "amd64"}
	u := selfupdatetest.Serve(t, win, selfupdatetest.Release{Version: "2.0.0", Binary: "new"})
	exe := installed(t, "tool.exe")
	if err := os.WriteFile(selfupdate.BackupPath(exe), []byte("ancient"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := u.Install(context.Background(), "2.0.0", exe); err != nil {
		t.Fatal(err)
	}
	if got := content(t, selfupdate.BackupPath(exe)); got != "old binary" {
		t.Errorf("backup = %q", got)
	}
}
