package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// install fakes an installed bbdxfg: an executable, a config directory with a
// config and a leftover temp file, an unrelated sibling config directory and a
// file the user generated. The executable hooks point at the fake.
type install struct {
	exe, cfgDir, cfgPath, sibling, userFile string
}

func fakeInstall(t *testing.T) install {
	t.Helper()
	root := t.TempDir()
	in := install{
		exe:      filepath.Join(root, "bin", "bbdxfg"),
		cfgDir:   filepath.Join(root, "config", "bbdx_file_gen"),
		sibling:  filepath.Join(root, "config", "other_tool"),
		userFile: filepath.Join(root, "Downloads", "report.pdf"),
	}
	in.cfgPath = filepath.Join(in.cfgDir, "config.json")
	for path, data := range map[string]string{
		in.exe:     "binary",
		in.cfgPath: `{"download_dir":"` + filepath.Dir(in.userFile) + `"}`,
		filepath.Join(in.cfgDir, ".config-1.tmp"): "partial",
		filepath.Join(in.sibling, "keep.json"):    "{}",
		in.userFile:                               "%PDF",
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("HOME", root)
	hook(t, &executable, func() (string, error) { return in.exe, nil })
	hook(t, &invokedAs, func() string { return in.exe })
	return in
}

func hook[T any](t *testing.T, v *T, f T) {
	orig := *v
	*v = f
	t.Cleanup(func() { *v = orig })
}

func runDeinstall(t *testing.T, answer string, args ...string) (int, string, string) {
	t.Helper()
	if args == nil {
		args = []string{"deinstall"}
	}
	var out, errb bytes.Buffer
	code := Run(args, strings.NewReader(answer), &out, &errb)
	return code, out.String(), errb.String()
}

func mustExist(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); err != nil {
			t.Errorf("%s was removed: %v", p, err)
		}
	}
}

func mustBeGone(t *testing.T, paths ...string) {
	t.Helper()
	for _, p := range paths {
		if _, err := os.Lstat(p); err == nil {
			t.Errorf("%s still exists", p)
		}
	}
}

func TestDeinstallDeclinedRemovesNothing(t *testing.T) {
	for _, answer := range []string{"n\n", "no\n", "\n", "", "maybe\n", "yy\n", "nope y\n"} {
		in := fakeInstall(t)
		tui := stubTUI(t)
		code, out, errOut := runDeinstall(t, answer)
		if code != 0 || out != "Are you sure?? [y/N] Nothing was removed.\n" || errOut != "" {
			t.Errorf("answer %q: exit %d, stdout %q, stderr %q", answer, code, out, errOut)
		}
		if *tui != nil {
			t.Errorf("answer %q started the wizard", answer)
		}
		mustExist(t, in.exe, in.cfgPath, in.sibling, in.userFile)
	}
}

func TestDeinstallConfirmedRemovesEverything(t *testing.T) {
	for _, answer := range []string{"y\n", "Y\n", "yes\n", " YES \r\n", "y"} {
		in := fakeInstall(t)
		code, out, errOut := runDeinstall(t, answer)
		want := "Are you sure?? [y/N] removed " + in.cfgDir + "\nremoved " + in.exe + "\nbbdxfg has been deinstalled.\n"
		if code != 0 || out != want || errOut != "" {
			t.Errorf("answer %q: exit %d, stdout %q, stderr %q", answer, code, out, errOut)
		}
		mustBeGone(t, in.exe, in.cfgDir)
		// Other tools' settings and the files the user generated stay.
		mustExist(t, in.sibling, in.userFile, filepath.Dir(in.exe))
	}
}

func TestDeinstallWithoutConfigRemovesExecutable(t *testing.T) {
	in := fakeInstall(t)
	os.RemoveAll(in.cfgDir)
	code, out, _ := runDeinstall(t, "y\n")
	if code != 0 || strings.Contains(out, in.cfgDir) || !strings.Contains(out, "removed "+in.exe+"\n") {
		t.Errorf("exit %d, stdout %q", code, out)
	}
	mustBeGone(t, in.exe)
}

