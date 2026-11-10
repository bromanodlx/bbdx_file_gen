package model

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func fakeFormat(ext string, aliases ...string) Format {
	return Format{
		Ext: ext, Name: strings.ToUpper(ext) + " test", MIME: "application/x-" + ext,
		Category: CategoryText, Aliases: aliases, Rule: SizeRule{Min: 3},
		Write: func(w io.Writer, size int64) error {
			_, err := w.Write(bytes.Repeat([]byte("x"), int(size)))
			return err
		},
	}
}

func TestRegistryLookupNormalizesInput(t *testing.T) {
	reg, err := NewRegistry(fakeFormat("jpg", "jpeg"), fakeFormat("png"))
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range []string{"jpg", ".jpg", "JPG", " .JpEg "} {
		f, ok := reg.Lookup(in)
		if !ok || f.Ext != "jpg" {
			t.Errorf("Lookup(%q) = %v, %v; want jpg", in, f.Ext, ok)
		}
	}
	if _, ok := reg.Lookup("gif"); ok {
		t.Error("unknown extension must not be found")
	}
}

func TestRegistryRejectsDuplicates(t *testing.T) {
	if _, err := NewRegistry(fakeFormat("jpg"), fakeFormat("jpg")); err == nil {
		t.Error("duplicate ext must be rejected")
	}
	if _, err := NewRegistry(fakeFormat("jpg", "jpeg"), fakeFormat("jpeg")); err == nil {
		t.Error("alias clashing with ext must be rejected")
	}
}

func TestRegistrySearchPrefix(t *testing.T) {
	reg, _ := NewRegistry(fakeFormat("pdf"), fakeFormat("png"), fakeFormat("py"), fakeFormat("jpg", "jpeg"))
	got := exts(reg.Search("p"))
	if strings.Join(got, ",") != "pdf,png,py" {
		t.Errorf("Search(p) = %v", got)
	}
	if got := exts(reg.Search(".JP")); strings.Join(got, ",") != "jpg" {
		t.Errorf("Search(.JP) = %v", got)
	}
	if got := exts(reg.Search("jpe")); strings.Join(got, ",") != "jpg" {
		t.Errorf("Search via alias = %v", got)
	}
	if got := reg.Search(""); len(got) != 4 {
		t.Errorf("empty search must list all, got %d", len(got))
	}
}

func TestFormatGenerateChecksSizeRule(t *testing.T) {
	f := fakeFormat("txt")
	var buf bytes.Buffer
	var se *SizeError
	if err := f.Generate(&buf, 2); !errors.As(err, &se) {
		t.Fatalf("expected SizeError, got %v", err)
	}
	if buf.Len() != 0 {
		t.Error("nothing must be written for an invalid size")
	}
}

func TestFormatGenerateDetectsWrongLength(t *testing.T) {
	f := fakeFormat("txt")
	f.Write = func(w io.Writer, size int64) error { _, err := w.Write([]byte("short")); return err }
	if err := f.Generate(io.Discard, 10); err == nil {
		t.Error("a generator that writes the wrong number of bytes must be reported")
	}
}

func exts(fs []Format) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = f.Ext
	}
	return out
}
