package model

import (
	"os"
	"path/filepath"
	"testing"
)

func writeUserDirs(t *testing.T, configHome, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(configHome, "user-dirs.dirs"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestDownloadDirDefaultsToHomeDownloads(t *testing.T) {
	home := t.TempDir()
	if got := downloadDir(home, t.TempDir(), ""); got != filepath.Join(home, "Downloads") {
		t.Errorf("got %q", got)
	}
}

func TestDownloadDirReadsXDGUserDirs(t *testing.T) {
	home, cfg := t.TempDir(), t.TempDir()
	writeUserDirs(t, cfg, "# comment\nXDG_DESKTOP_DIR=\"$HOME/Schreibtisch\"\nXDG_DOWNLOAD_DIR=\"$HOME/Herunterladen\"\n")
	if got := downloadDir(home, cfg, ""); got != filepath.Join(home, "Herunterladen") {
		t.Errorf("got %q", got)
	}
}

func TestDownloadDirAcceptsAbsoluteXDGPath(t *testing.T) {
	cfg := t.TempDir()
	writeUserDirs(t, cfg, "XDG_DOWNLOAD_DIR=\"/data/dl\"\n")
	if got := downloadDir(t.TempDir(), cfg, ""); got != "/data/dl" {
		t.Errorf("got %q", got)
	}
}

func TestDownloadDirEnvironmentWins(t *testing.T) {
	cfg := t.TempDir()
	writeUserDirs(t, cfg, "XDG_DOWNLOAD_DIR=\"$HOME/Herunterladen\"\n")
	if got := downloadDir(t.TempDir(), cfg, "/env/dl"); got != "/env/dl" {
		t.Errorf("got %q", got)
	}
}

func TestDownloadDirIgnoresHomeItself(t *testing.T) {
	// xdg-user-dirs uses "$HOME/" to disable a directory; fall back to ~/Downloads.
	home, cfg := t.TempDir(), t.TempDir()
	writeUserDirs(t, cfg, "XDG_DOWNLOAD_DIR=\"$HOME/\"\n")
	if got := downloadDir(home, cfg, ""); got != filepath.Join(home, "Downloads") {
		t.Errorf("got %q", got)
	}
}
