package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Config is the persistent user configuration.
type Config struct {
	// DownloadDir is the absolute path of the standard download directory.
	DownloadDir string `json:"download_dir"`
}

// ConfigStore loads and saves Config as JSON.
type ConfigStore struct{ Path string }

// DefaultConfigPath is $XDG_CONFIG_HOME/bbdx_file_gen/config.json
// (~/.config/... on Linux).
func DefaultConfigPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "bbdx_file_gen", "config.json"), nil
}

// Load returns the stored configuration; a missing file is an empty Config.
func (s ConfigStore) Load() (Config, error) {
	var cfg Config
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return cfg, fmt.Errorf("config %s is corrupt: %w", s.Path, err)
	}
	return cfg, nil
}

// Save writes the configuration atomically, creating its directory.
func (s ConfigStore) Save(cfg Config) error {
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.Path), ".config-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(append(data, '\n')); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path)
}

// ResolveDir interprets a directory as bash would and returns it as a clean
// absolute path: relative paths are resolved against cwd, a leading ~ means
// home, $VAR and ${VAR} are expanded ($HOME = home), "double" and 'single'
// quotes group words (single quotes suppress expansion) and \ escapes a byte.
func ResolveDir(input, cwd, home string) (string, error) {
	s := strings.TrimSpace(input)
	if s == "" {
		return "", errors.New("directory must not be empty")
	}
	var out strings.Builder
	switch {
	case s == "~" || strings.HasPrefix(s, "~/"):
		out.WriteString(home)
		s = s[1:]
	case strings.HasPrefix(s, "~"):
		return "", fmt.Errorf("%q: ~user is not supported, use an absolute path", input)
	}
	expand := func(v string) string {
		return os.Expand(v, func(name string) string {
			if name == "HOME" {
				return home
			}
			return os.Getenv(name)
		})
	}
	var word strings.Builder // pending text that is subject to $ expansion
	flush := func() { out.WriteString(expand(word.String())); word.Reset() }
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			if i+1 < len(s) {
				flush()
				i++
				out.WriteByte(s[i])
			}
		case '\'':
			end := strings.IndexByte(s[i+1:], '\'')
			if end < 0 {
				return "", fmt.Errorf("%q: unterminated single quote", input)
			}
			flush()
			out.WriteString(s[i+1 : i+1+end])
			i += end + 1
		case '"':
			j := i + 1
			for ; j < len(s) && s[j] != '"'; j++ {
				if s[j] == '\\' && j+1 < len(s) && strings.IndexByte(`$"\`, s[j+1]) >= 0 {
					flush()
					j++
					out.WriteByte(s[j])
					continue
				}
				word.WriteByte(s[j])
			}
			if j >= len(s) {
				return "", fmt.Errorf("%q: unterminated double quote", input)
			}
			i = j
		default:
			word.WriteByte(c)
		}
	}
	flush()
	p := out.String()
	if !filepath.IsAbs(p) {
		p = filepath.Join(cwd, p)
	}
	return filepath.Clean(p), nil
}

// CheckDir verifies that dir exists, is a directory and is writable.
func CheckDir(dir string) error {
	st, err := os.Stat(dir)
	switch {
	case errors.Is(err, os.ErrNotExist):
		return fmt.Errorf("%s does not exist", dir)
	case err != nil:
		return fmt.Errorf("%s is not reachable: %w", dir, err)
	case !st.IsDir():
		return fmt.Errorf("%s is not a directory", dir)
	}
	f, err := os.CreateTemp(dir, ".bbdx-write-test-*")
	if err != nil {
		return fmt.Errorf("%s is not writable", dir)
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

// CompleteDir completes the last path component of input to a matching
// directory like bash's tab completion: a unique match gets a trailing "/",
// several matches are completed to their longest common prefix.
func CompleteDir(input, cwd, home string) string {
	head, partial := "", input
	if i := strings.LastIndexByte(input, '/'); i >= 0 {
		head, partial = input[:i+1], input[i+1:]
	}
	dir := head
	switch {
	case dir == "":
		dir = cwd
	case dir == "~/" || strings.HasPrefix(dir, "~/"):
		dir = filepath.Join(home, dir[2:])
	case !filepath.IsAbs(dir):
		dir = filepath.Join(cwd, dir)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return input
	}
	var names []string
	for _, e := range entries {
		n := e.Name()
		if !strings.HasPrefix(n, partial) || (strings.HasPrefix(n, ".") && !strings.HasPrefix(partial, ".")) {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, n)); err == nil && st.IsDir() { // follows symlinks
			names = append(names, n)
		}
	}
	sort.Strings(names)
	switch len(names) {
	case 0:
		return input
	case 1:
		return head + shellEscape(names[0]) + "/"
	}
	prefix := names[0]
	for _, n := range names[1:] {
		for !strings.HasPrefix(n, prefix) {
			prefix = prefix[:len(prefix)-1]
		}
	}
	if len(prefix) <= len(partial) {
		return input
	}
	return head + shellEscape(prefix)
}

func shellEscape(name string) string {
	var b strings.Builder
	for _, r := range name {
		if strings.ContainsRune(" \\'\"$", r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// ConfigSaver is the part of a configuration store SetDownloadDir needs.
type ConfigSaver interface{ Save(Config) error }

// SetDownloadDir resolves input like bash (see ResolveDir), verifies the
// directory is reachable and stores it as the standard download directory.
func SetDownloadDir(store ConfigSaver, input, cwd, home string) (string, error) {
	dir, err := ResolveDir(input, cwd, home)
	if err != nil {
		return "", err
	}
	if err := CheckDir(dir); err != nil {
		return "", err
	}
	if err := store.Save(Config{DownloadDir: dir}); err != nil {
		return "", err
	}
	return dir, nil
}
