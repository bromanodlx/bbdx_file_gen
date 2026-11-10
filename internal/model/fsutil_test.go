package model

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noTempFiles fails if a writer's temporary file was left in dir.
func noTempFiles(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temporary file %s left behind", e.Name())
		}
	}
}

func TestExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "f")
	os.WriteFile(file, nil, 0o644)
	dangling := filepath.Join(dir, "dangling")
	if err := os.Symlink(filepath.Join(dir, "missing"), dangling); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	for path, want := range map[string]bool{
		file: true, dir: true, dangling: true, filepath.Join(dir, "missing"): false,
	} {
		if got := Exists(path); got != want {
			t.Errorf("Exists(%s) = %v, want %v", path, got, want)
		}
	}
}

func TestWriteFileAtomicPermissionsAndCleanup(t *testing.T) {
	dir := t.TempDir()
	for _, perm := range []os.FileMode{0o644, 0o600} {
		path := filepath.Join(dir, perm.String())
		err := writeFileAtomic(path, ".x-*.tmp", perm, true, func(w io.Writer) error {
			_, err := io.WriteString(w, "hello")
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		st, _ := os.Stat(path)
		if st.Mode().Perm() != perm || st.Size() != 5 {
			t.Errorf("%s: mode %v size %d", path, st.Mode().Perm(), st.Size())
		}
	}
	noTempFiles(t, dir)
}

func TestWriteFileAtomicFailureLeavesNothing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	boom := errors.New("boom")
	err := writeFileAtomic(path, ".x-*.tmp", 0o644, false, func(w io.Writer) error {
		io.WriteString(w, "partial")
		return boom
	})
	if !errors.Is(err, boom) || Exists(path) {
		t.Errorf("err = %v, exists = %v", err, Exists(path))
	}
	noTempFiles(t, dir)
}

func TestWriteFileAtomicOverwriteReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out")
	os.WriteFile(path, []byte("old"), 0o644)
	if err := writeFileAtomic(path, ".x-*.tmp", 0o644, false, func(w io.Writer) error {
		_, err := io.WriteString(w, "new")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "new" {
		t.Errorf("content %q", b)
	}
	noTempFiles(t, dir)
}

// A file that appears while the content is being generated (after Create's
// up-front check) must not be clobbered.
func TestServiceCreateNeverClobbersFileAppearingDuringWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "race.txt")
	racer := Format{Ext: "txt", Name: "racer", MIME: "text/plain", Category: CategoryText,
		Rule: SizeRule{Min: 3},
		Write: func(w io.Writer, size int64) error {
			if err := os.WriteFile(path, []byte("theirs"), 0o644); err != nil {
				return err
			}
			_, err := io.WriteString(w, "our")
			return err
		}}
	reg, err := NewRegistry(racer)
	if err != nil {
		t.Fatal(err)
	}
	svc := &Service{Registry: reg}
	_, err = svc.Create(Request{Dir: dir, Name: "race", Ext: "txt"})
	if !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "theirs" {
		t.Errorf("the other file was replaced: %q", b)
	}
	noTempFiles(t, dir)

	// With Overwrite the same situation replaces the file, as requested.
	if _, err := svc.Create(Request{Dir: dir, Name: "race", Ext: "txt", Overwrite: true}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "our" {
		t.Errorf("overwrite produced %q", b)
	}
}

func TestServiceCreateTreatsDanglingSymlinkAsExisting(t *testing.T) {
	dir := t.TempDir()
	link := filepath.Join(dir, "report.txt")
	target := filepath.Join(dir, "elsewhere")
	if err := os.Symlink(target, link); err != nil {
		t.Skip("symlinks unsupported:", err)
	}
	svc := &Service{Registry: Default()}
	if _, err := svc.Create(Request{Dir: dir, Name: "report", Ext: "txt"}); !errors.Is(err, ErrExists) {
		t.Fatalf("err = %v, want ErrExists", err)
	}
	if got, err := os.Readlink(link); err != nil || got != target {
		t.Errorf("symlink changed: %q, %v", got, err)
	}
	if Exists(target) {
		t.Error("the symlink's target was created")
	}
}

func TestServiceCreateResultAndPermissions(t *testing.T) {
	dir := t.TempDir()
	size := int64(100)
	res, err := (&Service{Registry: Default()}).Create(Request{Dir: dir, Name: "a", Ext: ".PNG", Size: &size})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := os.Stat(res.Path)
	if res.Path != filepath.Join(dir, "a.png") || res.Size != 100 || res.Format.Ext != "png" || res.EICAR ||
		st.Size() != 100 || st.Mode().Perm() != 0o644 {
		t.Errorf("result %+v, mode %v, size %d", res, st.Mode().Perm(), st.Size())
	}
	noTempFiles(t, dir)
}

func TestServiceCreateEICARIgnoresSizeAndRule(t *testing.T) {
	dir := t.TempDir()
	size := int64(5) // far below the tar minimum and not a multiple of 512
	res, err := (&Service{Registry: Default()}).Create(Request{Dir: dir, Name: "EICAR", Ext: "tar", Size: &size})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(res.Path)
	if !res.EICAR || res.Size != 68 || string(b) != EICAR || filepath.Base(res.Path) != "EICAR.tar" {
		t.Errorf("result %+v, content %q", res, b)
	}
}

func TestServiceCreateSizeErrorNamesFormat(t *testing.T) {
	size := int64(45)
	_, err := (&Service{Registry: Default()}).Create(Request{Dir: t.TempDir(), Name: "a", Ext: "wav", Size: &size})
	var se *SizeError
	if !errors.As(err, &se) || se.Ext != "wav" || se.Suggestion() != 46 {
		t.Errorf("err = %#v", err)
	}
}

func TestFormatCheckSize(t *testing.T) {
	tar, _ := Default().Lookup("tar")
	if err := tar.CheckSize(1536); err != nil {
		t.Errorf("1536: %v", err)
	}
	err := tar.CheckSize(1500)
	var se *SizeError
	if !errors.As(err, &se) || se.Ext != "tar" || se.Below != 1024 || se.Above != 1536 ||
		!strings.HasPrefix(err.Error(), "a .tar file cannot be exactly 1500 bytes") {
		t.Errorf("err = %v (%#v)", err, se)
	}
	// The rule itself stays unaware of the extension.
	var bare *SizeError
	if errors.As(tar.Rule.Check(1500), &bare); bare.Ext != "" {
		t.Errorf("Rule.Check set Ext = %q", bare.Ext)
	}
}

func TestSizeErrorSuggestion(t *testing.T) {
	for _, tc := range []struct{ below, above, want int64 }{
		{10, 20, 20}, {10, -1, 10}, {-1, 20, 20}, {-1, -1, -1},
	} {
		if got := (&SizeError{Below: tc.below, Above: tc.above}).Suggestion(); got != tc.want {
			t.Errorf("Below %d Above %d: got %d, want %d", tc.below, tc.above, got, tc.want)
		}
	}
}

func TestWriteCycle(t *testing.T) {
	block := []byte("abcde")
	for _, tc := range []struct {
		off, n int64
		want   string
	}{
		{0, 0, ""}, {0, 3, "abc"}, {0, 12, "abcdeabcdeab"}, {3, 6, "deabcd"}, {12, 4, "cdea"},
	} {
		var b bytes.Buffer
		if err := writeCycle(&b, block, tc.off, tc.n); err != nil || b.String() != tc.want {
			t.Errorf("off %d n %d: %q, %v; want %q", tc.off, tc.n, b.String(), err, tc.want)
		}
	}
}

func TestLoremStreamContinuesAcrossWrites(t *testing.T) {
	var whole, parts bytes.Buffer
	Lorem(&whole, 1000, false)
	s := &loremStream{}
	for _, n := range []int64{1, 74, 300, 0, 625} {
		s.write(&parts, n)
	}
	if whole.String() != parts.String() {
		t.Error("split lorem text differs from a single write")
	}
}

func TestZipRecordsAreSelfConsistent(t *testing.T) {
	e := zipEntry{name: "a/b.xml", crc: 0xdeadbeef, size: 1234, offset: 99, madeBy: 20}
	lh, cd, eocd := zipLocalHeader(e), zipCentralHeader(e), zipEOCD(3, 300, 4000, 7)
	if len(lh) != zipLocalHeaderLen+len(e.name) || len(cd) != zipCentralHeaderLen+len(e.name) || len(eocd) != zipEOCDLen {
		t.Fatalf("lengths %d %d %d", len(lh), len(cd), len(eocd))
	}
	if !bytes.HasPrefix(lh, []byte("PK\x03\x04")) || !bytes.HasPrefix(cd, []byte("PK\x01\x02")) || !bytes.HasPrefix(eocd, []byte("PK\x05\x06")) {
		t.Error("bad signatures")
	}
	// CRC and sizes appear identically in both headers.
	if !bytes.Equal(lh[14:26], cd[16:28]) || !strings.HasSuffix(string(lh), e.name) || !strings.HasSuffix(string(cd), e.name) {
		t.Error("local and central headers disagree")
	}
}
