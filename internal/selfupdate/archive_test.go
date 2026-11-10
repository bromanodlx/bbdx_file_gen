package selfupdate_test

import (
	"testing"

	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate"

	"github.com/bromanodlx/bbdx_file_gen/internal/selfupdate/selfupdatetest"
)

func TestChecksumFor(t *testing.T) {
	sums := []byte("AB12  a.tar.gz\ncd34 *b.zip\n")
	if got, err := selfupdate.ChecksumFor(sums, "a.tar.gz"); err != nil || got != "ab12" {
		t.Errorf("a: %q %v", got, err)
	}
	if got, err := selfupdate.ChecksumFor(sums, "b.zip"); err != nil || got != "cd34" {
		t.Errorf("b: %q %v", got, err)
	}
	if _, err := selfupdate.ChecksumFor(sums, "c.zip"); err == nil {
		t.Error("missing entry accepted")
	}
}

func TestExtract(t *testing.T) {
	files := map[string]string{"d/README.md": "r", "d/tool": "unix", "d/tool.exe": "win"}
	if got, err := selfupdate.Extract(selfupdatetest.TarGz(t, files), false, "tool"); err != nil || string(got) != "unix" {
		t.Errorf("tar: %q %v", got, err)
	}
	if got, err := selfupdate.Extract(selfupdatetest.Zip(t, files), true, "tool.exe"); err != nil || string(got) != "win" {
		t.Errorf("zip: %q %v", got, err)
	}
	readme := map[string]string{"d/README.md": "r"}
	if _, err := selfupdate.Extract(selfupdatetest.TarGz(t, readme), false, "tool"); err == nil {
		t.Error("tar without binary accepted")
	}
	if _, err := selfupdate.Extract(selfupdatetest.Zip(t, readme), true, "tool.exe"); err == nil {
		t.Error("zip without binary accepted")
	}
}
