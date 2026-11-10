package model

import (
	"errors"
	"testing"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"0", 0},
		{"1024", 1024},
		{"  77 ", 77},
		{"10B", 10},
		{"1KB", 1000},
		{"1kb", 1000},
		{"1KiB", 1024},
		{"1K", 1024},
		{"2 MiB", 2 << 20},
		{"1.5KiB", 1536},
		{"3MB", 3_000_000},
		{"1GiB", 1 << 30},
		{"1G", 1 << 30},
	}
	for _, c := range cases {
		got, err := ParseSize(c.in)
		if err != nil {
			t.Errorf("ParseSize(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseSize(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestParseSizeRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "abc", "-5", "1.5", "1.0001KiB", "10XB", "KB", "1e3"} {
		if _, err := ParseSize(in); err == nil {
			t.Errorf("ParseSize(%q) expected error", in)
		}
	}
}

func TestHumanSize(t *testing.T) {
	cases := map[int64]string{
		0:             "0 B",
		512:           "512 B",
		1024:          "1 KiB",
		1536:          "1.5 KiB",
		1 << 20:       "1 MiB",
		1<<30 + 1<<29: "1.5 GiB",
	}
	for in, want := range cases {
		if got := HumanSize(in); got != want {
			t.Errorf("HumanSize(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestSizeRuleMinimum(t *testing.T) {
	r := SizeRule{Min: 10}
	if err := r.Check(10); err != nil {
		t.Fatalf("min must be valid: %v", err)
	}
	var se *SizeError
	if err := r.Check(9); !errors.As(err, &se) {
		t.Fatalf("below min must be a SizeError, got %v", err)
	}
	if se.Above != 10 || se.Below != -1 {
		t.Errorf("suggestions = (%d, %d), want (-1, 10)", se.Below, se.Above)
	}
}

func TestSizeRuleAlignment(t *testing.T) {
	r := SizeRule{Min: 1024, Align: 512}
	for _, ok := range []int64{1024, 1536, 4096} {
		if err := r.Check(ok); err != nil {
			t.Errorf("Check(%d) unexpected error: %v", ok, err)
		}
	}
	var se *SizeError
	if err := r.Check(1500); !errors.As(err, &se) {
		t.Fatalf("misaligned size must fail")
	}
	if se.Below != 1024 || se.Above != 1536 {
		t.Errorf("suggestions = (%d, %d), want (1024, 1536)", se.Below, se.Above)
	}
}

func TestSizeRuleGaps(t *testing.T) {
	r := SizeRule{Min: 96, Gaps: []Range{{97, 116}}}
	if err := r.Check(96); err != nil {
		t.Fatal(err)
	}
	if err := r.Check(117); err != nil {
		t.Fatal(err)
	}
	var se *SizeError
	if err := r.Check(100); !errors.As(err, &se) {
		t.Fatalf("size inside gap must fail")
	}
	if se.Below != 96 || se.Above != 117 {
		t.Errorf("suggestions = (%d, %d), want (96, 117)", se.Below, se.Above)
	}
}

func TestSizeRuleAlignmentAndGapsCombined(t *testing.T) {
	// RIFF style: even sizes only, plus a gap the padding chunk cannot fill.
	r := SizeRule{Min: 34, Align: 2, Gaps: []Range{{36, 51}}}
	var se *SizeError
	if err := r.Check(40); !errors.As(err, &se) {
		t.Fatal("expected error")
	}
	if se.Below != 34 || se.Above != 52 {
		t.Errorf("suggestions = (%d, %d), want (34, 52)", se.Below, se.Above)
	}
}

func TestSizeRuleMaximum(t *testing.T) {
	r := SizeRule{Min: 0}
	if err := r.Check(MaxSize); err != nil {
		t.Fatalf("MaxSize must be allowed: %v", err)
	}
	var se *SizeError
	if err := r.Check(MaxSize + 1); !errors.As(err, &se) {
		t.Fatal("above MaxSize must fail")
	}
	if se.Above != -1 || se.Below != MaxSize {
		t.Errorf("suggestions = (%d, %d), want (%d, -1)", se.Below, se.Above, MaxSize)
	}
}

func TestSizeErrorMessageMentionsSuggestions(t *testing.T) {
	err := SizeRule{Min: 1024, Align: 512}.Check(1500)
	msg := err.Error()
	for _, want := range []string{"1500", "1024", "1536"} {
		if !contains(msg, want) {
			t.Errorf("error %q should mention %s", msg, want)
		}
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s[:len(sub)] == sub || contains(s[1:], sub)))
}
