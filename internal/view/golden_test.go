package view

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"testing"
)

// update regenerates the golden file: go test ./internal/view -run Golden -update
var update = flag.Bool("update", false, "rewrite golden files instead of comparing against them")

const goldenViewPath = "testdata/golden_view.txt"

// goldenStates covers every step and the optional parts of each screen.
var goldenStates = []struct {
	name string
	s    State
}{
	{"name", State{Step: StepName, Input: "> report", Error: "file name must not be empty"}},
	{"ext", State{Step: StepExt, Name: "report", Input: "> p",
		Suggestions: []Suggestion{
			{Ext: "pdf", Name: "Portable Document Format 1.4", Category: "Documents", MinSize: "595 B"},
			{Ext: "png", Name: "Portable Network Graphics", Category: "Images", MinSize: "70 B", Selected: true},
		}, MoreSuggestions: 3}},
	{"ext-empty", State{Step: StepExt, Name: "report", Input: "> zzz"}},
	{"size", State{Step: StepSize, Name: "report", Ext: "wav", FormatName: "Waveform Audio (PCM)",
		TargetPath: "report.wav", MinSize: "44 B", MinBytes: 44, SizeNote: "RIFF files always have an even number of bytes",
		Error: "a .wav file cannot be exactly 45 bytes", Hint: "tab: use 46 bytes"}},
	{"location", State{Step: StepLocation, TargetPath: "report.pdf", CurrentDir: "/work", DownloadDir: "/home/u/Downloads", LocationYes: true}},
	{"location-no-eicar", State{Step: StepLocation, TargetPath: "eicar.com", CurrentDir: "/work", DownloadDir: "/home/u/Downloads", EICAR: true}},
	{"confirm", State{Step: StepConfirm, TargetPath: "/work/report.pdf"}},
	{"generating", State{Step: StepGenerating, TargetPath: "/work/big.zip", Spinner: "*", SizeLabel: "1 GiB"}},
	{"done", State{Step: StepDone, TargetPath: "/work/report.pdf", FormatName: "Portable Document Format 1.4", SizeLabel: "595 B", Bytes: 595}},
	{"done-eicar", State{Step: StepDone, TargetPath: "/work/eicar.txt", FormatName: "Plain text", SizeLabel: "68 B", Bytes: 68, EICAR: true}},
	{"downloads-first-run", State{Step: StepDownloadDir, Input: "> ~/Downloads",
		DownloadReason: "The configured download directory is not reachable:\n  /gone does not exist"}},
	{"downloads-change", State{Step: StepDownloadDir, Input: "> /data", DownloadCancel: true}},
	{"control-chars", State{Step: StepLocation, TargetPath: "evil\x1b]0;pwned\x07.txt", CurrentDir: "/work\r\x1b[2J",
		DownloadDir: "/dl\x9b31m", Error: "bad\x1b[31m red"}},
	{"c1-and-reason", State{Step: StepDownloadDir, Input: "> x",
		DownloadReason: "first\u009b2J line\n  second \x1b]8;;http://x\x07link"}},
	{"update", State{Step: StepUpdate, UpdateVersion: "1.5.0", InstalledVersion: "1.4.1"}},
	{"update-control-chars", State{Step: StepUpdate, UpdateVersion: "9.9.9\x1b]0;pwned\x07", InstalledVersion: "1.4.1\x1b[2J"}},
}

func renderGolden() string {
	var b strings.Builder
	for _, gs := range goldenStates {
		for _, width := range []int{0, 40} {
			for _, plain := range []bool{false, true} {
				s := gs.s
				s.Width, s.Plain = width, plain
				fmt.Fprintf(&b, "=== %s width=%d plain=%v\n%q\n", gs.name, width, plain, Render(s))
			}
		}
	}
	return b.String()
}

// TestGoldenView pins every rendered screen, ANSI styling included.
func TestGoldenView(t *testing.T) {
	got := renderGolden()
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(goldenViewPath, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(goldenViewPath)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		wl, gl := strings.Split(string(want), "\n"), strings.Split(got, "\n")
		for i := 0; i < min(len(wl), len(gl)); i++ {
			if wl[i] != gl[i] {
				t.Fatalf("view differs at golden line %d:\n  want %s\n   got %s", i+1, wl[i], gl[i])
			}
		}
		t.Fatalf("view output has %d lines, golden %d", len(gl), len(wl))
	}
}
