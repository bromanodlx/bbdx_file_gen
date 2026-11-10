package controller

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/bromanodlx/bbdx_file_gen/internal/model"
)

// update regenerates the golden file: go test ./internal/controller -run Golden -update
var update = flag.Bool("update", false, "rewrite golden files instead of comparing against them")

const goldenTranscriptPath = "testdata/golden_transcript.txt"

// event is one scripted input: typed text, a special key, the background
// update check answering, or the update question's grace period passing.
type event struct {
	text   string
	key    tea.KeyType
	update *string
	wait   bool
}

func txt(s string) event       { return event{text: s} }
func k(key tea.KeyType) event  { return event{key: key} }
func upd(version string) event { return event{update: &version} }
func wait() event              { return event{wait: true} }
func (e event) String() string {
	switch {
	case e.text != "":
		return fmt.Sprintf("type %q", e.text)
	case e.update != nil:
		return fmt.Sprintf("update check answers %q", *e.update)
	case e.wait:
		return "grace period passes"
	}
	return "key " + tea.KeyMsg{Type: e.key}.String()
}

// goldenFlows drive the wizard through every branch: sizes and units,
// unreachable sizes, aliases, both locations, overwrite, EICAR, back
// navigation, the download directory screen and its completion.
var goldenFlows = []struct {
	name   string
	events []event
}{
	{"minimum-yes", []event{txt("report"), k(tea.KeyEnter), txt("pn"), k(tea.KeyEnter), k(tea.KeyEnter), txt("y"), k(tea.KeyEnter)}},
	{"units-no", []event{txt("data"), k(tea.KeyEnter), txt(".JSON"), k(tea.KeyEnter), txt("2KiB"), k(tea.KeyEnter), txt("n"), txt("q")}},
	{"unreachable-tab", []event{txt("t"), k(tea.KeyEnter), txt("tar"), k(tea.KeyEnter), txt("1500"), k(tea.KeyEnter), k(tea.KeyTab), k(tea.KeyEnter), k(tea.KeyRight), k(tea.KeyLeft), k(tea.KeyDown), k(tea.KeyEnter)}},
	{"bad-inputs", []event{k(tea.KeyEnter), txt("   "), k(tea.KeyEnter), txt("x/.."), k(tea.KeyEnter), k(tea.KeyBackspace), k(tea.KeyBackspace), k(tea.KeyBackspace), txt("y"), k(tea.KeyEnter), txt("nope"), k(tea.KeyEnter), k(tea.KeyBackspace), k(tea.KeyBackspace), k(tea.KeyBackspace), k(tea.KeyBackspace), txt("wav"), k(tea.KeyEnter), txt("lots"), k(tea.KeyEnter), k(tea.KeyBackspace), k(tea.KeyBackspace), k(tea.KeyBackspace), k(tea.KeyBackspace), txt("45"), k(tea.KeyEnter), txt("1.5B"), k(tea.KeyEnter)}},
	{"list-navigation", []event{txt("x"), k(tea.KeyEnter), k(tea.KeyDown), k(tea.KeyDown), k(tea.KeyUp), k(tea.KeyTab), k(tea.KeyBackspace), k(tea.KeyBackspace), k(tea.KeyBackspace), k(tea.KeyBackspace), k(tea.KeyBackspace)}},
	{"list-scroll", append(append([]event{txt("x"), k(tea.KeyEnter)}, repeatKey(tea.KeyDown, 12)...), repeatKey(tea.KeyUp, 12)...)},
	{"name-prefills-type-alias", []event{txt("pic.JPEG"), k(tea.KeyEnter), k(tea.KeyEnter), k(tea.KeyEnter), k(tea.KeyEsc), k(tea.KeyEsc), k(tea.KeyEsc), k(tea.KeyEsc)}},
	{"overwrite", []event{txt("dup"), k(tea.KeyEnter), txt("txt"), k(tea.KeyEnter), k(tea.KeyEnter), txt("y"), k(tea.KeyEnter), txt("dup"), k(tea.KeyEnter), txt("txt"), k(tea.KeyEnter), k(tea.KeyEnter), txt("y"), txt("n"), txt("Y"), txt("Y")}},
	{"eicar", []event{txt("EICAR.com"), k(tea.KeyEnter), k(tea.KeyEnter), k(tea.KeyEsc), k(tea.KeyEnter), txt("N")}},
	{"absolute", []event{txt("@ABS@/abs"), k(tea.KeyEnter), txt("csv"), k(tea.KeyEnter), txt("100"), k(tea.KeyEnter), k(tea.KeyEnter)}},
	{"absolute-eicar", []event{txt("@ABS@/eicar"), k(tea.KeyEnter), txt("zip"), k(tea.KeyEnter)}},
	{"ctrl-d", []event{txt("a"), k(tea.KeyCtrlD), k(tea.KeyEsc), k(tea.KeyEnter), k(tea.KeyCtrlD), k(tea.KeyCtrlU), txt("su"), k(tea.KeyTab), k(tea.KeyEnter), txt("txt"), k(tea.KeyEnter), k(tea.KeyEnter), txt("n"), k(tea.KeyCtrlD), k(tea.KeyCtrlU), txt("/nonexistent/dir"), k(tea.KeyEnter), k(tea.KeyEsc)}},
	{"generation-error", []event{txt("missing/sub/file"), k(tea.KeyEnter), txt("txt"), k(tea.KeyEnter), k(tea.KeyEnter), txt("y"), k(tea.KeyCtrlC)}},
	// The update question: it pops up over any step, ignores keys during the
	// grace period, returns to the step unchanged on no, quits on yes and is
	// asked once per start.
	{"update-none", []event{txt("a"), upd(""), txt("b")}},
	{"update-decline-keeps-inputs", []event{txt("report"), k(tea.KeyEnter), txt("pd"), upd("1.5.0"), wait(), txt("n"), txt("f"), k(tea.KeyEnter), upd("1.6.0"), k(tea.KeyEnter)}},
	{"update-early-keys-ignored", []event{txt("ma"), upd("1.5.0"), txt("y"), txt("n"), k(tea.KeyEnter), k(tea.KeyEsc), wait(), k(tea.KeyEsc), txt("p")}},
	{"update-accept", []event{txt("x"), k(tea.KeyEnter), upd("1.5.0"), wait(), txt("Y")}},
	{"update-keeps-error-and-hint", []event{txt("t"), k(tea.KeyEnter), txt("tar"), k(tea.KeyEnter), txt("1500"), k(tea.KeyEnter), upd("1.5.0"), wait(), k(tea.KeyTab), txt("x"), k(tea.KeyEnter), k(tea.KeyTab)}},
	{"update-over-location", []event{txt("loc"), k(tea.KeyEnter), txt("txt"), k(tea.KeyEnter), k(tea.KeyEnter), k(tea.KeyRight), upd("1.5.0"), wait(), k(tea.KeyEsc), k(tea.KeyEnter)}},
	{"update-over-confirm", []event{txt("dup"), k(tea.KeyEnter), txt("txt"), k(tea.KeyEnter), k(tea.KeyEnter), txt("y"), k(tea.KeyEnter), txt("dup"), k(tea.KeyEnter), txt("txt"), k(tea.KeyEnter), k(tea.KeyEnter), txt("y"), upd("1.5.0"), wait(), txt("n"), txt("y")}},
	{"update-over-done", []event{txt("d"), k(tea.KeyEnter), txt("txt"), k(tea.KeyEnter), k(tea.KeyEnter), txt("y"), upd("2.0.0"), wait(), txt("q"), txt("n"), txt("q")}},
	{"update-ctrl-d-ignored-ctrl-c-quits", []event{upd("1.5.0"), wait(), k(tea.KeyCtrlD), k(tea.KeyCtrlC)}},
}

