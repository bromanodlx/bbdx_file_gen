package view

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func render(s State) string { return ansi.Strip(Render(s)) }

func TestRenderShowsTitleAndSteps(t *testing.T) {
	out := render(State{Step: StepName, Input: "> report"})
	for _, want := range []string{"bbdx_file_gen", "Name", "Type", "Size", "File name", "> report"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRenderShowsError(t *testing.T) {
	out := render(State{Step: StepName, Error: "file name must not be empty"})
	if !strings.Contains(out, "file name must not be empty") {
		t.Errorf("error not rendered:\n%s", out)
	}
}

func TestRenderExtensionSuggestions(t *testing.T) {
	out := render(State{
		Step: StepExt, Name: "report", Input: "> p",
		Suggestions: []Suggestion{
			{Ext: "pdf", Name: "Portable Document Format", Category: "Documents", MinSize: "560 B"},
			{Ext: "png", Name: "Portable Network Graphics", Category: "Images", MinSize: "70 B", Selected: true},
		},
		MoreSuggestions: 3,
	})
	for _, want := range []string{"File type", "pdf", "Portable Document Format", "png", "70 B", "3 more"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	// the selected entry carries a marker
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "png") && !strings.Contains(line, "▸") {
			t.Errorf("selected suggestion not marked: %q", line)
		}
		if strings.Contains(line, "pdf") && strings.Contains(line, "▸") {
			t.Errorf("unselected suggestion marked: %q", line)
		}
	}
}

func TestRenderSizeStepShowsMinimum(t *testing.T) {
	out := render(State{
		Step: StepSize, Name: "report", Ext: "png", FormatName: "Portable Network Graphics",
		TargetPath: "report.png", MinSize: "70 B", MinBytes: 70, SizeNote: "RIFF files are even",
		Hint: "tab: use 72",
	})
	for _, want := range []string{"report.png", "Portable Network Graphics", "70 B", "70 bytes", "RIFF files are even", "tab: use 72", "KiB"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRenderConfirmOverwrite(t *testing.T) {
	out := render(State{Step: StepConfirm, TargetPath: "/tmp/x.png"})
	if !strings.Contains(out, "/tmp/x.png") || !strings.Contains(out, "Overwrite") || !strings.Contains(out, "y/N") {
		t.Errorf("confirm prompt incomplete:\n%s", out)
	}
}

func TestRenderGenerating(t *testing.T) {
	out := render(State{Step: StepGenerating, TargetPath: "big.zip", Spinner: "⣾", SizeLabel: "1 GiB"})
	if !strings.Contains(out, "big.zip") || !strings.Contains(out, "1 GiB") {
		t.Errorf("progress incomplete:\n%s", out)
	}
}

func TestRenderDone(t *testing.T) {
	out := render(State{Step: StepDone, TargetPath: "/tmp/x.png", SizeLabel: "1 KiB", Bytes: 1024, FormatName: "PNG"})
	for _, want := range []string{"/tmp/x.png", "1 KiB", "1024 bytes", "PNG", "another"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}

func TestRenderHelpDependsOnStep(t *testing.T) {
	if out := render(State{Step: StepExt}); !strings.Contains(out, "tab") || !strings.Contains(out, "↑/↓") {
		t.Errorf("extension step needs tab/arrow help:\n%s", out)
	}
	if out := render(State{Step: StepName}); strings.Contains(out, "↑/↓") {
		t.Errorf("name step must not advertise arrows:\n%s", out)
	}
	if out := render(State{Step: StepSize}); !strings.Contains(out, "esc") {
		t.Errorf("size step needs esc help:\n%s", out)
	}
}

func TestRenderFitsNarrowTerminal(t *testing.T) {
	s := State{Step: StepExt, Width: 40, Suggestions: []Suggestion{{Ext: "docx", Name: "Office Open XML word processing document", Category: "Documents", MinSize: "1.2 KiB"}}}
	for _, line := range strings.Split(Render(s), "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("line is %d cells wide on a 40 column terminal: %q", w, ansi.Strip(line))
		}
	}
}

func TestRenderListGroupsByCategory(t *testing.T) {
	out := RenderList([]ListRow{
		{Ext: "png", Aliases: "", Name: "Portable Network Graphics", Category: "Images", MinSize: "70 B"},
		{Ext: "jpg", Aliases: "jpeg", Name: "JPEG", Category: "Images", MinSize: "141 B"},
		{Ext: "txt", Name: "Plain text", Category: "Text & data", MinSize: "0 B"},
	})
	if strings.Count(out, "Images") != 1 || !strings.Contains(out, "Text & data") {
		t.Errorf("categories must appear once each:\n%s", out)
	}
	if !strings.Contains(out, "jpeg") || !strings.Contains(out, "141 B") {
		t.Errorf("aliases and minimum sizes must be listed:\n%s", out)
	}
	if !strings.Contains(out, "3 file types") {
		t.Errorf("total missing:\n%s", out)
	}
}

func TestRenderLocationQuestion(t *testing.T) {
	s := State{Step: StepLocation, TargetPath: "report.pdf", CurrentDir: "/home/u/proj", DownloadDir: "/home/u/Downloads", LocationYes: true}
	out := render(s)
	for _, want := range []string{"4 Location", "report.pdf", "current directory", "/home/u/proj", "/home/u/Downloads", "Yes", "No"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	marked := func(out, word string) bool {
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, word) && strings.Contains(line, "▸") {
				return true
			}
		}
		return false
	}
	if !marked(out, "Yes") || marked(out, "No ") {
		t.Errorf("yes must be marked:\n%s", out)
	}
	s.LocationYes = false
	if out := render(s); marked(out, "Yes") || !marked(out, "No ") {
		t.Errorf("no must be marked:\n%s", out)
	}
	if out := render(s); !strings.Contains(out, "y/n") {
		t.Errorf("location help must mention y/n:\n%s", out)
	}
}

func TestRenderDownloadDirSetup(t *testing.T) {
	s := State{Step: StepDownloadDir, Input: "> ~/Downloads", DownloadReason: "No standard download directory is configured yet."}
	out := render(s)
	for _, want := range []string{"Standard download directory", "No standard download directory", "> ~/Downloads", "relative", "absolute", "~", "esc quit"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "1 Name") {
		t.Errorf("the settings screen is not part of the wizard steps:\n%s", out)
	}
	s.DownloadCancel = true
	if out := render(s); !strings.Contains(out, "esc cancel") {
		t.Errorf("an existing setting must be cancellable:\n%s", out)
	}
}

func TestHelpAdvertisesDownloadDirShortcut(t *testing.T) {
	for _, step := range []Step{StepName, StepExt, StepSize, StepLocation, StepDone} {
		if out := render(State{Step: step}); !strings.Contains(out, "ctrl+d download dir") {
			t.Errorf("step %v help lacks ctrl+d:\n%s", step, out)
		}
	}
	for _, step := range []Step{StepConfirm, StepGenerating} {
		if out := render(State{Step: step}); strings.Contains(out, "ctrl+d") {
			t.Errorf("step %v must not offer ctrl+d", step)
		}
	}
}

func TestRenderEICARNote(t *testing.T) {
	loc := render(State{Step: StepLocation, EICAR: true, TargetPath: "eicar.txt", CurrentDir: "/a", DownloadDir: "/b"})
	if !strings.Contains(loc, "EICAR") || !strings.Contains(loc, "68") {
		t.Errorf("location step must note the EICAR file and its size:\n%s", loc)
	}
	done := render(State{Step: StepDone, EICAR: true, TargetPath: "eicar.txt", SizeLabel: "68 B", Bytes: 68, FormatName: "Plain text"})
	if !strings.Contains(done, "EICAR") {
		t.Errorf("done step must note the EICAR file:\n%s", done)
	}
	if plain := render(State{Step: StepLocation, TargetPath: "x.txt", CurrentDir: "/a", DownloadDir: "/b"}); strings.Contains(plain, "EICAR") {
		t.Errorf("an ordinary file must not mention EICAR:\n%s", plain)
	}
}

func TestRenderDrawsAFramedCard(t *testing.T) {
	out := render(State{Step: StepName, Input: "> report", Width: 74})
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	var top, bottom string
	var boxed []string
	for _, l := range lines {
		switch {
		case strings.HasPrefix(l, "╭"):
			top = l
		case strings.HasPrefix(l, "╰"):
			bottom = l
		}
		if strings.HasPrefix(l, "│") || strings.HasPrefix(l, "╭") || strings.HasPrefix(l, "╰") {
			boxed = append(boxed, l)
		}
	}
	if top == "" || bottom == "" {
		t.Fatalf("card is missing its top or bottom border:\n%s", out)
	}
	w := ansi.StringWidth(boxed[0])
	for _, l := range boxed {
		if got := ansi.StringWidth(l); got != w {
			t.Errorf("ragged frame: line width %d != %d: %q", got, w, l)
		}
	}
}

func TestRenderShowsFigletBanner(t *testing.T) {
	out := render(State{Step: StepName, Input: "> report", Width: 74})
	for _, row := range Banner {
		if !strings.Contains(out, row) {
			t.Errorf("banner row missing: %q\n%s", row, out)
		}
	}
	// the banner falls back to plain text when it will not fit
	narrow := render(State{Step: StepName, Input: "> x", Width: 30})
	if strings.Contains(narrow, Banner[0]) {
		t.Errorf("banner must collapse at narrow widths:\n%s", narrow)
	}
	if !strings.Contains(narrow, "bbdx_file_gen") {
		t.Errorf("narrow fallback must still name the tool:\n%s", narrow)
	}
}

func TestRenderNeverExceedsWidthAcrossSteps(t *testing.T) {
	steps := []State{
		{Step: StepName, Input: "> x"},
		{Step: StepExt, Name: "report", Input: "> p", Suggestions: []Suggestion{{Ext: "docx", Name: "Office Open XML word processing document", Category: "Documents", MinSize: "1.2 KiB", Selected: true}}, MoreSuggestions: 9},
		{Step: StepSize, FormatName: "Portable Network Graphics", TargetPath: "/very/long/path/to/a/report.png", MinSize: "70 B", MinBytes: 70, SizeNote: "RIFF files are even", Hint: "tab: use 72"},
		{Step: StepLocation, TargetPath: "report.pdf", CurrentDir: "/home/user/a/very/long/project/directory/path", DownloadDir: "/home/user/Downloads", EICAR: true},
		{Step: StepDone, TargetPath: "/tmp/x.png", SizeLabel: "1 KiB", Bytes: 1024, FormatName: "PNG"},
		{Step: StepDownloadDir, Input: "> ~/Downloads", DownloadReason: "No standard download directory is configured yet.", Error: "x"},
	}
	for _, w := range []int{40, 60, 80} {
		for _, s := range steps {
			s.Width = w
			for _, line := range strings.Split(Render(s), "\n") {
				if got := ansi.StringWidth(line); got > w {
					t.Errorf("width %d, step %v: line is %d cells: %q", w, s.Step, got, ansi.Strip(line))
				}
			}
		}
	}
}

func nonASCII(s string) (rune, bool) {
	for _, r := range s {
		if r > 0x7e || (r < 0x20 && r != '\n') {
			return r, true
		}
	}
	return 0, false
}

func TestRenderPlainModeIsAsciiAndColorless(t *testing.T) {
	states := []State{
		{Step: StepName, Input: "> report"},
		{Step: StepExt, Name: "report", Input: "> p", Suggestions: []Suggestion{
			{Ext: "pdf", Name: "Portable Document Format", Category: "Documents", MinSize: "595 B", Selected: true},
			{Ext: "png", Name: "Portable Network Graphics", Category: "Images", MinSize: "70 B"},
		}, MoreSuggestions: 4},
		{Step: StepSize, FormatName: "PNG", TargetPath: "report.png", MinSize: "70 B", MinBytes: 70, Hint: "tab: use 72"},
		{Step: StepLocation, TargetPath: "eicar.zip", CurrentDir: "/a", DownloadDir: "/b", EICAR: true},
		{Step: StepConfirm, TargetPath: "/tmp/x.png"},
		{Step: StepGenerating, TargetPath: "big.zip", Spinner: "|", SizeLabel: "1 GiB"},
		{Step: StepDone, TargetPath: "/tmp/x.png", SizeLabel: "1 KiB", Bytes: 1024, FormatName: "PNG"},
		{Step: StepDownloadDir, Input: "> ~/dl", DownloadReason: "No standard download directory is configured yet."},
	}
	for _, s := range states {
		s.Plain = true
		s.Width = 70
		out := Render(s)
		if strings.ContainsRune(out, 0x1b) {
			t.Errorf("step %v: plain output still contains ANSI escapes", s.Step)
		}
		if r, bad := nonASCII(out); bad {
			t.Errorf("step %v: plain output has non-ASCII rune %q:\n%s", s.Step, r, out)
		}
		for _, line := range strings.Split(out, "\n") {
			if len(line) > 70 {
				t.Errorf("step %v: line exceeds 70 cells: %q", s.Step, line)
			}
		}
	}
}

func TestRenderPlainKeepsContentAndUsesAsciiFrame(t *testing.T) {
	out := Render(State{Step: StepName, Plain: true, Input: "> report", Width: 70})
	for _, want := range []string{"bbdx_file_gen", "Name", "File name", "> report", "+", "|", "-"} {
		if !strings.Contains(out, want) {
			t.Errorf("plain output lacks %q:\n%s", want, out)
		}
	}
	if strings.ContainsAny(out, "╭│●❯▍") {
		t.Errorf("plain output must not use box-drawing glyphs:\n%s", out)
	}
}

func TestRenderDefaultStaysUnicode(t *testing.T) {
	out := Render(State{Step: StepName, Input: "> report", Width: 70})
	if !strings.ContainsRune(out, '╭') {
		t.Errorf("default mode must keep the rounded frame:\n%s", out)
	}
}
