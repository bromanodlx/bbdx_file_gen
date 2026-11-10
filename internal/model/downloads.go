package model

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// DownloadDir returns the user's standard download directory: $XDG_DOWNLOAD_DIR,
// else XDG_DOWNLOAD_DIR from user-dirs.dirs (localized names such as
// ~/Herunterladen), else ~/Downloads.
func DownloadDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		cfg = filepath.Join(home, ".config")
	}
	return downloadDir(home, cfg, os.Getenv("XDG_DOWNLOAD_DIR")), nil
}

func downloadDir(home, configHome, env string) string {
	if env != "" {
		return env
	}
	fallback := filepath.Join(home, "Downloads")
	f, err := os.Open(filepath.Join(configHome, "user-dirs.dirs"))
	if err != nil {
		return fallback
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, ok := strings.Cut(strings.TrimSpace(sc.Text()), "=")
		if !ok || key != "XDG_DOWNLOAD_DIR" {
			continue
		}
		if unq, err := strconv.Unquote(val); err == nil {
			val = unq
		}
		switch {
		case strings.HasPrefix(val, "$HOME/"):
			val = filepath.Join(home, strings.TrimPrefix(val, "$HOME/"))
		case !filepath.IsAbs(val):
			return fallback
		}
		if filepath.Clean(val) == filepath.Clean(home) {
			return fallback // "$HOME/" disables the directory
		}
		return val
	}
	return fallback
}
