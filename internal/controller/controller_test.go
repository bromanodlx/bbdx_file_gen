package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"bbdx_file_gen/internal/model"
	"bbdx_file_gen/internal/view"
)

type harness struct {
	t         *testing.T
	c         *Controller
	dir       string // stands in for the current directory
	downloads string // stands in for ~/Downloads (not created yet)
	quit      bool
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	dl := filepath.Join(t.TempDir(), "Downloads")
	c := New(&model.Service{Registry: model.Default()}, Locations{Current: dir, Downloads: dl})
	h := &harness{t: t, c: c, dir: dir, downloads: dl}
	h.run(h.c.Init())
	return h
}

// run executes commands synchronously and feeds their messages back.
func (h *harness) run(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.QuitMsg:
		h.quit = true
	case tea.BatchMsg:
		for _, c := range msg {
			h.run(c)
		}
	case spinnerTick: // animation only, never loop on it
	default:
		h.send(msg)
	}
}

func (h *harness) send(msg tea.Msg) {
	m, cmd := h.c.Update(msg)
	h.c = m.(*Controller)
	h.run(cmd)
}

func (h *harness) typ(s string) {
	for _, r := range s {
		h.send(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func (h *harness) key(k tea.KeyType) { h.send(tea.KeyMsg{Type: k}) }

func (h *harness) screen() string { return ansi.Strip(h.c.View()) }

func (h *harness) step() view.Step { return h.c.State().Step }

func (h *harness) expectStep(want view.Step) {
	h.t.Helper()
	if got := h.step(); got != want {
		h.t.Fatalf("step = %v, want %v\n%s", got, want, h.screen())
	}
}

func fileSize(t *testing.T, p string) int64 {
	t.Helper()
	st, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.Size()
}

func TestStartsWithNamePrompt(t *testing.T) {
	h := newHarness(t)
	h.expectStep(view.StepName)
	if !strings.Contains(h.screen(), "File name") {
		t.Error(h.screen())
	}
}

func TestEmptyNameIsRejected(t *testing.T) {
	h := newHarness(t)
	h.key(tea.KeyEnter)
	h.expectStep(view.StepName)
	if h.c.State().Error == "" {
		t.Error("expected an error message")
	}
}

func TestFullFlowWithMinimumSize(t *testing.T) {
	h := newHarness(t)
	h.typ("report")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepExt)

	h.typ("pn")
	if s := h.c.State().Suggestions; len(s) == 0 || s[0].Ext != "png" || !s[0].Selected {
		t.Fatalf("suggestions = %+v", s)
	}
	h.key(tea.KeyEnter)
	h.expectStep(view.StepSize)
	png, _ := model.Default().Lookup("png")
	if st := h.c.State(); st.MinBytes != png.Rule.Min || st.TargetPath != "report.png" {
		t.Fatalf("size state = %+v", st)
	}

	h.key(tea.KeyEnter) // empty = minimum
	h.expectStep(view.StepLocation)
	h.typ("y")
	h.expectStep(view.StepDone)
	if got := fileSize(t, filepath.Join(h.dir, "report.png")); got != png.Rule.Min {
		t.Errorf("file has %d bytes, want minimum %d", got, png.Rule.Min)
	}
	if !strings.Contains(h.screen(), "report.png") {
		t.Error(h.screen())
	}
}

func TestExplicitSizeWithUnit(t *testing.T) {
	h := newHarness(t)
	h.typ("data")
	h.key(tea.KeyEnter)
	h.typ("json")
	h.key(tea.KeyEnter)
	h.typ("2KiB")
	h.key(tea.KeyEnter)
	h.typ("y")
	h.expectStep(view.StepDone)
	if got := fileSize(t, filepath.Join(h.dir, "data.json")); got != 2048 {
		t.Errorf("file has %d bytes", got)
	}
}

func TestInvalidSizeTextStaysOnStep(t *testing.T) {
	h := newHarness(t)
	h.typ("a")
	h.key(tea.KeyEnter)
	h.typ("txt")
	h.key(tea.KeyEnter)
	h.typ("lots")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepSize)
	if h.c.State().Error == "" {
		t.Error("expected parse error")
	}
}

func TestUnreachableSizeOffersNearestAndTabAppliesIt(t *testing.T) {
	h := newHarness(t)
	h.typ("t")
	h.key(tea.KeyEnter)
	h.typ("tar")
	h.key(tea.KeyEnter)
	h.typ("1500")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepSize)
	st := h.c.State()
	if !strings.Contains(st.Error, "1536") || !strings.Contains(st.Hint, "1536") {
		t.Fatalf("error %q / hint %q should offer 1536", st.Error, st.Hint)
	}
	h.key(tea.KeyTab)
	if h.c.sizeInput.Value() != "1536" {
		t.Fatalf("tab should fill in 1536, input = %q", h.c.sizeInput.Value())
	}
	h.key(tea.KeyEnter)
	h.typ("y")
	h.expectStep(view.StepDone)
	if got := fileSize(t, filepath.Join(h.dir, "t.tar")); got != 1536 {
		t.Errorf("file has %d bytes", got)
	}
}

