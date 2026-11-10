package controller

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bromanodlx/bbdx_file_gen/internal/model"
	"github.com/bromanodlx/bbdx_file_gen/internal/view"
)

func TestListRowsGroupsByCategory(t *testing.T) {
	rows := ListRows(model.Default())
	if len(rows) != 50 {
		t.Fatalf("%d rows", len(rows))
	}
	for i := 1; i < len(rows); i++ {
		a, b := rows[i-1], rows[i]
		if a.Category > b.Category || (a.Category == b.Category && a.Ext >= b.Ext) {
			t.Errorf("rows %d/%d out of order: %s/%s then %s/%s", i-1, i, a.Category, a.Ext, b.Category, b.Ext)
		}
	}
	for _, r := range rows {
		if r.Ext == "jpg" && (r.Aliases != "jpeg,jpe" || r.MinSize != "141 B" || r.Category != "Images") {
			t.Errorf("jpg row = %+v", r)
		}
	}
}

func TestTabCompletionDescendsIntoEscapedDirectory(t *testing.T) {
	h := newHarness(t)
	mkdir(t, filepath.Join(h.dir, "my dir", "target"))
	h.key(tea.KeyCtrlD)
	h.setInput("my")
	h.key(tea.KeyTab)
	h.typ("t")
	h.key(tea.KeyTab)
	if got := h.c.dlInput.Value(); got != `my\ dir/target/` {
		t.Fatalf("input after two tabs = %q", got)
	}
	h.key(tea.KeyEnter)
	if want := filepath.Join(h.dir, "my dir", "target"); h.store.cfg.DownloadDir != want {
		t.Errorf("saved %q, want %q", h.store.cfg.DownloadDir, want)
	}
}

func TestDanglingSymlinkAsksBeforeReplacing(t *testing.T) {
	h := newHarness(t)
	link := filepath.Join(h.dir, "x.txt")
	if err := os.Symlink(filepath.Join(h.dir, "nowhere"), link); err != nil {
		t.Skip(err)
	}
	h.toLocation("x", "txt")
	h.typ("y")
	h.expectStep(view.StepConfirm)
	h.typ("n")
	if _, err := os.Readlink(link); err != nil {
		t.Errorf("symlink gone after declining: %v", err)
	}
	h.typ("y") // location question again: current directory
	h.typ("y") // confirm overwrite
	h.expectStep(view.StepDone)
	if st, err := os.Lstat(link); err != nil || st.Mode()&os.ModeSymlink != 0 || st.Size() != 0 {
		t.Errorf("not replaced by a regular file: %v %v", st, err)
	}
}

func TestOnlyPlainLettersAnswerQuestions(t *testing.T) {
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'y'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'y'}, Paste: true},
		{Type: tea.KeyRunes, Runes: []rune("yes")},
	} {
		h := newHarness(t)
		h.toLocation("a", "txt")
		h.send(msg)
		h.expectStep(view.StepLocation)
	}
	for _, letter := range []string{"y", "Y"} {
		if !isKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(letter)}, "y") {
			t.Errorf("%q not accepted", letter)
		}
	}
}

func TestCanChangeDownloadDir(t *testing.T) {
	want := map[view.Step]bool{
		view.StepName: true, view.StepExt: true, view.StepSize: true, view.StepLocation: true, view.StepDone: true,
		view.StepConfirm: false, view.StepGenerating: false, view.StepDownloadDir: false, view.StepUpdate: false,
	}
	for step, ok := range want {
		if canChangeDownloadDir(step) != ok {
			t.Errorf("step %v: got %v", step, !ok)
		}
	}
}

func TestOnlyActiveStepHasInput(t *testing.T) {
	h := newHarness(t)
	h.toLocation("a", "txt")
	if st := h.c.State(); st.Input != "" {
		t.Errorf("location step renders an input: %q", st.Input)
	}
	for _, in := range []bool{h.c.nameInput.Focused(), h.c.extInput.Focused(), h.c.sizeInput.Focused(), h.c.dlInput.Focused()} {
		if in {
			t.Error("an input is focused on the location step")
		}
	}
	h.key(tea.KeyEsc)
	if !h.c.sizeInput.Focused() || !strings.HasPrefix(ansiStrip(h.c.State().Input), "❯ ") {
		t.Error("size input not focused after going back")
	}
}

