package selfupdate

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
)

// extract returns the content of the regular file named bin in a .zip or
// .tar.gz archive, wherever in the archive it is.
func extract(archive []byte, isZip bool, bin string) ([]byte, error) {
	if isZip {
		return extractZip(archive, bin)
	}
	return extractTarGz(archive, bin)
}

func extractZip(archive []byte, bin string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
	if err != nil {
		return nil, err
	}
	for _, f := range zr.File {
		if path.Base(f.Name) != bin || !f.Mode().IsRegular() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		defer rc.Close()
		return readLimited(rc, maxBinarySize)
	}
	return nil, errNotFound(bin)
}

func extractTarGz(archive []byte, bin string) ([]byte, error) {
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, errNotFound(bin)
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && path.Base(h.Name) == bin {
			return readLimited(tr, maxBinarySize)
		}
	}
}

func errNotFound(bin string) error { return fmt.Errorf("no %s in the archive", bin) }
