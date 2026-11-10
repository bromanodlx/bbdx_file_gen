package model

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// validator checks that data is a structurally valid file of one format.
type validator func(t *testing.T, data []byte)

// validators is filled by the per-family test files via registerValidator.
var validators = map[string]validator{}

func registerValidator(ext string, v validator) { validators[ext] = v }

const expectedFormatCount = 50

func TestDefaultRegistryHasFiftyFormats(t *testing.T) {
	all := Default().All()
	if len(all) != expectedFormatCount {
		t.Errorf("default registry has %d formats, want %d", len(all), expectedFormatCount)
	}
	for _, f := range all {
		if f.Name == "" || f.MIME == "" || f.Category == "" || f.Write == nil {
			t.Errorf("%s: incomplete metadata %+v", f.Ext, f)
		}
	}
}

func TestEveryFormatHasValidator(t *testing.T) {
	for _, f := range Default().All() {
		if _, ok := validators[f.Ext]; !ok {
			t.Errorf("%s: no validator registered", f.Ext)
		}
	}
}

// sweepSizes returns the sizes every format is exercised with.
func sweepSizes(f Format, short bool) []int64 {
	min := f.Rule.Min
	var sizes []int64
	n := int64(160)
	if short {
		n = 24
	}
	for s := min; s <= min+n; s++ {
		sizes = append(sizes, s)
	}
	sizes = append(sizes, min+511, min+512, min+513, 4096, 65535, 65536, 65537, 70001, 131072+517)
	if !short {
		sizes = append(sizes, 1<<20, 3<<20+7)
	}
	return sizes
}

func TestEveryFormatProducesExactSizes(t *testing.T) {
	for _, f := range Default().All() {
		f := f
		t.Run(f.Ext, func(t *testing.T) {
			t.Parallel()
			validate := validators[f.Ext]
			for _, size := range sweepSizes(f, testing.Short()) {
				var buf bytes.Buffer
				err := f.Generate(&buf, size)
				if ruleErr := f.Rule.Check(size); ruleErr != nil {
					var se *SizeError
					if !errors.As(err, &se) {
						t.Fatalf("size %d violates the rule (%v) but Generate returned %v", size, ruleErr, err)
					}
					continue
				}
				if err != nil {
					t.Fatalf("size %d: %v", size, err)
				}
				if int64(buf.Len()) != size {
					t.Fatalf("size %d: generated %d bytes", size, buf.Len())
				}
				if validate != nil {
					validate(t, buf.Bytes())
					if t.Failed() {
						t.Fatalf("size %d failed validation", size)
					}
				}
			}
		})
	}
}

func TestMinimumSizeIsAlwaysValid(t *testing.T) {
	for _, f := range Default().All() {
		if err := f.Rule.Check(f.Rule.Min); err != nil {
			t.Errorf("%s: minimum %d rejected by its own rule: %v", f.Ext, f.Rule.Min, err)
		}
	}
}

func TestGenerationIsDeterministic(t *testing.T) {
	for _, f := range Default().All() {
		size := f.Rule.Min
		if err := f.Rule.Check(size + 1024); err == nil {
			size += 1024
		}
		var a, b bytes.Buffer
		if err := f.Generate(&a, size); err != nil {
			t.Fatalf("%s: %v", f.Ext, err)
		}
		_ = f.Generate(&b, size)
		if !bytes.Equal(a.Bytes(), b.Bytes()) {
			t.Errorf("%s: output differs between runs", f.Ext)
		}
	}
}

// --- helpers for validators that use external tools ---

// generate returns a file of the given format and size or fails the test.
func generate(t *testing.T, ext string, size int64) []byte {
	t.Helper()
	f, ok := Default().Lookup(ext)
	if !ok {
		t.Fatalf("unknown format %s", ext)
	}
	var buf bytes.Buffer
	if err := f.Generate(&buf, size); err != nil {
		t.Fatalf("%s/%d: %v", ext, size, err)
	}
	return buf.Bytes()
}

// writeTemp stores data in a temporary file with the given extension.
func writeTemp(t *testing.T, ext string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sample."+ext)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// runTool runs an external validator; the test is skipped when the tool is missing.
func runTool(t *testing.T, name string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed", name)
	}
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %v failed: %v\n%s", name, args, err, out)
	}
	return string(out)
}
