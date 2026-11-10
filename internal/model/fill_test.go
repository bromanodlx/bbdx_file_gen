package model

import (
	"bytes"
	"strings"
	"testing"
)

func TestLoremWritesExactLengthOfPrintableASCII(t *testing.T) {
	for _, n := range []int64{0, 1, 5, 79, 80, 81, 1000, 70000} {
		var buf bytes.Buffer
		if err := Lorem(&buf, n, true); err != nil {
			t.Fatal(err)
		}
		if int64(buf.Len()) != n {
			t.Fatalf("Lorem(%d) wrote %d bytes", n, buf.Len())
		}
		for _, c := range buf.Bytes() {
			if !(c == '\n' || (c >= 0x20 && c < 0x7f)) {
				t.Fatalf("Lorem(%d) produced non-printable byte %#x", n, c)
			}
		}
	}
}

func TestLoremWithoutNewlinesIsSingleLineWords(t *testing.T) {
	var buf bytes.Buffer
	_ = Lorem(&buf, 5000, false)
	s := buf.String()
	if strings.ContainsAny(s, "\n\r\t,;\"'<>&") {
		t.Error("single-line lorem must only contain letters, spaces and periods")
	}
}

func TestLoremWrapsLines(t *testing.T) {
	var buf bytes.Buffer
	_ = Lorem(&buf, 5000, true)
	for _, line := range strings.Split(buf.String(), "\n") {
		if len(line) > 80 {
			t.Fatalf("line longer than 80 chars: %q", line)
		}
	}
}

func TestRepeat(t *testing.T) {
	var buf bytes.Buffer
	if err := Repeat(&buf, 0xAB, 100000); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 100000 || bytes.Count(buf.Bytes(), []byte{0xAB}) != 100000 {
		t.Error("Repeat wrote wrong content")
	}
}

func TestCountingWriter(t *testing.T) {
	var cw CountingWriter
	cw.Write([]byte("hello"))
	cw.Write([]byte(" world"))
	if cw.N != 11 {
		t.Errorf("N = %d", cw.N)
	}
}