func repeatKey(key tea.KeyType, n int) []event {
	out := make([]event, n)
	for i := range out {
		out[i] = k(key)
	}
	return out
}

// runTranscript plays a flow and records the view state after each event,
// with the random temporary directories replaced by placeholders.
func runTranscript(t *testing.T, events []event) string {
	t.Helper()
	root := t.TempDir()
	dir, home, dl, abs := mkdir(t, filepath.Join(root, "cwd")), mkdir(t, filepath.Join(root, "home")),
		mkdir(t, filepath.Join(root, "dl")), mkdir(t, filepath.Join(root, "abs"))
	mkdir(t, filepath.Join(dir, "sub"))
	h := newHarnessAt(t, &memStore{cfg: model.Config{DownloadDir: dl}}, dir, home)
	clk := &clock{t: time.Unix(1_000_000, 0)}
	h.c.setup.Now, h.c.setup.Version = clk.now, "1.4.1"
	clean := strings.NewReplacer(abs, "@ABS@", dir, "@CWD@", home, "@HOME@", dl, "@DL@", root, "@ROOT@").Replace

	var b strings.Builder
	record := func(label string) {
		st := h.c.State()
		fmt.Fprintf(&b, "-- %s quit=%v\n%s\n", label, h.quit, clean(fmt.Sprintf("%#v", st)))
	}
	record("start")
	for _, e := range events {
		switch {
		case e.text != "":
			h.typ(strings.ReplaceAll(e.text, "@ABS@", abs))
		case e.update != nil:
			h.send(updateMsg{*e.update})
		case e.wait:
			clk.t = clk.t.Add(updateGrace)
		default:
			h.key(e.key)
		}
		record(e.String())
	}
	return b.String()
}

// TestGoldenTranscript pins the controller's state machine: the full view
// state after every key of every flow in goldenFlows.
func TestGoldenTranscript(t *testing.T) {
	var b strings.Builder
	for _, f := range goldenFlows {
		fmt.Fprintf(&b, "==== %s\n%s", f.name, runTranscript(t, f.events))
	}
	got := b.String()
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenTranscriptPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenTranscriptPath)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
		for i := 0; i < min(len(wl), len(gl)); i++ {
			if wl[i] != gl[i] {
				t.Fatalf("transcript differs at golden line %d:\n  want %s\n   got %s", i+1, wl[i], gl[i])
			}
		}
		t.Fatalf("transcript has %d lines, golden %d", len(gl), len(wl))
	}
}
