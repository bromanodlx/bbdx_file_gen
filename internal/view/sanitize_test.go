package view

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestSanitize(t *testing.T) {
	for in, want := range map[string]string{
		"":                  "",
		"report.pdf":        "report.pdf",
		"Größe 日本 ✓":        "Größe 日本 ✓", // printable non-ASCII is kept
		"a\x1b[2Jb":         "a?[2Jb",
		"title\x1b]0;x\x07": "title?]0;x?",
		"cr\rlf\ntab\t":     "cr?lf?tab?",
		"c1\u009b31m":       "c1?31m",
		"raw\x9b31m":        "raw?31m", // invalid UTF-8 byte
		"del\x7f":           "del?",
		"\x00nul":           "?nul",
	} {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
	if got := sanitizeLines("a\x1b\nb\r"); got != "a?\nb?" {
		t.Errorf("sanitizeLines = %q", got)
	}
}

func TestRenderNeverEmitsUntrustedControlCharacters(t *testing.T) {
	evil := "x\x1b]0;pwned\x07\r\x9b\u009by"
	for _, step := range []Step{StepName, StepExt, StepSize, StepLocation, StepConfirm, StepGenerating, StepDone, StepDownloadDir} {
		for _, plain := range []bool{false, true} {
			out := Render(State{
				Step: step, Plain: plain, Name: evil, Ext: evil, FormatName: evil, TargetPath: evil,
				MinSize: evil, SizeNote: evil, CurrentDir: evil, DownloadDir: evil, DownloadReason: evil + "\n" + evil,
				SizeLabel: evil, Spinner: evil, Error: evil, Hint: evil, EICAR: true,
				Suggestions: []Suggestion{{Ext: evil, Name: evil, Category: evil, MinSize: evil, Selected: true}, {Ext: evil, Name: evil}},
			})
			if strings.Contains(out, "pwned\x07") || strings.ContainsAny(out, "\r\x07\u009b") || strings.Contains(out, "\x1b]") {
				t.Errorf("step %v plain=%v leaks control characters: %q", step, plain, out)
			}
		}
	}
}

func TestPlainNoMatchListIsASCII(t *testing.T) {
	out := Render(State{Step: StepExt, Name: "a", Input: "> zzz", Plain: true})
	for _, r := range out {
		if r > 127 {
			t.Fatalf("non-ASCII %q in plain output:\n%s", r, out)
		}
	}
	if !strings.Contains(out, "no matching file type - keep typing") {
		t.Errorf("message missing:\n%s", out)
	}
}

func TestPadTruncatesAndPads(t *testing.T) {
	for _, tc := range []struct {
		left, right string
		w           int
		want        string
	}{
		{"ab", "cd", 10, "ab      cd"},
		{"ab", "cd", 6, "ab  cd"},
		{"abc", "def", 6, "abc..."},
		{"", "", 3, "   "},
	} {
		if got := pad(tc.left, tc.right, tc.w, "..."); got != tc.want {
			t.Errorf("pad(%q, %q, %d) = %q, want %q", tc.left, tc.right, tc.w, got, tc.want)
		}
	}
}

// Regression: the update question shows versions that come from the network;
// they must be sanitized like every other text field.
func TestUpdateVersionsAreSanitized(t *testing.T) {
	out := Render(State{Step: StepUpdate, UpdateVersion: "9.9.9\x1b]0;pwned\x07", InstalledVersion: "1.4.1\x1b[2J\x9b"})
	for _, bad := range []string{"\x1b]0;", "\x07", "\x1b[2J", "\x9b"} {
		if strings.Contains(out, bad) {
			t.Errorf("rendered update question contains %q", bad)
		}
	}
	if plain := ansi.Strip(out); !strings.Contains(plain, "9.9.9?]0;pwned?") || !strings.Contains(plain, "1.4.1?[2J?") {
		t.Errorf("versions not shown sanitized:\n%s", plain)
	}
}
