package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"bbdx_file_gen/internal/controller"
)

// cli runs the command line with a private config and home directory.
func cli(t *testing.T, args ...string) (code int, stdout, stderr string, cfgDir string) {
	t.Helper()
	cfgDir = t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", cfgDir)
	t.Setenv("HOME", t.TempDir())
	var out, errb bytes.Buffer
	code = Run("bbdx_file_gen", args, &out, &errb)
	return code, out.String(), errb.String(), cfgDir
}

// stubTUI replaces the interactive program and records the controller.
func stubTUI(t *testing.T) **controller.Controller {
	t.Helper()
	var got *controller.Controller
	orig := startTUI
	startTUI = func(c *controller.Controller) error { got = c; return nil }
	t.Cleanup(func() { startTUI = orig })
	return &got
}

func TestListMatchesGolden(t *testing.T) {
	code, out, _, _ := cli(t, "-list")
	want, err := os.ReadFile("testdata/golden_list.txt")
	if err != nil {
		t.Fatal(err)
	}
	if code != 0 || out != string(want) {
		t.Errorf("exit %d, -list output differs from the 1.1.0 baseline:\n%s", code, out)
	}
}

func TestVersion(t *testing.T) {
	if code, out, _, _ := cli(t, "-version"); code != 0 || out != "bbdx_file_gen "+Version+"\n" {
		t.Errorf("exit %d, output %q", code, out)
	}
}

func TestSetDownloadDirStoresAbsolutePath(t *testing.T) {
	dl := t.TempDir()
	t.Chdir(filepath.Dir(dl))
	code, out, errOut, cfgDir := cli(t, "-download-dir", filepath.Base(dl))
	if code != 0 || out != "standard download directory set to "+dl+"\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	path := filepath.Join(cfgDir, "bbdx_file_gen", "config.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		DownloadDir string `json:"download_dir"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil || cfg.DownloadDir != dl {
		t.Errorf("config %s = %s (%v)", path, data, err)
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o600 {
		t.Errorf("config mode %v, want 0600", st.Mode().Perm())
	}
}

func TestSetUnreachableDownloadDirFails(t *testing.T) {
	code, out, errOut, cfgDir := cli(t, "-download-dir", "/nonexistent/bbdx")
	if code != 1 || out != "" || errOut != "bbdx_file_gen: /nonexistent/bbdx does not exist\n" {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(cfgDir, "bbdx_file_gen", "config.json")); err == nil {
		t.Error("a config was written for an unreachable directory")
	}
}

func TestUsageErrors(t *testing.T) {
	for _, args := range [][]string{{"-bogus"}, {"report.pdf"}, {"-plain", "extra"}, {"-download-dir"}} {
		code, out, errOut, _ := cli(t, args...)
		if code != 2 || out != "" || !strings.Contains(errOut, "Usage of bbdx_file_gen") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
	}
	if _, _, errOut, _ := cli(t, "report.pdf"); !strings.Contains(errOut, `unexpected argument "report.pdf"`) {
		t.Errorf("stray argument not named: %q", errOut)
	}
}

func TestHelpExitsZero(t *testing.T) {
	if code, _, errOut, _ := cli(t, "-h"); code != 0 || !strings.Contains(errOut, "-download-dir") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
}

func TestPlainModeSelection(t *testing.T) {
	for _, tc := range []struct {
		args          []string
		noColor, term string
		plain         bool
	}{
		{nil, "", "xterm-256color", false},
		{[]string{"-plain"}, "", "xterm-256color", true},
		{nil, "1", "xterm-256color", true},
		{nil, "", "dumb", true},
	} {
		got := stubTUI(t)
		t.Setenv("NO_COLOR", tc.noColor)
		t.Setenv("TERM", tc.term)
		if code, _, errOut, _ := cli(t, tc.args...); code != 0 || *got == nil {
			t.Fatalf("%+v: exit %d, stderr %q", tc, code, errOut)
		}
		if p := (*got).State().Plain; p != tc.plain {
			t.Errorf("%+v: plain = %v", tc, p)
		}
	}
}

// bbdxfg is the same tool under a short name: identical behavior, messages
// that name the command the user typed.
func TestShortNameBbdxfg(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	runAs := func(name string, args ...string) (int, string, string) {
		var out, errb bytes.Buffer
		code := Run(name, args, &out, &errb)
		return code, out.String(), errb.String()
	}
	for _, args := range [][]string{{"-list"}, {"-version"}} {
		c1, o1, _ := runAs("bbdx_file_gen", args...)
		c2, o2, _ := runAs("bbdxfg", args...)
		if c1 != 0 || c2 != 0 || o1 != o2 {
			t.Errorf("%v: bbdxfg differs from bbdx_file_gen", args)
		}
	}
	if code, _, errOut := runAs("bbdxfg", "stray"); code != 2 ||
		!strings.HasPrefix(errOut, `bbdxfg: unexpected argument "stray"`) || !strings.Contains(errOut, "Usage of bbdxfg:") {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	if code, _, errOut := runAs("bbdxfg", "-download-dir", "/nonexistent/bbdx"); code != 1 ||
		errOut != "bbdxfg: /nonexistent/bbdx does not exist\n" {
		t.Errorf("exit %d, stderr %q", code, errOut)
	}
	got := stubTUI(t)
	if code, _, errOut := runAs("bbdxfg"); code != 0 || *got == nil {
		t.Errorf("bbdxfg did not start the TUI: exit %d, stderr %q", code, errOut)
	}
}
