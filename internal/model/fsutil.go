package model

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
)

// Exists reports whether anything (a file, directory or symlink, even a
// dangling one) occupies path. It does not follow a final symlink, so a link
// is never replaced unnoticed because its target is missing.
func Exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// writeFileAtomic writes a new file at path through a temporary file in the
// same directory (named after pattern, see os.CreateTemp) that is renamed into
// place, so readers never see a partial file and a failure leaves nothing
// behind. With noClobber an existing path is never replaced, even if it
// appears while the file is being written; the error then wraps ErrExists.
func writeFileAtomic(path, pattern string, perm fs.FileMode, noClobber bool, write func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), pattern)
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // no-op once the file has been renamed
	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), perm); err != nil {
		return err
	}
	if noClobber {
		return placeNoClobber(tmp.Name(), path)
	}
	return os.Rename(tmp.Name(), path)
}

// placeNoClobber moves tmp to path unless path exists. A hard link fails
// atomically when the target exists; file systems without hard links fall
// back to a check followed by a rename.
func placeNoClobber(tmp, path string) error {
	err := os.Link(tmp, path)
	switch {
	case err == nil:
		return nil // the deferred removal of tmp drops the extra name
	case errors.Is(err, fs.ErrExist):
		return fmt.Errorf("%w: %s", ErrExists, path)
	}
	if Exists(path) {
		return fmt.Errorf("%w: %s", ErrExists, path)
	}
	return os.Rename(tmp, path)
}
