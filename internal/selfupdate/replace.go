package selfupdate

import (
	"fmt"
	"os"
	"path/filepath"
)

// BackupPath is where an update on Windows leaves the replaced executable:
// a running .exe cannot be overwritten or deleted, only renamed. The next
// update removes it.
func BackupPath(exe string) string { return exe + ".old" }

// replace puts bin in place of exe, keeping its permissions. The new
// executable is written next to it and renamed over it, so an interrupted
// update leaves the old one intact. With aside (Windows) the old one is
// first moved to BackupPath(exe).
func replace(exe string, bin []byte, aside bool) error {
	dir := filepath.Dir(exe)
	mode := os.FileMode(0o755)
	if st, err := os.Stat(exe); err == nil {
		mode = st.Mode().Perm() | 0o100
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(exe)+"-update-*.tmp")
	if err != nil {
		return fmt.Errorf("cannot write to %s: %w", dir, err)
	}
	defer os.Remove(tmp.Name()) // no-op once renamed
	if _, err := tmp.Write(bin); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), mode); err != nil {
		return err
	}
	if !aside {
		return os.Rename(tmp.Name(), exe)
	}
	old := BackupPath(exe)
	_ = os.Remove(old)
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), exe); err != nil {
		_ = os.Rename(old, exe) // put the old version back
		return err
	}
	return nil
}