// Regression: esc quits the mandatory first-run download directory screen,
// but on the update question popped up over it, esc only declines.
func TestUpdateOverMandatoryDownloadDirDoesNotQuit(t *testing.T) {
	h := newHarnessWith(t, &memStore{})
	h.expectStep(view.StepDownloadDir)
	clk := &clock{t: time.Unix(1_000_000, 0)}
	h.c.setup.Now = clk.now
	h.popUp(clk, "1.5.0")
	h.key(tea.KeyEsc)
	h.expectStep(view.StepDownloadDir)
	if h.quit {
		t.Fatal("esc on the update question quit the wizard")
	}
	if !h.c.dlInput.Focused() || !strings.Contains(h.screen(), "No standard download directory") {
		t.Errorf("download dir screen not restored:\n%s", h.screen())
	}
}

// Regression: an update found while generating pops up only afterwards,
// and when generation failed the error stays visible behind the question.
func TestUpdateAfterFailedGenerationKeepsError(t *testing.T) {
	h, clk := newUpdateHarness(t)
	h.toLocation("a", "txt")
	h.c.goTo(view.StepGenerating)
	h.send(updateMsg{"1.5.0"})
	h.expectStep(view.StepGenerating)
	h.send(generatedMsg{err: errors.New("disk full")})
	h.expectStep(view.StepUpdate)
	clk.t = clk.t.Add(updateGrace)
	h.typ("n")
	h.expectStep(view.StepLocation)
	if got := h.c.State().Error; got != "disk full" {
		t.Errorf("error = %q", got)
	}
}

// Regression: only a plain y accepts the update; alt+y, a pasted y or a
// word starting with y must not install anything.
func TestOnlyPlainYAcceptsUpdate(t *testing.T) {
	for _, msg := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'y'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'y'}, Paste: true},
		{Type: tea.KeyRunes, Runes: []rune("yes")},
		{Type: tea.KeySpace, Runes: []rune{' '}},
	} {
		h, clk := newUpdateHarness(t)
		h.popUp(clk, "1.5.0")
		h.send(msg)
		h.expectStep(view.StepUpdate)
		if h.quit || h.c.UpdateRequested() != "" {
			t.Errorf("%v: quit %v, requested %q", msg, h.quit, h.c.UpdateRequested())
		}
	}
}

// Regression: the grace period ends exactly after updateGrace.
func TestUpdateGraceBoundary(t *testing.T) {
	h, clk := newUpdateHarness(t)
	h.send(updateMsg{"1.5.0"})
	clk.t = clk.t.Add(updateGrace - time.Nanosecond)
	h.typ("y")
	if h.quit {
		t.Fatal("accepted inside the grace period")
	}
	clk.t = clk.t.Add(time.Nanosecond)
	h.typ("y")
	if !h.quit || h.c.UpdateRequested() != "1.5.0" {
		t.Errorf("not accepted right after the grace period: quit %v", h.quit)
	}
}

// Regression: a resize while the question is open is applied and keeps it.
func TestResizeDuringUpdateQuestion(t *testing.T) {
	h, clk := newUpdateHarness(t)
	h.popUp(clk, "1.5.0")
	h.send(tea.WindowSizeMsg{Width: 40, Height: 20})
	h.expectStep(view.StepUpdate)
	for _, line := range strings.Split(h.screen(), "\n") {
		if w := len([]rune(line)); w > 40 {
			t.Errorf("line wider than 40: %q", line)
		}
	}
}

// Regression: declining never requests an update, even after quitting.
func TestDeclineThenQuitRequestsNothing(t *testing.T) {
	h, clk := newUpdateHarness(t)
	h.popUp(clk, "1.5.0")
	h.typ("n")
	h.key(tea.KeyEsc) // quits from the name step
	if !h.quit || h.c.UpdateRequested() != "" {
		t.Errorf("quit %v, requested %q", h.quit, h.c.UpdateRequested())
	}
}