// Started through a symlink (e.g. ~/.local/bin/bbdxfg -> /opt/bbdxfg), both
// the link and the real executable go; a link that only shares the name but
// points elsewhere stays.
func TestDeinstallRemovesSymlinkItWasStartedThrough(t *testing.T) {
	in := fakeInstall(t)
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "bbdxfg")
	if err := os.Symlink(in.exe, link); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	hook(t, &executable, func() (string, error) { return link, nil }) // resolved like /proc/self/exe
	hook(t, &invokedAs, func() string { return link })
	code, out, errOut := runDeinstall(t, "y\n")
	if code != 0 || !strings.Contains(out, "removed "+link+"\nremoved "+in.exe+"\n") {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	mustBeGone(t, link, in.exe)

	in = fakeInstall(t)
	other := filepath.Join(t.TempDir(), "bbdxfg")
	os.WriteFile(filepath.Join(filepath.Dir(other), "elsewhere"), nil, 0o755)
	os.Symlink(filepath.Join(filepath.Dir(other), "elsewhere"), other)
	hook(t, &invokedAs, func() string { return other })
	if code, out, _ := runDeinstall(t, "y\n"); code != 0 || strings.Contains(out, other) {
		t.Errorf("unrelated link: exit %d, stdout %q", code, out)
	}
	mustExist(t, other, filepath.Join(filepath.Dir(other), "elsewhere"))
}

// A config directory that is a symlink is unlinked; its target is not
// emptied.
func TestDeinstallDoesNotFollowConfigSymlink(t *testing.T) {
	in := fakeInstall(t)
	target := filepath.Join(t.TempDir(), "real_config")
	if err := os.Rename(in.cfgDir, target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, in.cfgDir); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	if code, _, errOut := runDeinstall(t, "y\n"); code != 0 {
		t.Fatalf("exit %d, stderr %q", code, errOut)
	}
	mustBeGone(t, in.cfgDir)
	mustExist(t, filepath.Join(target, "config.json"))
}

func TestDeinstallReportsWhatCouldNotBeRemoved(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can remove anything")
	}
	in := fakeInstall(t)
	bin := filepath.Dir(in.exe)
	os.Chmod(bin, 0o555) // like a system directory the user cannot write
	t.Cleanup(func() { os.Chmod(bin, 0o755) })
	code, out, errOut := runDeinstall(t, "y\n")
	if code != 1 || !strings.Contains(errOut, "bbdxfg: could not remove "+in.exe) ||
		strings.Contains(out, "has been deinstalled") || !strings.Contains(out, "removed "+in.cfgDir) {
		t.Errorf("exit %d, stdout %q, stderr %q", code, out, errOut)
	}
	mustExist(t, in.exe)
}

func TestDeinstallUsageErrors(t *testing.T) {
	for _, args := range [][]string{
		{"deinstall", "now"}, {"deinstall", "-plain"}, {"-plain", "deinstall"}, {"-list", "deinstall"},
	} {
		in := fakeInstall(t)
		code, out, errOut := runDeinstall(t, "y\n", args...)
		if code != 2 || out != "" || !strings.HasPrefix(errOut, "bbdxfg: deinstall takes no flags or arguments\nUsage of bbdxfg:") {
			t.Errorf("%q: exit %d, stdout %q, stderr %q", args, code, out, errOut)
		}
		mustExist(t, in.exe, in.cfgPath)
	}
	for _, args := range [][]string{{"uninstall"}, {"Deinstall"}, {"report.pdf", "deinstall"}} {
		in := fakeInstall(t)
		code, _, errOut := runDeinstall(t, "y\n", args...)
		if code != 2 || !strings.HasPrefix(errOut, `bbdxfg: unexpected argument "`+args[0]+`"`) {
			t.Errorf("%q: exit %d, stderr %q", args, code, errOut)
		}
		mustExist(t, in.exe, in.cfgPath)
	}
}

func TestHelpListsDeinstall(t *testing.T) {
	_, _, errOut, _ := cli(t, "-h")
	if !strings.HasPrefix(errOut, "Usage of bbdxfg:\n  bbdxfg [flags]      start the wizard\n  bbdxfg deinstall    remove bbdxfg and its configuration\nFlags:\n  -download-dir") {
		t.Errorf("help:\n%s", errOut)
	}
}
