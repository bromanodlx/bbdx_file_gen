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
