// Package selfupdate keeps an installed program up to date from its GitHub
// releases: it finds the latest release, downloads and verifies the archive
// for the running platform, and swaps the executable in place.
//
// Releases are expected to follow this layout, for program name N, version V,
// GOOS os and GOARCH arch:
//
//	<releases>/latest                  redirects to <releases>/tag/vV
//	<releases>/download/vV/checksums.txt   "<sha256>  <file>" lines
//	<releases>/download/vV/N_V_os_arch.tar.gz  (.zip on Windows),
//	                                       containing N (N.exe on Windows)
package selfupdate

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"path"
	"runtime"
	"strings"
)

// Updater updates one program from one GitHub releases page.
type Updater struct {
	// ReleasesURL is the releases page, e.g.
	// https://github.com/OWNER/REPO/releases.
	ReleasesURL string
	// Name is the program name used in archive and executable names.
	Name string
	// GOOS and GOARCH select the archive; empty means the running platform.
	GOOS, GOARCH string
	// Client makes the requests; nil means http.DefaultClient.
	Client *http.Client
}

const (
	checksumsFile = "checksums.txt"
	maxChecksums  = 1 << 20
	maxBinarySize = 200 << 20
)

func (u Updater) goos() string   { return or(u.GOOS, runtime.GOOS) }
func (u Updater) goarch() string { return or(u.GOARCH, runtime.GOARCH) }

func or(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func (u Updater) client() *http.Client {
	if u.Client != nil {
		return u.Client
	}
	return http.DefaultClient
}

// Latest returns the version of the latest release, without the leading
// "v". It reads the redirect of releases/latest, which needs no API token
// and is not subject to the GitHub API rate limit.
func (u Updater) Latest(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, u.ReleasesURL+"/latest", nil)
	if err != nil {
		return "", err
	}
	noFollow := *u.client()
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := noFollow.Do(req)
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

// Install downloads the archive of version for the platform, verifies it
// against the release's checksums.txt and puts its executable in place of
// exe. exe is left untouched on any error.
func (u Updater) Install(ctx context.Context, version, exe string) error {
	asset := u.AssetName(version)
	base := u.ReleasesURL + "/download/v" + version + "/"
	sums, err := u.download(ctx, base+checksumsFile, maxChecksums)
	if err != nil {
		return err
	}
	want, err := checksumFor(sums, asset)
	if err != nil {
		return err
	}
	archive, err := u.download(ctx, base+asset, maxBinarySize)
	if err != nil {
		return err
	}
	if got := sha256.Sum256(archive); hex.EncodeToString(got[:]) != want {
		return fmt.Errorf("%s does not match its checksum", asset)
	}
	bin, err := extract(archive, strings.HasSuffix(asset, ".zip"), u.BinaryName())
	if err != nil {
		return fmt.Errorf("%s: %w", asset, err)
	}
	return replace(exe, bin, u.goos() == "windows")
}

// AssetName is the release archive for version on the platform, e.g.
// bbdxfg_1.4.0_linux_amd64.tar.gz.
func (u Updater) AssetName(version string) string {
	ext := ".tar.gz"
	if u.goos() == "windows" {
		ext = ".zip"
	}
	return fmt.Sprintf("%s_%s_%s_%s%s", u.Name, version, u.goos(), u.goarch(), ext)
}

// BinaryName is the executable's file name inside the archive.
func (u Updater) BinaryName() string {
	if u.goos() == "windows" {
		return u.Name + ".exe"
	}
	return u.Name
}