func TestUnknownExtensionIsRejected(t *testing.T) {
	h := newHarness(t)
	h.typ("a")
	h.key(tea.KeyEnter)
	h.typ("xyz")
	if len(h.c.State().Suggestions) != 0 {
		t.Fatal("xyz must not match anything")
	}
	h.key(tea.KeyEnter)
	h.expectStep(view.StepExt)
	if !strings.Contains(h.c.State().Error, "xyz") {
		t.Errorf("error = %q", h.c.State().Error)
	}
}

func TestArrowKeysAndTabInSuggestions(t *testing.T) {
	h := newHarness(t)
	h.typ("a")
	h.key(tea.KeyEnter)
	h.typ("p")
	first := h.c.State().Suggestions[0].Ext
	h.key(tea.KeyDown)
	sel := selected(h.c.State().Suggestions)
	if sel == "" || sel == first {
		t.Fatalf("down should move the selection, selected = %q", sel)
	}
	h.key(tea.KeyUp)
	if selected(h.c.State().Suggestions) != first {
		t.Fatal("up should move back")
	}
	h.key(tea.KeyUp) // stays at the top
	if selected(h.c.State().Suggestions) != first {
		t.Fatal("up at the top must not wrap")
	}
	h.key(tea.KeyDown)
	h.key(tea.KeyTab)
	if h.c.extInput.Value() != sel {
		t.Errorf("tab should complete to %q, got %q", sel, h.c.extInput.Value())
	}
}

func TestExactExtensionWinsOverSelection(t *testing.T) {
	h := newHarness(t)
	h.typ("a")
	h.key(tea.KeyEnter)
	h.typ("c") // matches c, css, csv
	h.key(tea.KeyEnter)
	h.expectStep(view.StepSize)
	if h.c.State().Ext != "c" {
		t.Errorf("ext = %q, want c", h.c.State().Ext)
	}
}

func TestAliasIsAccepted(t *testing.T) {
	h := newHarness(t)
	h.typ("photo")
	h.key(tea.KeyEnter)
	h.typ(".JPEG")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepSize)
	if h.c.State().TargetPath != "photo.jpg" {
		t.Errorf("target = %q", h.c.State().TargetPath)
	}
}

func TestNameWithKnownExtensionPrefillsType(t *testing.T) {
	h := newHarness(t)
	h.typ("song.mp3")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepExt)
	if h.c.extInput.Value() != "mp3" {
		t.Errorf("ext input = %q, want mp3", h.c.extInput.Value())
	}
	h.key(tea.KeyEnter)
	if h.c.State().TargetPath != "song.mp3" {
		t.Errorf("target = %q", h.c.State().TargetPath)
	}
}

func TestEscapeGoesBackAndKeepsInput(t *testing.T) {
	h := newHarness(t)
	h.typ("keep")
	h.key(tea.KeyEnter)
	h.typ("zip")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepSize)
	h.key(tea.KeyEsc)
	h.expectStep(view.StepExt)
	if h.c.extInput.Value() != "zip" {
		t.Errorf("ext input lost: %q", h.c.extInput.Value())
	}
	h.key(tea.KeyEsc)
	h.expectStep(view.StepName)
	if h.c.nameInput.Value() != "keep" {
		t.Errorf("name input lost: %q", h.c.nameInput.Value())
	}
}

