package controller

import (
	"errors"
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
	store     *memStore
	dir       string // stands in for the current directory
	home      string
	downloads string // the configured standard download directory
	quit      bool
}

// memStore is an in-memory settings store.
type memStore struct {
	cfg     model.Config
	loadErr error
	saveErr error
	saves   int
}

func (m *memStore) Load() (model.Config, error) { return m.cfg, m.loadErr }

func (m *memStore) Save(c model.Config) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.cfg = c
	m.saves++
	return nil
}

// newHarness starts with a valid, configured download directory.
func newHarness(t *testing.T) *harness {
	t.Helper()
	dl := t.TempDir()
	return newHarnessWith(t, &memStore{cfg: model.Config{DownloadDir: dl}})
}

func newHarnessWith(t *testing.T, store *memStore) *harness {
	t.Helper()
	dir, home := t.TempDir(), t.TempDir()
	c := New(&model.Service{Registry: model.Default()}, Setup{
		Current: dir, Home: home, Suggested: filepath.Join(home, "Downloads"), Store: store,
	})
	h := &harness{t: t, c: c, store: store, dir: dir, home: home, downloads: store.cfg.DownloadDir}
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
	if entries, _ := os.ReadDir(h.downloads); len(entries) != 0 {
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

// --- standard download directory ---

func mkdir(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func (h *harness) setInput(v string) {
	h.c.dlInput.SetValue(v)
	h.c.dlInput.CursorEnd()
}

func TestFirstRunAsksForDownloadDir(t *testing.T) {
	h := newHarnessWith(t, &memStore{})
	h.expectStep(view.StepDownloadDir)
	st := h.c.State()
	if !strings.Contains(st.DownloadReason, "No standard download directory") {
		t.Errorf("reason = %q", st.DownloadReason)
	}
	if h.c.dlInput.Value() != filepath.Join(h.home, "Downloads") {
		t.Errorf("input must be prefilled with the suggestion, got %q", h.c.dlInput.Value())
	}
	mkdir(t, filepath.Join(h.home, "Downloads"))
	h.key(tea.KeyEnter)
	h.expectStep(view.StepName)
	if h.store.cfg.DownloadDir != filepath.Join(h.home, "Downloads") || h.store.saves != 1 {
		t.Errorf("stored %+v after %d saves", h.store.cfg, h.store.saves)
	}
}

func TestSuggestionIsNeverUsedWithoutConfirmation(t *testing.T) {
	h := newHarnessWith(t, &memStore{})
	mkdir(t, filepath.Join(h.home, "Downloads"))
	h.key(tea.KeyEsc) // first run: esc quits instead of silently using the suggestion
	if !h.quit || h.store.saves != 0 {
		t.Errorf("quit=%v saves=%d", h.quit, h.store.saves)
	}
}

func TestUnreachableConfiguredDirAsksAgain(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "removed")
	h := newHarnessWith(t, &memStore{cfg: model.Config{DownloadDir: gone}})
	h.expectStep(view.StepDownloadDir)
	if r := h.c.State().DownloadReason; !strings.Contains(r, gone) || !strings.Contains(r, "does not exist") {
		t.Errorf("reason = %q", r)
	}
	if h.c.dlInput.Value() != gone {
		t.Errorf("input should show the configured path, got %q", h.c.dlInput.Value())
	}
}

func TestCorruptConfigAsksForDownloadDir(t *testing.T) {
	h := newHarnessWith(t, &memStore{loadErr: errors.New("config is corrupt")})
	h.expectStep(view.StepDownloadDir)
	if !strings.Contains(h.c.State().DownloadReason, "corrupt") {
		t.Errorf("reason = %q", h.c.State().DownloadReason)
	}
}

func TestDownloadDirAcceptsRelativePath(t *testing.T) {
	h := newHarnessWith(t, &memStore{})
	mkdir(t, filepath.Join(h.dir, "out", "dl"))
	h.setInput("out/./dl/")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepName)
	if want := filepath.Join(h.dir, "out", "dl"); h.store.cfg.DownloadDir != want {
		t.Errorf("stored %q, want absolute %q", h.store.cfg.DownloadDir, want)
	}
}

func TestDownloadDirAcceptsTildeAndAbsolutePath(t *testing.T) {
	h := newHarnessWith(t, &memStore{})
	mkdir(t, filepath.Join(h.home, "Herunterladen"))
	h.setInput("~/Herunterladen")
	h.key(tea.KeyEnter)
	if want := filepath.Join(h.home, "Herunterladen"); h.store.cfg.DownloadDir != want {
		t.Errorf("stored %q, want %q", h.store.cfg.DownloadDir, want)
	}

	abs := t.TempDir()
	h.key(tea.KeyCtrlD)
	h.setInput(abs)
	h.key(tea.KeyEnter)
	if h.store.cfg.DownloadDir != abs {
		t.Errorf("stored %q, want %q", h.store.cfg.DownloadDir, abs)
	}
}

func TestInvalidDownloadDirIsRejected(t *testing.T) {
	h := newHarnessWith(t, &memStore{})
	h.setInput(filepath.Join(h.dir, "missing"))
	h.key(tea.KeyEnter)
	h.expectStep(view.StepDownloadDir)
	if !strings.Contains(h.c.State().Error, "does not exist") || h.store.saves != 0 {
		t.Errorf("error %q, saves %d", h.c.State().Error, h.store.saves)
	}
}

func TestDownloadDirSaveFailureIsShown(t *testing.T) {
	h := newHarnessWith(t, &memStore{saveErr: errors.New("disk full")})
	mkdir(t, filepath.Join(h.home, "Downloads"))
	h.key(tea.KeyEnter)
	h.expectStep(view.StepDownloadDir)
	if !strings.Contains(h.c.State().Error, "disk full") {
		t.Errorf("error = %q", h.c.State().Error)
	}
}

func TestCtrlDChangesDownloadDirAtAnyTime(t *testing.T) {
	for _, tc := range []struct {
		name string
		goTo func(h *harness)
		want view.Step
	}{
		{"name", func(h *harness) { h.typ("kept") }, view.StepName},
		{"ext", func(h *harness) { h.typ("kept"); h.key(tea.KeyEnter); h.typ("pd") }, view.StepExt},
		{"size", func(h *harness) { h.typ("kept"); h.key(tea.KeyEnter); h.typ("pdf"); h.key(tea.KeyEnter) }, view.StepSize},
		{"location", func(h *harness) { h.toLocation("kept", "pdf") }, view.StepLocation},
		{"done", func(h *harness) { h.toLocation("kept", "pdf"); h.typ("y") }, view.StepDone},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			tc.goTo(h)
			before := h.c.nameInput.Value() + "|" + h.c.extInput.Value()
			h.key(tea.KeyCtrlD)
			h.expectStep(view.StepDownloadDir)
			if h.c.dlInput.Value() != h.downloads {
				t.Errorf("input must show the current setting, got %q", h.c.dlInput.Value())
			}
			if !h.c.State().DownloadCancel {
				t.Error("changing an existing setting must be cancellable")
			}
			newDir := t.TempDir()
			h.setInput(newDir)
			h.key(tea.KeyEnter)
			h.expectStep(tc.want)
			if after := h.c.nameInput.Value() + "|" + h.c.extInput.Value(); after != before {
				t.Errorf("inputs changed from %q to %q", before, after)
			}
			if h.store.cfg.DownloadDir != newDir || h.c.State().DownloadDir != newDir {
				t.Errorf("setting not applied: store %q, state %q", h.store.cfg.DownloadDir, h.c.State().DownloadDir)
			}
		})
	}
}

