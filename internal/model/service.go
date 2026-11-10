package model

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

var (
	ErrUnknownFormat = errors.New("unknown file format")
	ErrExists        = errors.New("file already exists")
)

// Request describes one file to generate.
type Request struct {
	// Dir is the base directory for relative names ("" = working directory).
	Dir  string
	Name string
	Ext  string
	// Size is the exact size in bytes; nil means the format's minimum.
	Size      *int64
	Overwrite bool
}

// Result describes a generated file.
type Result struct {
	Path   string
	Size   int64
	Format Format
	// EICAR is true when the file was filled with the EICAR test string.
	EICAR bool
}

// Service creates files on disk.
type Service struct {
	Registry *Registry
}

// ValidateName checks a user supplied file name (which may contain directories).
func ValidateName(name string) error {
	n := strings.TrimSpace(name)
	switch {
	case n == "":
		return errors.New("file name must not be empty")
	case strings.ContainsRune(n, 0):
		return errors.New("file name must not contain NUL bytes")
	case strings.HasSuffix(n, "/") || strings.HasSuffix(n, string(filepath.Separator)):
		return errors.New("file name must not end with a path separator")
	}
	if base := filepath.Base(n); base == "." || base == ".." {
		return fmt.Errorf("%q is not a valid file name", n)
	}
	return nil
}

// FileName appends ".ext" unless name already ends with it.
func FileName(name, ext string) string {
	n := strings.TrimSpace(name)
	if strings.HasSuffix(strings.ToLower(n), "."+ext) {
		return n
	}
	return n + "." + ext
}

// TargetPath resolves the final path for name and ext; relative names are
// placed in dir.
func (s *Service) TargetPath(dir, name, ext string) (string, error) {
	_, path, err := s.resolve(dir, name, ext)
	return path, err
}

// resolve validates name, looks up the format for ext and builds the path.
func (s *Service) resolve(dir, name, ext string) (Format, string, error) {
	if err := ValidateName(name); err != nil {
		return Format{}, "", err
	}
	f, ok := s.Registry.Lookup(ext)
	if !ok {
		return Format{}, "", fmt.Errorf("%w: %q", ErrUnknownFormat, NormalizeExt(ext))
	}
	p := FileName(name, f.Ext)
	if !filepath.IsAbs(p) && dir != "" {
		p = filepath.Join(dir, p)
	}
	return f, p, nil
}

// payload is what Create writes: size bytes produced by write.
type payload struct {
	size  int64
	eicar bool
	write func(io.Writer) error
}

// plan decides the content of the requested file. An "eicar" name produces
// the EICAR test string at its natural 68 bytes, regardless of the format's
// size rule or any requested size; otherwise the format is generated at the
// requested size, or at its minimum when none is given.
func plan(f Format, req Request) (payload, error) {
	if IsEICARName(req.Name) {
		return payload{size: int64(len(EICAR)), eicar: true, write: func(w io.Writer) error {
			_, err := io.WriteString(w, EICAR)
			return err
		}}, nil
	}
	size := f.Rule.Min
	if req.Size != nil {
		size = *req.Size
	}
	if err := f.CheckSize(size); err != nil {
		return payload{}, err
	}
	return payload{size: size, write: func(w io.Writer) error { return f.Generate(w, size) }}, nil
}

// Create generates the requested file. The file is written to a temporary
// name first and renamed into place, so failures never leave partial files.
// Unless req.Overwrite is set, an existing file is never replaced.
func (s *Service) Create(req Request) (Result, error) {
	f, path, err := s.resolve(req.Dir, req.Name, req.Ext)
	if err != nil {
		return Result{}, err
	}
	p, err := plan(f, req)
	if err != nil {
		return Result{}, err
	}
	// Checked up front so a large file is not generated only to be discarded;
	// writeFileAtomic enforces it again when the file is put in place.
	if !req.Overwrite && Exists(path) {
		return Result{}, fmt.Errorf("%w: %s", ErrExists, path)
	}
	dir := filepath.Dir(path)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return Result{}, fmt.Errorf("directory %q does not exist", dir)
	}
	if err := writeFileAtomic(path, ".bbdx-*.tmp", 0o644, !req.Overwrite, p.write); err != nil {
		return Result{}, err
	}
	return Result{Path: path, Size: p.size, Format: f, EICAR: p.eicar}, nil
}
