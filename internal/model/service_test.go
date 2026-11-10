package model

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func newTestService(t *testing.T) (*Service, string) {
	t.Helper()
	reg, err := NewRegistry(fakeFormat("txt"), fakeFormat("jpg", "jpeg"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	return &Service{Registry: reg}, dir
}

func TestValidateName(t *testing.T) {
	for _, bad := range []string{"", "   ", ".", "..", "a/", "with\x00nul", "trailing/"} {
		if err := ValidateName(bad); err == nil {
			t.Errorf("ValidateName(%q) expected error", bad)
		}
	}
	for _, good := range []string{"report", "my file", "sub/dir/file", "x.tar"} {
		if err := ValidateName(good); err != nil {
			t.Errorf("ValidateName(%q) unexpected error: %v", good, err)
		}
	}
}

func TestFileName(t *testing.T) {
	cases := []struct{ name, ext, want string }{
		{"report", "pdf", "report.pdf"},
		{"report.pdf", "pdf", "report.pdf"},
		{"REPORT.PDF", "pdf", "REPORT.PDF"},
		{"archive.tar", "gz", "archive.tar.gz"},
		{" spaced ", "txt", "spaced.txt"},
	}
	for _, c := range cases {
		if got := FileName(c.name, c.ext); got != c.want {
			t.Errorf("FileName(%q,%q) = %q, want %q", c.name, c.ext, got, c.want)
		}
	}
}

func TestServiceCreateUsesMinimumWhenSizeOmitted(t *testing.T) {
	svc, dir := newTestService(t)
	res, err := svc.Create(Request{Dir: dir, Name: "a", Ext: "txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Size != 3 || res.Path != filepath.Join(dir, "a.txt") {
		t.Errorf("result = %+v", res)
	}
	assertFileSize(t, res.Path, 3)
}

func TestServiceCreateExplicitSize(t *testing.T) {
	svc, dir := newTestService(t)
	size := int64(4096)
	res, err := svc.Create(Request{Dir: dir, Name: "b", Ext: ".JPEG", Size: &size})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(res.Path) != "b.jpg" || res.Format.Ext != "jpg" {
		t.Errorf("result = %+v", res)
	}
	assertFileSize(t, res.Path, 4096)
}

func TestServiceCreateUnknownExtension(t *testing.T) {
	svc, dir := newTestService(t)
	if _, err := svc.Create(Request{Dir: dir, Name: "a", Ext: "xyz"}); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("expected ErrUnknownFormat, got %v", err)
	}
}

func TestServiceCreateInvalidSizeLeavesNoFile(t *testing.T) {
	svc, dir := newTestService(t)
	size := int64(1)
	var se *SizeError
	if _, err := svc.Create(Request{Dir: dir, Name: "a", Ext: "txt", Size: &size}); !errors.As(err, &se) {
		t.Fatalf("expected SizeError, got %v", err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Errorf("no file (and no temp file) may remain, found %d entries", len(entries))
	}
}

func TestServiceRefusesOverwriteUnlessAllowed(t *testing.T) {
	svc, dir := newTestService(t)
	if _, err := svc.Create(Request{Dir: dir, Name: "a", Ext: "txt"}); err != nil {
		t.Fatal(err)
	}
	size := int64(10)
	if _, err := svc.Create(Request{Dir: dir, Name: "a", Ext: "txt", Size: &size}); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	res, err := svc.Create(Request{Dir: dir, Name: "a", Ext: "txt", Size: &size, Overwrite: true})
	if err != nil {
		t.Fatal(err)
	}
	assertFileSize(t, res.Path, 10)
}

func TestServiceTargetPath(t *testing.T) {
	svc, dir := newTestService(t)
	p, err := svc.TargetPath(dir, "x", "jpeg")
	if err != nil || p != filepath.Join(dir, "x.jpg") {
		t.Errorf("TargetPath = %q, %v", p, err)
	}
}

func TestServiceCreateMissingDirectory(t *testing.T) {
	svc, dir := newTestService(t)
	if _, err := svc.Create(Request{Dir: dir, Name: "nope/a", Ext: "txt"}); err == nil {
		t.Error("expected error for a missing parent directory")
	}
}

func assertFileSize(t *testing.T, path string, want int64) {
	t.Helper()
	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if st.Size() != want {
		t.Errorf("%s has %d bytes, want %d", path, st.Size(), want)
	}
}

func TestServiceTargetPathIgnoresDirForAbsoluteNames(t *testing.T) {
	svc, dir := newTestService(t)
	abs := filepath.Join(t.TempDir(), "x")
	if p, _ := svc.TargetPath(dir, abs, "txt"); p != abs+".txt" {
		t.Errorf("TargetPath = %q", p)
	}
}

func TestServiceCreateMakesBaseDirOnlyWhenAsked(t *testing.T) {
	svc, dir := newTestService(t)
	missing := filepath.Join(dir, "Downloads")
	if _, err := svc.Create(Request{Dir: missing, Name: "a", Ext: "txt"}); err == nil {
		t.Fatal("a missing directory must not be created implicitly")
	}
	res, err := svc.Create(Request{Dir: missing, MakeDir: true, Name: "a", Ext: "txt"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != filepath.Join(missing, "a.txt") {
		t.Errorf("path = %q", res.Path)
	}
	// MakeDir creates the base directory, not directories inside the name
	if _, err := svc.Create(Request{Dir: missing, MakeDir: true, Name: "sub/a", Ext: "txt"}); err == nil {
		t.Error("sub directories of the name must not be created")
	}
}
