package model

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// update regenerates the golden files: go test ./internal/... -run Golden -update
var update = flag.Bool("update", false, "rewrite golden files instead of comparing against them")

const goldenFormatsPath = "testdata/golden_formats.txt"

// goldenSizes are the sizes whose exact bytes are pinned for f: every valid
// size in the first 300 bytes above the minimum, and the valid sizes nearest
// to the boundaries where generators switch layout (chunk, block, segment and
// sub-block limits).
func goldenSizes(f Format) []int64 {
	seen := map[int64]bool{}
	var sizes []int64
	add := func(s int64) {
		if s >= 0 && !seen[s] && f.Rule.Check(s) == nil {
			seen[s] = true
			sizes = append(sizes, s)
		}
	}
	for s := f.Rule.Min; s <= f.Rule.Min+300; s++ {
		add(s)
	}
	for _, b := range []int64{1024, 4096, 32 << 10, 65535, 65536, 65537, 65540, 2*65540 + 5, 70001, 131072 + 517, 1<<20 + 7} {
		add(f.Rule.below(b))
		add(b)
		add(f.Rule.above(b))
	}
	sort.Slice(sizes, func(i, j int) bool { return sizes[i] < sizes[j] })
	return sizes
}

// goldenFormatHashes returns one "ext count sha256" line per built-in format.
// The digest covers the size and the full content of every file in
// goldenSizes, so a single changed byte in any of them changes the line.
func goldenFormatHashes(t *testing.T) []string {
	var lines []string
	for _, f := range Default().All() {
		digest := sha256.New()
		sizes := goldenSizes(f)
		for _, size := range sizes {
			h := sha256.New()
			if err := f.Generate(h, size); err != nil {
				t.Fatalf("%s/%d: %v", f.Ext, size, err)
			}
			fmt.Fprintf(digest, "%d:%x\n", size, h.Sum(nil))
		}
		lines = append(lines, fmt.Sprintf("%s %d %s", f.Ext, len(sizes), hex.EncodeToString(digest.Sum(nil))))
	}
	return lines
}

// TestGoldenFormatBytes pins the exact output of every generator, so that
// refactoring cannot change a single byte unnoticed.
func TestGoldenFormatBytes(t *testing.T) {
	got := goldenFormatHashes(t)
	if *update {
		writeGolden(t, goldenFormatsPath, got)
		return
	}
	want := readGolden(t, goldenFormatsPath)
	compareGolden(t, want, got)
}

func writeGolden(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("wrote %d lines to %s", len(lines), path)
}

func readGolden(t *testing.T, path string) []string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	defer f.Close()
	var lines []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return lines
}

// compareGolden reports the first few differing lines.
func compareGolden(t *testing.T, want, got []string) {
	t.Helper()
	if len(want) != len(got) {
		t.Errorf("golden has %d entries, generated %d", len(want), len(got))
	}
	diffs := 0
	for i := 0; i < min(len(want), len(got)); i++ {
		if want[i] != got[i] {
			t.Errorf("entry %d differs:\n  want %s\n   got %s", i, want[i], got[i])
			if diffs++; diffs >= 10 {
				t.Fatal("too many differences")
			}
		}
	}
}