func TestOverwriteConfirmation(t *testing.T) {
	h := newHarness(t)
	target := filepath.Join(h.dir, "dup.txt")
	if err := os.WriteFile(target, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.typ("dup")
	h.key(tea.KeyEnter)
	h.typ("txt")
	h.key(tea.KeyEnter)
	h.typ("100")
	h.key(tea.KeyEnter)
	h.typ("y")
	h.expectStep(view.StepConfirm)

	h.typ("n")
	h.expectStep(view.StepLocation)
	if got := fileSize(t, target); got != 8 {
		t.Fatal("declining must keep the original file")
	}

	h.typ("y")
	h.expectStep(view.StepConfirm)
	h.typ("y")
	h.expectStep(view.StepDone)
	if got := fileSize(t, target); got != 100 {
		t.Errorf("file has %d bytes after overwrite", got)
	}
}

func TestDoneAllowsAnotherFileOrQuit(t *testing.T) {
	h := newHarness(t)
	h.typ("one")
	h.key(tea.KeyEnter)
	h.typ("md")
	h.key(tea.KeyEnter)
	h.key(tea.KeyEnter)
	h.typ("y")
	h.expectStep(view.StepDone)

	h.key(tea.KeyEnter)
	h.expectStep(view.StepName)
	if h.c.nameInput.Value() != "" || h.c.extInput.Value() != "" || h.c.sizeInput.Value() != "" {
		t.Error("a new run must start with empty inputs")
	}

	h.typ("two")
	h.key(tea.KeyEnter)
	h.typ("md")
	h.key(tea.KeyEnter)
	h.key(tea.KeyEnter)
	h.typ("y")
	h.typ("q")
	if !h.quit {
		t.Error("q on the done screen must quit")
	}
}

func TestCtrlCQuitsFromAnyStep(t *testing.T) {
	h := newHarness(t)
	h.typ("x")
	h.key(tea.KeyEnter)
	h.key(tea.KeyCtrlC)
	if !h.quit {
		t.Error("ctrl+c must quit")
	}
}

func TestGenerationErrorIsShown(t *testing.T) {
	h := newHarness(t)
	h.typ("missing/dir/file")
	h.key(tea.KeyEnter)
	h.typ("txt")
	h.key(tea.KeyEnter)
	h.key(tea.KeyEnter)
	h.typ("y")
	h.expectStep(view.StepLocation)
	if !strings.Contains(h.c.State().Error, "does not exist") {
		t.Errorf("error = %q", h.c.State().Error)
	}
}

func TestWindowSizeIsPassedToView(t *testing.T) {
	h := newHarness(t)
	h.send(tea.WindowSizeMsg{Width: 50, Height: 20})
	if st := h.c.State(); st.Width != 50 || st.Height != 20 {
		t.Errorf("state size = %dx%d", st.Width, st.Height)
	}
}

func selected(s []view.Suggestion) string {
	for _, sg := range s {
		if sg.Selected {
			return sg.Ext
		}
	}
	return ""
}

// toLocation walks the wizard up to the location question.
func (h *harness) toLocation(name, ext string) {
	h.t.Helper()
	h.typ(name)
	h.key(tea.KeyEnter)
	h.typ(ext)
	h.key(tea.KeyEnter)
	h.key(tea.KeyEnter)
	h.expectStep(view.StepLocation)
}

func TestLocationQuestionShowsBothDirectories(t *testing.T) {
	h := newHarness(t)
	h.toLocation("a", "txt")
	out := h.screen()
	for _, want := range []string{"current directory", h.dir, h.downloads} {
		if !strings.Contains(out, want) {
			t.Errorf("location screen lacks %q:\n%s", want, out)
		}
	}
}

func TestYesCreatesInCurrentDirectory(t *testing.T) {
	h := newHarness(t)
	h.toLocation("here", "txt")
	h.typ("y")
	h.expectStep(view.StepDone)
	fileSize(t, filepath.Join(h.dir, "here.txt"))
	if _, err := os.Stat(h.downloads); err == nil {
		t.Error("the download directory must not be touched")
	}
}

func TestNoCreatesInDownloadDirectory(t *testing.T) {
	h := newHarness(t)
	h.toLocation("there", "json")
	h.typ("n")
	h.expectStep(view.StepDone)
	want := filepath.Join(h.downloads, "there.json")
	fileSize(t, want)
	if h.c.State().TargetPath != want {
		t.Errorf("done screen shows %q, want %q", h.c.State().TargetPath, want)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "there.json")); err == nil {
		t.Error("file must not be created in the current directory")
	}
}

func TestLocationArrowsAndEnter(t *testing.T) {
	h := newHarness(t)
	h.toLocation("arrow", "txt")
	if !h.c.State().LocationYes {
		t.Fatal("yes (current directory) must be preselected")
	}
	h.key(tea.KeyRight)
	if h.c.State().LocationYes {
		t.Fatal("right must select no")
	}
	h.key(tea.KeyLeft)
	h.key(tea.KeyRight)
	h.key(tea.KeyEnter)
	h.expectStep(view.StepDone)
	fileSize(t, filepath.Join(h.downloads, "arrow.txt"))
}

func TestEnterAcceptsPreselectedYes(t *testing.T) {
	h := newHarness(t)
	h.toLocation("default", "txt")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepDone)
	fileSize(t, filepath.Join(h.dir, "default.txt"))
}

func TestEscFromLocationGoesBackToSize(t *testing.T) {
	h := newHarness(t)
	h.toLocation("a", "txt")
	h.key(tea.KeyEsc)
	h.expectStep(view.StepSize)
}

func TestAbsoluteNameSkipsLocationQuestion(t *testing.T) {
	h := newHarness(t)
	abs := filepath.Join(t.TempDir(), "absolute")
	h.typ(abs)
	h.key(tea.KeyEnter)
	h.typ("txt")
	h.key(tea.KeyEnter)
	h.key(tea.KeyEnter)
	h.expectStep(view.StepDone)
	fileSize(t, abs+".txt")
}
