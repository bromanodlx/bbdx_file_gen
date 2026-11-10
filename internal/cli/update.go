package cli

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// noUpdateEnv turns the start-up update check off when set to any value.
const noUpdateEnv = "BBDXFG_NO_UPDATE_CHECK"

// Hooks for the update check; tests replace them.
var (
	// releasesURL is the GitHub releases page of the project.
	releasesURL = "https://github.com/bromanodlx/bbdx_file_gen/releases"
	// restart replaces the running program with the executable at exe,
	// started with the same arguments. It only returns on failure, or with
	// the exit code of the restarted program where the platform cannot
	// replace a running process (Windows).
	restart = restartProcess
)

const (
	// checkTimeout bounds the background check; the wizard never waits
	// for it.
	checkTimeout    = 15 * time.Second
	downloadTimeout = 2 * time.Minute
	maxBinarySize   = 200 << 20
)

// newerRelease returns the version of the latest GitHub release if it is
// newer than Version, and "" otherwise, including when the check is turned
// off or fails (offline, rate-limited, ...): a missing network must never
// get in the way of the tool. The wizard runs it in the background.
func newerRelease() string {
	if os.Getenv(noUpdateEnv) != "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), checkTimeout)
	defer cancel()
	latest, err := latestVersion(ctx)
	if err != nil || !newer(latest, Version) {
		return ""
	}
	return latest
}

// applyUpdate installs version after the wizard has closed and restarts the
// tool. It returns the process exit code: the restarted program's where the
// platform keeps this process around (Windows), 1 if the update failed.
func applyUpdate(version string, stdout, stderr io.Writer) int {
	fmt.Fprintf(stdout, "Downloading %s %s ...\n", name, version)
	exe, err := installRelease(version)
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

// latestVersion returns the version of the latest GitHub release, without
// the leading "v". It reads the redirect of releases/latest, which needs no
// API token and is not subject to the API rate limit.
func latestVersion(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, releasesURL+"/latest", nil)
	if err != nil {
		return "", err
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	resp.Body.Close()
	loc := resp.Header.Get("Location")
	tag := path.Base(loc)
	if resp.StatusCode/100 != 3 || !strings.Contains(loc, "/releases/tag/") || parseVersion(tag) == nil {
		return "", fmt.Errorf("unexpected answer for the latest release: %s %q", resp.Status, loc)
	}
	return strings.TrimPrefix(tag, "v"), nil
}

// parseVersion splits "1.2.3" or "v1.2.3" into its numbers; nil if it is
// not such a version.
func parseVersion(v string) []int {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		nums[i] = n
	}
	return nums
}

// newer reports whether version a is later than version b.
func newer(a, b string) bool {
	va, vb := parseVersion(a), parseVersion(b)
	if va == nil || vb == nil {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

// installRelease downloads the release archive of version for this platform,
// checks it against the release's checksums.txt, and puts its executable in
// place of the running one. It returns the path of the new executable.
func installRelease(version string) (string, error) {
	exe, err := executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate the %s executable: %w", name, err)
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}

	ctx, cancel := context.WithTimeout(context.Background(), downloadTimeout)
	defer cancel()
	asset := assetName(version, runtime.GOOS, runtime.GOARCH)
	base := releasesURL + "/download/v" + version + "/"
	sums, err := download(ctx, base+"checksums.txt", 1<<20)
	if err != nil {
		return "", err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return "", err
	}
	archive, err := download(ctx, base+asset, maxBinarySize)
	if err != nil {
		return "", err
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return "", fmt.Errorf("%s does not match its checksum", asset)
	}
	bin, err := extractBinary(archive, strings.HasSuffix(asset, ".zip"), binaryName(runtime.GOOS))
	if err != nil {
		return "", fmt.Errorf("%s: %w", asset, err)
	}
	return exe, replaceExecutable(exe, bin)
}

// assetName is the release archive for a platform, e.g.
// bbdxfg_1.4.0_linux_amd64.tar.gz.
func assetName(version, goos, goarch string) string {
	ext := ".tar.gz"
	if goos == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("%s_%s_%s_%s%s", name, version, goos, goarch, ext)
}

// binaryName is the executable's file name inside the archive.
func binaryName(goos string) string {
	if goos == "windows" {
		return name + ".exe"
	}
	return name
}

// download fetches url, failing for a non-200 answer or more than limit bytes.
func download(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: %s", url, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, fmt.Errorf("download %s: %w", url, err)
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("download %s: larger than %d bytes", url, limit)
	}
	return data, nil
}

// checksumFor returns the SHA-256 listed for file in a checksums.txt.
func checksumFor(sums []byte, file string) (string, error) {
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && strings.TrimPrefix(f[1], "*") == file {
			return strings.ToLower(f[0]), nil
		}
	}
	return "", fmt.Errorf("no checksum for %s (is this platform released?)", file)
}

// extractBinary returns the content of the file named bin in a .zip or
// .tar.gz archive, wherever in the archive it is.
func extractBinary(archive []byte, isZip bool, bin string) ([]byte, error) {
	if isZip {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if path.Base(f.Name) == bin && !f.FileInfo().IsDir() {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return readLimited(rc)
			}
		}
		return nil, fmt.Errorf("no %s in the archive", bin)
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("no %s in the archive", bin)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == bin {
			return readLimited(tr)
		}
	}
}

func readLimited(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxBinarySize+1))
	if err == nil && len(data) > maxBinarySize {
		err = fmt.Errorf("executable larger than %d bytes", maxBinarySize)
	}
	return data, err
}

// replaceExecutable puts bin in place of exe. The new executable is written
// next to it and renamed over it, so an interrupted update leaves the old
// one intact. Windows cannot overwrite a running .exe but can rename it, so
// there the old one is first moved aside to exe.old (removed on the next
// update).
func replaceExecutable(exe string, bin []byte) error {
	dir := filepath.Dir(exe)
	mode := os.FileMode(0o755)
	if st, err := os.Stat(exe); err == nil {
		mode = st.Mode().Perm() | 0o100
	}
	tmp, err := os.CreateTemp(dir, "."+name+"-update-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			return err
		}
		if err := os.Rename(tmp.Name(), exe); err != nil {
			_ = os.Rename(old, exe) // put the old version back
			return err
		}
		return nil
	}
	return os.Rename(tmp.Name(), exe)
}
