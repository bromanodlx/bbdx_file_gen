package model

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
)

// Category groups formats in the user interface.
type Category string

const (
	CategoryDocument Category = "Documents"
	CategoryText     Category = "Text & data"
	CategoryWeb      Category = "Web"
	CategoryCode     Category = "Source code"
	CategoryImage    Category = "Images"
	CategoryAudio    Category = "Audio"
	CategoryVideo    Category = "Video"
	CategoryArchive  Category = "Archives & databases"
)

// Format knows how to produce standard-compliant files of one file type.
type Format struct {
	Ext      string // canonical extension without dot, lower case
	Aliases  []string
	Name     string
	MIME     string
	Category Category
	Rule     SizeRule
	// Write emits exactly size bytes; size has already been checked against Rule.
	Write func(w io.Writer, size int64) error
}

// Generate writes a file of exactly size bytes or returns a *SizeError.
func (f Format) Generate(w io.Writer, size int64) error {
	if err := f.Rule.Check(size); err != nil {
		err.(*SizeError).Ext = f.Ext
		return err
	}
	bw := bufio.NewWriterSize(w, 64<<10)
	cw := &CountingWriter{W: bw}
	if err := f.Write(cw, size); err != nil {
		return fmt.Errorf("generating %s: %w", f.Ext, err)
	}
	if err := bw.Flush(); err != nil {
		return err
	}
	if cw.N != size {
		return fmt.Errorf("internal error: %s generator wrote %d bytes instead of %d", f.Ext, cw.N, size)
	}
	return nil
}

// Registry is the set of known formats, addressable by extension or alias.
type Registry struct {
	formats []Format
	byExt   map[string]int
}

// NormalizeExt turns " .JPG " into "jpg".
func NormalizeExt(ext string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}

// NewRegistry builds a registry, rejecting duplicate extensions or aliases.
func NewRegistry(formats ...Format) (*Registry, error) {
	r := &Registry{byExt: map[string]int{}}
	sorted := append([]Format(nil), formats...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Ext < sorted[j].Ext })
	for i, f := range sorted {
		for _, key := range append([]string{f.Ext}, f.Aliases...) {
			key = NormalizeExt(key)
			if _, dup := r.byExt[key]; dup {
				return nil, fmt.Errorf("duplicate extension %q", key)
			}
			r.byExt[key] = i
		}
	}
	r.formats = sorted
	return r, nil
}

// Lookup finds a format by extension or alias (case and leading dot ignored).
func (r *Registry) Lookup(ext string) (Format, bool) {
	i, ok := r.byExt[NormalizeExt(ext)]
	if !ok {
		return Format{}, false
	}
	return r.formats[i], true
}

// All returns every format sorted by extension.
func (r *Registry) All() []Format { return append([]Format(nil), r.formats...) }

// Search returns formats whose extension or alias starts with prefix.
func (r *Registry) Search(prefix string) []Format {
	p := NormalizeExt(prefix)
	var out []Format
	for _, f := range r.formats {
		for _, key := range append([]string{f.Ext}, f.Aliases...) {
			if strings.HasPrefix(key, p) {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

var (
	defaultOnce sync.Once
	defaultReg  *Registry
)

// Default returns the registry of all built-in formats.
func Default() *Registry {
	defaultOnce.Do(func() {
		var all []Format
		for _, family := range [][]Format{
			textFormats(), imageFormats(), audioFormats(), videoFormats(),
			archiveFormats(), documentFormats(),
		} {
			all = append(all, family...)
		}
		reg, err := NewRegistry(all...)
		if err != nil {
			panic(err)
		}
		defaultReg = reg
	})
	return defaultReg
}
