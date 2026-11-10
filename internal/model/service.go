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
	if err := ValidateName(name); err != nil {
		return "", err
	}
	f, ok := s.Registry.Lookup(ext)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrUnknownFormat, NormalizeExt(ext))
	}
	p := FileName(name, f.Ext)
	if !filepath.IsAbs(p) && dir != "" {
		p = filepath.Join(dir, p)
	}
	return p, nil
}

// Create generates the requested file. The file is written to a temporary
// name first and renamed into place, so failures never leave partial files.
func (s *Service) Create(req Request) (Result, error) {
	path, err := s.TargetPath(req.Dir, req.Name, req.Ext)
	if err != nil {
		return Result{}, err
	}
	f, _ := s.Registry.Lookup(req.Ext)

	// An "eicar" name produces the EICAR test string at its natural 68 bytes,
	// regardless of the format's size rule or any requested size.
	eicar := IsEICARName(req.Name)
	size := f.Rule.Min
	switch {
	case eicar:
		size = int64(len(EICAR))
	case req.Size != nil:
		size = *req.Size
	}
	if !eicar {
		if err := f.Rule.Check(size); err != nil {
			err.(*SizeError).Ext = f.Ext
			return Result{}, err
		}
	}
	if _, err := os.Stat(path); err == nil && !req.Overwrite {
		return Result{}, fmt.Errorf("%w: %s", ErrExists, path)
	}
	dir := filepath.Dir(path)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return Result{}, fmt.Errorf("directory %q does not exist", dir)
	}
	tmp, err := os.CreateTemp(dir, ".bbdx-*.tmp")
	if err != nil {
		return Result{}, err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	write := func(w io.Writer) error { return f.Generate(w, size) }
	if eicar {
		write = func(w io.Writer) error { _, err := io.WriteString(w, EICAR); return err }
	}
	if err := write(tmp); err != nil {
		tmp.Close()
		return Result{}, err
	}
	if err := tmp.Close(); err != nil {
		return Result{}, err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return Result{}, err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		return Result{}, err
	}
	return Result{Path: path, Size: size, Format: f, EICAR: eicar}, nil
}
