package controller

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bromanodlx/bbdx_file_gen/internal/model"
	"github.com/bromanodlx/bbdx_file_gen/internal/view"
)

// clock is a settable time source.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

// newUpdateHarness starts the wizard with a manual clock and no automatic
// update check; tests deliver the check's result with h.send.
func newUpdateHarness(t *testing.T) (*harness, *clock) {
	t.Helper()
	h := newHarness(t)
	clk := &clock{t: time.Unix(1_000_000, 0)}
	h.c.setup.Now, h.c.setup.Version = clk.now, "1.4.1"
	return h, clk
}

// popUp delivers a positive update check and lets the grace period pass.
func (h *harness) popUp(clk *clock, version string) {
	h.t.Helper()
	h.send(updateMsg{version})
	h.expectStep(view.StepUpdate)
	clk.t = clk.t.Add(updateGrace)
}

func TestInitRunsUpdateCheckInBackground(t *testing.T) {
	c := New(&model.Service{Registry: model.Default()}, Setup{
		Current: t.TempDir(), Home: t.TempDir(), Store: &memStore{cfg: model.Config{DownloadDir: t.TempDir()}},
		CheckUpdate: func() string { return "9.0.0" },
	})
	cmd := c.Init()
	if cmd == nil {
		t.Fatal("Init starts no update check")
	}
	// The wizard is usable before the check has answered.
	if c.State().Step != view.StepName {
		t.Fatalf("step = %v before the check answered", c.State().Step)
	}
	if msg, ok := cmd().(updateMsg); !ok || msg.version != "9.0.0" {
		t.Fatalf("check result = %#v", msg)
	}

	c = New(&model.Service{Registry: model.Default()}, Setup{Store: &memStore{}})
	if c.Init() != nil {
		t.Error("Init without CheckUpdate returns a command")
	}
}

func TestNoNewerVersionShowsNothing(t *testing.T) {
	h, _ := newUpdateHarness(t)
	h.typ("rep")
	h.send(updateMsg{""})
	h.expectStep(view.StepName)
	if v := h.c.nameInput.Value(); v != "rep" {
		t.Errorf("name = %q", v)
	}
}

func TestUpdateQuestionPopsUpOverCurrentStep(t *testing.T) {
	h, clk := newUpdateHarness(t)
	h.typ("report")
	h.key(tea.KeyEnter)
	h.typ("pd")
	h.expectStep(view.StepExt)

	h.popUp(clk, "1.5.0")
	out := h.screen()
	for _, want := range []string{"Update available", "1.5.0", "installed: 1.4.1", "[y/N]", "y install & restart"} {
		if !strings.Contains(out, want) {
			t.Errorf("question lacks %q:\n%s", want, out)
		}
	}
}

func TestDecliningUpdateReturnsWithInputsIntact(t *testing.T) {
	for name, decline := range map[string]func(*harness){
		"n":     func(h *harness) { h.typ("n") },
		"N":     func(h *harness) { h.typ("N") },
		"esc":   func(h *harness) { h.key(tea.KeyEsc) },
		"enter": func(h *harness) { h.key(tea.KeyEnter) },
	} {
		h, clk := newUpdateHarness(t)
		h.typ("report")
		h.key(tea.KeyEnter)
		h.typ("pd")
		h.popUp(clk, "1.5.0")
		decline(h)
		h.expectStep(view.StepExt)
		if h.quit || h.c.UpdateRequested() != "" {
			t.Errorf("%s: quit %v, requested %q", name, h.quit, h.c.UpdateRequested())
		}
		if n, e := h.c.nameInput.Value(), h.c.extInput.Value(); n != "report" || e != "pd" {
			t.Errorf("%s: inputs = %q, %q", name, n, e)
		}
		// typing continues in the step's input
		h.typ("f")
		if e := h.c.extInput.Value(); e != "pdf" {
			t.Errorf("%s: ext after typing = %q", name, e)
		}
		// asked once per start
		h.send(updateMsg{"1.5.0"})
		h.expectStep(view.StepExt)
	}
}

func TestAcceptingUpdateQuitsAndRequestsIt(t *testing.T) {
	for _, k := range []string{"y", "Y"} {
		h, clk := newUpdateHarness(t)
		h.popUp(clk, "1.5.0")
		h.typ(k)
		if !h.quit || h.c.UpdateRequested() != "1.5.0" {
			t.Errorf("%s: quit %v, requested %q", k, h.quit, h.c.UpdateRequested())
		}
	}
}

func TestKeysRightAfterPopUpAreIgnored(t *testing.T) {
	h, clk := newUpdateHarness(t)
	h.typ("ma")
	h.send(updateMsg{"1.5.0"})
	h.typ("y") // meant for the file name
	clk.t = clk.t.Add(updateGrace / 2)
	h.typ("n")
	h.expectStep(view.StepUpdate)
	if h.quit || h.c.UpdateRequested() != "" {
		t.Fatalf("early key answered the question: quit %v requested %q", h.quit, h.c.UpdateRequested())
	}
	clk.t = clk.t.Add(updateGrace)
	h.typ("n")
	h.expectStep(view.StepName)
	if v := h.c.nameInput.Value(); v != "ma" {
		t.Errorf("name = %q", v)
	}
}

func TestUpdateWaitsForGenerationToFinish(t *testing.T) {
	h, clk := newUpdateHarness(t)
	h.c.goTo(view.StepGenerating)
	h.send(updateMsg{"1.5.0"})
	h.expectStep(view.StepGenerating)
	h.send(generatedMsg{res: model.Result{Path: "x.pdf", Size: 10}})
	h.expectStep(view.StepUpdate)
	clk.t = clk.t.Add(updateGrace)
	h.typ("n")
	h.expectStep(view.StepDone)
}

func TestErrorSurvivesUpdateQuestion(t *testing.T) {
	h, clk := newUpdateHarness(t)
	h.key(tea.KeyEnter) // empty name
	errMsg := h.c.State().Error
	if errMsg == "" {
		t.Fatal("no error")
	}
	h.popUp(clk, "1.5.0")
	h.key(tea.KeyEsc)
	h.expectStep(view.StepName)
	if got := h.c.State().Error; got != errMsg {
		t.Errorf("error = %q, want %q", got, errMsg)
	}
}

func TestCtrlDAndCtrlCOnUpdateQuestion(t *testing.T) {
	h, clk := newUpdateHarness(t)
	h.popUp(clk, "1.5.0")
	h.key(tea.KeyCtrlD)
	h.expectStep(view.StepUpdate)
	h.key(tea.KeyCtrlC)
	if !h.quit || h.c.UpdateRequested() != "" {
		t.Errorf("ctrl+c: quit %v requested %q", h.quit, h.c.UpdateRequested())
	}
}