func TestNewDownloadDirIsUsedForNo(t *testing.T) {
	h := newHarness(t)
	newDir := t.TempDir()
	h.key(tea.KeyCtrlD)
	h.setInput(newDir)
	h.key(tea.KeyEnter)
	h.toLocation("moved", "txt")
	h.typ("n")
	h.expectStep(view.StepDone)
	fileSize(t, filepath.Join(newDir, "moved.txt"))
}

func TestEscCancelsDownloadDirChange(t *testing.T) {
	h := newHarness(t)
	h.typ("abc")
	h.key(tea.KeyCtrlD)
	h.setInput("/somewhere/else")
	h.key(tea.KeyEsc)
	h.expectStep(view.StepName)
	if h.quit || h.store.cfg.DownloadDir != h.downloads || h.store.saves != 0 {
		t.Errorf("cancel must keep the setting: quit=%v cfg=%q saves=%d", h.quit, h.store.cfg.DownloadDir, h.store.saves)
	}
}

func TestTabCompletesDownloadDir(t *testing.T) {
	h := newHarnessWith(t, &memStore{})
	mkdir(t, filepath.Join(h.home, "Downloads"))
	h.setInput("~/Dow")
	h.key(tea.KeyTab)
	if h.c.dlInput.Value() != "~/Downloads/" {
		t.Errorf("completed to %q", h.c.dlInput.Value())
	}
}

func TestCtrlDIgnoredWhileGeneratingOrConfirming(t *testing.T) {
	h := newHarness(t)
	os.WriteFile(filepath.Join(h.dir, "dup.txt"), nil, 0o644)
	h.toLocation("dup", "txt")
	h.typ("y")
	h.expectStep(view.StepConfirm)
	h.key(tea.KeyCtrlD)
	h.expectStep(view.StepConfirm)
}

// --- EICAR test files ---

func TestEICARNameSkipsSizeStepAndWritesTheString(t *testing.T) {
	h := newHarness(t)
	h.typ("eicar")
	h.key(tea.KeyEnter)
	h.typ("txt")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepLocation) // the size step is skipped
	h.typ("y")
	h.expectStep(view.StepDone)
	data, err := os.ReadFile(filepath.Join(h.dir, "eicar.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != model.EICAR {
		t.Errorf("content = %q", data)
	}
	if st := h.c.State(); st.Bytes != 68 {
		t.Errorf("done screen size = %d, want 68", st.Bytes)
	}
}

func TestEICARNameIsCaseInsensitive(t *testing.T) {
	h := newHarness(t)
	h.typ("EiCaR")
	h.key(tea.KeyEnter)
	h.typ("txt")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepLocation)
	if !h.c.State().EICAR {
		t.Error("state must mark the EICAR mode")
	}
}

func TestEICAREscFromLocationReturnsToType(t *testing.T) {
	h := newHarness(t)
	h.typ("eicar")
	h.key(tea.KeyEnter)
	h.typ("txt")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepLocation)
	h.key(tea.KeyEsc) // no size step to go back to
	h.expectStep(view.StepExt)
}

func TestEICARAbsoluteNameSkipsSizeAndLocation(t *testing.T) {
	h := newHarness(t)
	abs := filepath.Join(t.TempDir(), "eicar")
	h.typ(abs)
	h.key(tea.KeyEnter)
	h.typ("txt")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepDone)
	data, err := os.ReadFile(abs + ".txt")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != model.EICAR {
		t.Errorf("content = %q", data)
	}
}

func TestNonEICARNameStillAsksForSize(t *testing.T) {
	h := newHarness(t)
	h.typ("report")
	h.key(tea.KeyEnter)
	h.typ("txt")
	h.key(tea.KeyEnter)
	h.expectStep(view.StepSize)
}
