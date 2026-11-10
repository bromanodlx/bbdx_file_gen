package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"bbdx_file_gen/internal/model"
	"bbdx_file_gen/internal/view"
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
		view.StepConfirm: false, view.StepGenerating: false, view.StepDownloadDir: false,
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
