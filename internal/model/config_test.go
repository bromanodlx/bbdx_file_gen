package model

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveDirBashLike(t *testing.T) {
	cwd, home := "/work/proj", "/home/u"
	t.Setenv("BBDX_TEST_VAR", "/srv/data")
	cases := map[string]string{
		"/abs/path":           "/abs/path",
		"/abs/path/":          "/abs/path",
		"rel":                 "/work/proj/rel",
		"./rel/../other":      "/work/proj/other",
		".":                   "/work/proj",
		"..":                  "/work",
		"~":                   "/home/u",
		"~/Downloads":         "/home/u/Downloads",
		"$HOME/x":             "/home/u/x",
		"${BBDX_TEST_VAR}/in": "/srv/data/in",
		`"My Files"`:          "/work/proj/My Files",
		`'~/quoted'`:          "/work/proj/~/quoted", // single quotes suppress expansion, as in bash
		`My\ Files`:           "/work/proj/My Files",
		"  ~/trimmed  ":       "/home/u/trimmed",
	}
	for in, want := range cases {
		got, err := ResolveDir(in, cwd, home)
		if err != nil {
			t.Errorf("ResolveDir(%q) error: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ResolveDir(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveDirUsesHomeForHOMEVariable(t *testing.T) {
	t.Setenv("HOME", "/somewhere/else")
	if got, _ := ResolveDir("$HOME/x", "/w", "/home/u"); got != "/home/u/x" {
		t.Errorf("got %q", got)
	}
}

func TestResolveDirRejects(t *testing.T) {
	for _, in := range []string{"", "   ", "~other/x", `"unterminated`} {
		if _, err := ResolveDir(in, "/w", "/h"); err == nil {
			t.Errorf("ResolveDir(%q) expected error", in)
		}
	}
}

func TestCheckDir(t *testing.T) {
	dir := t.TempDir()
	if err := CheckDir(dir); err != nil {
		t.Errorf("writable dir rejected: %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Error("CheckDir must not leave files behind")
	}
	if err := CheckDir(filepath.Join(dir, "missing")); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("missing dir: %v", err)
	}
	file := filepath.Join(dir, "file")
	os.WriteFile(file, nil, 0o644)
	if err := CheckDir(file); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("file: %v", err)
	}
	ro := filepath.Join(dir, "ro")
	os.Mkdir(ro, 0o555)
	if os.Geteuid() != 0 {
		if err := CheckDir(ro); err == nil || !strings.Contains(err.Error(), "not writable") {
			t.Errorf("read-only dir: %v", err)
		}
	}
}

func TestConfigStoreRoundTrip(t *testing.T) {
	store := ConfigStore{Path: filepath.Join(t.TempDir(), "sub", "config.json")}
	cfg, err := store.Load()
	if err != nil || cfg.DownloadDir != "" {
		t.Fatalf("missing config must load empty without error: %+v, %v", cfg, err)
	}
	if err := store.Save(Config{DownloadDir: "/data/dl"}); err != nil {
		t.Fatal(err)
	}
	cfg, err = store.Load()
	if err != nil || cfg.DownloadDir != "/data/dl" {
		t.Errorf("loaded %+v, %v", cfg, err)
	}
}

func TestConfigStoreCorruptFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	os.WriteFile(p, []byte("{not json"), 0o644)
	if _, err := (ConfigStore{Path: p}).Load(); err == nil {
		t.Error("corrupt config must be reported")
	}
}

func TestDefaultConfigPath(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/cfg")
	p, err := DefaultConfigPath()
	if err != nil || p != "/cfg/bbdx_file_gen/config.json" {
		t.Errorf("DefaultConfigPath = %q, %v", p, err)
	}
}

func TestCompleteDir(t *testing.T) {
	base := t.TempDir()
	for _, d := range []string{"Downloads", "Documents", "Desktop", "music"} {
		os.Mkdir(filepath.Join(base, d), 0o755)
	}
	os.WriteFile(filepath.Join(base, "Dfile"), nil, 0o644) // files are never offered
	cases := map[string]string{
		"Dow":     "Downloads/",
		"Do":      "Do", // ambiguous: Documents, Downloads -> common prefix only
		"Doc":     "Documents/",
		"m":       "music/",
		"x":       "x",
		"Desktop": "Desktop/",
	}
	for in, want := range cases {
		if got := CompleteDir(in, base, "/h"); got != want {
			t.Errorf("CompleteDir(%q) = %q, want %q", in, got, want)
		}
	}
	if got := CompleteDir(base+"/Dow", "/elsewhere", "/h"); got != base+"/Downloads/" {
		t.Errorf("absolute completion = %q", got)
	}
	if got := CompleteDir("~/Dow", "/elsewhere", base); got != "~/Downloads/" {
		t.Errorf("tilde completion keeps the ~: %q", got)
	}
}

func TestSetDownloadDir(t *testing.T) {
	cwd := t.TempDir()
	os.Mkdir(filepath.Join(cwd, "dl"), 0o755)
	store := ConfigStore{Path: filepath.Join(t.TempDir(), "config.json")}
	got, err := SetDownloadDir(store, "dl", cwd, "/h")
	if err != nil || got != filepath.Join(cwd, "dl") {
		t.Fatalf("SetDownloadDir = %q, %v", got, err)
	}
	if cfg, _ := store.Load(); cfg.DownloadDir != got {
		t.Errorf("stored %q", cfg.DownloadDir)
	}
	if _, err := SetDownloadDir(store, "missing", cwd, "/h"); err == nil {
		t.Error("an unreachable directory must not be stored")
	}
	if cfg, _ := store.Load(); cfg.DownloadDir != got {
		t.Errorf("a failed change must keep the old value, got %q", cfg.DownloadDir)
	}
}

// Completion reads its input with ResolveDir's rules, so text produced by a
// previous completion (escapes) and other bash-like spellings keep working.
func TestCompleteDirUnderstandsEscapesQuotesAndVariables(t *testing.T) {
	cwd, home := t.TempDir(), t.TempDir()
	for _, d := range []string{"my dir/subdir", `it's "q"/inner`, "plain/alpha", "plain/alpine"} {
		if err := os.MkdirAll(filepath.Join(cwd, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(home, "docs", "letters"), 0o755)
	t.Setenv("BBDX_TEST_DIR", filepath.Join(cwd, "my dir"))
	for in, want := range map[string]string{
		"my":                     `my\ dir/`,
		`my\ dir/`:               `my\ dir/subdir/`,
		`my\ dir/su`:             `my\ dir/subdir/`,
		`"my dir"/su`:            `"my dir"/subdir/`,
		`'my dir'/su`:            `'my dir'/subdir/`,
		`my\ d`:                  `my\ dir/`,
		`"my d`:                  `"my d`, // unterminated quote: left alone
		`$BBDX_TEST_DIR/su`:      `$BBDX_TEST_DIR/subdir/`,
		`${BBDX_TEST_DIR}/`:      `${BBDX_TEST_DIR}/subdir/`,
		"it":                     `it\'s\ \"q\"/`,
		`it\'s\ \"q\"/in`:        `it\'s\ \"q\"/inner/`,
		"~/do":                   "~/docs/",
		"$HOME/docs/l":           "$HOME/docs/letters/",
		"plain/al":               "plain/alp",
		"plain/alp":              "plain/alp", // ambiguous, nothing more in common
		"~nobody/x":              "~nobody/x",
		filepath.Join(cwd, "pl"): filepath.Join(cwd, "plain") + "/",
	} {
		if got := CompleteDir(in, cwd, home); got != want {
			t.Errorf("CompleteDir(%q) = %q, want %q", in, got, want)
		}
	}
	// Whatever completion produces resolves to the directory it names.
	got, err := ResolveDir(CompleteDir(`it\'s\ \"q\"/in`, cwd, home), cwd, home)
	if err != nil || got != filepath.Join(cwd, `it's "q"`, "inner") {
		t.Errorf("completion does not round-trip: %q, %v", got, err)
	}
}

func TestConfigStoreSaveIsPrivateAndLeavesNoTemp(t *testing.T) {
	dir := t.TempDir()
	s := ConfigStore{Path: filepath.Join(dir, "sub", "config.json")}
	for _, d := range []string{"/a", "/b"} { // the second save replaces the first
		if err := s.Save(Config{DownloadDir: d}); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := s.Load()
	st, _ := os.Stat(s.Path)
	if err != nil || cfg.DownloadDir != "/b" || st.Mode().Perm() != 0o600 {
		t.Errorf("cfg %+v, err %v, mode %v", cfg, err, st.Mode().Perm())
	}
	data, _ := os.ReadFile(s.Path)
	if string(data) != "{\n  \"download_dir\": \"/b\"\n}\n" {
		t.Errorf("config file format changed: %q", data)
	}
	noTempFiles(t, filepath.Dir(s.Path))
}
