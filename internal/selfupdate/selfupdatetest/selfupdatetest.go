// Package selfupdatetest serves fake GitHub releases for tests of programs
// that update themselves with selfupdate.
package selfupdatetest

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
	"strings"
	"testing"

	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate"
)

// Release describes the latest release a fake server offers.
type Release struct {
	Version string
	Binary  string // content of the executable in the platform's archive
	// CorruptChecksum lists a wrong checksum for the archive.
	CorruptChecksum bool
}

// Serve starts a releases page whose latest release is rel, with an archive
// for u's platform, and returns u pointed at it. The server stops when the
// test ends.
func Serve(t testing.TB, u selfupdate.Updater, rel Release) selfupdate.Updater {
	t.Helper()
	asset := u.AssetName(rel.Version)
	dir := strings.TrimSuffix(strings.TrimSuffix(asset, ".zip"), ".tar.gz")
	files := map[string]string{dir + "/README.md": "readme", dir + "/" + u.BinaryName(): rel.Binary}
	archive := TarGz(t, files)
	if strings.HasSuffix(asset, ".zip") {
		archive = Zip(t, files)
	}
	sum := sha256.Sum256(archive)
	if rel.CorruptChecksum {
		sum[0] ^= 0xff
	}
	sums := fmt.Sprintf("%s  %s\n%s  other.tar.gz\n", hex.EncodeToString(sum[:]), asset, strings.Repeat("0", 64))

	mux := http.NewServeMux()
	mux.HandleFunc("/releases/latest", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/releases/tag/v"+rel.Version, http.StatusFound)
	})
	download := "/releases/download/v" + rel.Version + "/"
	mux.HandleFunc(download+"checksums.txt", func(w http.ResponseWriter, _ *http.Request) {
		io.WriteString(w, sums)
	})
	mux.HandleFunc(download+asset, func(w http.ResponseWriter, _ *http.Request) {
		w.Write(archive)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	u.ReleasesURL = srv.URL + "/releases"
	return u
}

// Offline returns u pointed at a server that refuses connections.
func Offline(u selfupdate.Updater) selfupdate.Updater {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	u.ReleasesURL = srv.URL + "/releases"
	return u
}

// TarGz builds a .tar.gz archive of regular files.
func TarGz(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, data := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		if _, err := tw.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Zip builds a .zip archive of regular files.
func Zip(t testing.TB, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, data := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
