package model

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"io"
	"strings"
	"testing"
)

func init() {
	registerValidator("zip", validateZIP)
	registerValidator("gz", validateGZ)
	registerValidator("tar", validateTAR)
	registerValidator("xz", validateXZ)
	registerValidator("sqlite", validateSQLite)
}

func validateZIP(t *testing.T, data []byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Errorf("zip: %v", err)
		return
	}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Errorf("zip member %s: %v", f.Name, err)
			return
		}
		if _, err := io.Copy(io.Discard, rc); err != nil {
			t.Errorf("zip member %s: %v", f.Name, err)
		}
		rc.Close()
	}
}

func validateGZ(t *testing.T, data []byte) {
	t.Helper()
	br := bytes.NewReader(data)
	zr, err := gzip.NewReader(br)
	if err != nil {
		t.Errorf("gzip: %v", err)
		return
	}
	zr.Multistream(false)
	if _, err := io.Copy(io.Discard, zr); err != nil {
		t.Errorf("gzip: %v", err)
	}
	if br.Len() != 0 {
		t.Errorf("gzip: %d trailing bytes after member", br.Len())
	}
}

func validateTAR(t *testing.T, data []byte) {
	t.Helper()
	if len(data)%512 != 0 {
		t.Errorf("tar: size %d not a multiple of 512", len(data))
	}
	if !bytes.Equal(data[len(data)-1024:], make([]byte, 1024)) {
		t.Error("tar: missing end-of-archive blocks")
	}
	tr := tar.NewReader(bytes.NewReader(data))
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Errorf("tar: %v", err)
			return
		}
		n, err := io.Copy(io.Discard, tr)
		if err != nil || n != h.Size {
			t.Errorf("tar member %s: read %d/%d: %v", h.Name, n, h.Size, err)
		}
	}
}

// validateXZ parses the .xz container completely: stream header and footer,
// block headers, LZMA2 uncompressed chunks, CRC32 checks, index and padding.
func validateXZ(t *testing.T, data []byte) {
	t.Helper()
	if err := checkXZ(data); err != nil {
		t.Errorf("xz: %v", err)
	}
}

func readVLI(b []byte) (uint64, int, error) {
	var v uint64
	for i := 0; i < len(b) && i < 9; i++ {
		v |= uint64(b[i]&0x7f) << (7 * i)
		if b[i]&0x80 == 0 {
			return v, i + 1, nil
		}
	}
	return 0, 0, errors.New("bad VLI")
}

func checkXZ(data []byte) error {
	if len(data)%4 != 0 || len(data) < 32 {
		return errors.New("size must be >= 32 and a multiple of 4")
	}
	if !bytes.Equal(data[:6], []byte{0xFD, '7', 'z', 'X', 'Z', 0}) {
		return errors.New("bad stream header magic")
	}
	flags := data[6:8]
	if crc32.ChecksumIEEE(flags) != binary.LittleEndian.Uint32(data[8:12]) {
		return errors.New("bad stream header CRC")
	}
	if flags[0] != 0 || flags[1] != 0x01 {
		return errors.New("expected CRC32 check type")
	}
	// strip stream padding
	end := len(data)
	for end >= 4 && bytes.Equal(data[end-4:end], []byte{0, 0, 0, 0}) {
		end -= 4
	}
	footer := data[end-12 : end]
	if !bytes.Equal(footer[10:], []byte("YZ")) {
		return errors.New("bad footer magic")
	}
	if crc32.ChecksumIEEE(footer[4:10]) != binary.LittleEndian.Uint32(footer[:4]) {
		return errors.New("bad footer CRC")
	}
	if !bytes.Equal(footer[8:10], flags) {
		return errors.New("footer flags differ from header")
	}
	indexSize := (int(binary.LittleEndian.Uint32(footer[4:8])) + 1) * 4
	idxStart := end - 12 - indexSize
	if idxStart < 12 {
		return errors.New("bad backward size")
	}
	idx := data[idxStart : end-12]
	if crc32.ChecksumIEEE(idx[:len(idx)-4]) != binary.LittleEndian.Uint32(idx[len(idx)-4:]) {
		return errors.New("bad index CRC")
	}
	if idx[0] != 0 {
		return errors.New("bad index indicator")
	}
	p := 1
	count, n, err := readVLI(idx[p:])
	if err != nil {
		return err
	}
	p += n
	pos := 12
	for i := uint64(0); i < count; i++ {
		unpadded, n, err := readVLI(idx[p:])
		if err != nil {
			return err
		}
		p += n
		usize, n, err := readVLI(idx[p:])
		if err != nil {
			return err
		}
		p += n
		hsize := (int(data[pos]) + 1) * 4
		hdr := data[pos : pos+hsize]
		if crc32.ChecksumIEEE(hdr[:hsize-4]) != binary.LittleEndian.Uint32(hdr[hsize-4:]) {
			return errors.New("bad block header CRC")
		}
		q := pos + hsize
		var out []byte
		for first := true; ; first = false {
			ctl := data[q]
			q++
			if ctl == 0 {
				break
			}
			if ctl != 1 && ctl != 2 || (first && ctl != 1) {
				return errors.New("unexpected LZMA2 control byte")
			}
			sz := int(binary.BigEndian.Uint16(data[q:])) + 1
			q += 2
			out = append(out, data[q:q+sz]...)
			q += sz
		}
		if uint64(q-pos+4) != unpadded {
			return errors.New("unpadded size mismatch")
		}
		if uint64(len(out)) != usize {
			return errors.New("uncompressed size mismatch")
		}
		for q%4 != 0 {
			if data[q] != 0 {
				return errors.New("non-zero block padding")
			}
			q++
		}
		if crc32.ChecksumIEEE(out) != binary.LittleEndian.Uint32(data[q:]) {
			return errors.New("bad block check")
		}
		pos = q + 4
	}
	if pos != idxStart {
		return errors.New("blocks do not end at index")
	}
	for p%4 != 0 {
		if idx[p] != 0 {
			return errors.New("non-zero index padding")
		}
		p++
	}
	if p != len(idx)-4 {
		return errors.New("index length mismatch")
	}
	return nil
}

func validateSQLite(t *testing.T, data []byte) {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("SQLite format 3\x00")) {
		t.Error("sqlite: bad magic")
		return
	}
	pageSize := int(binary.BigEndian.Uint16(data[16:18]))
	if pageSize != 512 || len(data)%pageSize != 0 {
		t.Errorf("sqlite: page size %d, file size %d", pageSize, len(data))
		return
	}
	if got := int(binary.BigEndian.Uint32(data[28:32])); got != len(data)/pageSize {
		t.Errorf("sqlite: header page count %d, file has %d pages", got, len(data)/pageSize)
	}
	if binary.BigEndian.Uint32(data[24:28]) != binary.BigEndian.Uint32(data[92:96]) {
		t.Error("sqlite: version-valid-for differs from change counter")
	}
	if binary.BigEndian.Uint32(data[32:36]) != 0 || binary.BigEndian.Uint32(data[36:40]) != 0 {
		t.Error("sqlite: unexpected freelist")
	}
	if data[100] != 0x0D {
		t.Errorf("sqlite: page 1 type %#x, want table leaf", data[100])
	}
}

// --- external tools ---

func TestZIPWithUnzipAnd7z(t *testing.T) {
	for _, size := range []int64{22, 100, 22 + 65535, 22 + 65536, 200_000} {
		p := writeTemp(t, "zip", generate(t, "zip", size))
		runTool(t, "7z", "t", p)
		if size >= int64(zipMemberFixed) { // unzip reports empty archives with exit code 1
			out := runTool(t, "unzip", "-t", p)
			if !strings.Contains(out, "No errors detected") {
				t.Errorf("unzip: %s", out)
			}
		}
	}
}

func TestGZWithGzipAnd7z(t *testing.T) {
	for _, size := range []int64{20, 21, 22, 23, 24, 100, 70_000, 300_000} {
		p := writeTemp(t, "gz", generate(t, "gz", size))
		runTool(t, "gzip", "-t", p)
		runTool(t, "7z", "t", p)
	}
}

func TestTARWithTarAnd7z(t *testing.T) {
	for _, size := range []int64{1024, 1536, 2048, 10240, 1 << 20} {
		p := writeTemp(t, "tar", generate(t, "tar", size))
		runTool(t, "tar", "-tvf", p)
		if size > 1024 { // 7z refuses archives without members
			runTool(t, "7z", "t", p)
		}
	}
}

func TestXZWithXzAnd7z(t *testing.T) {
	for _, size := range []int64{32, 36, 52, 56, 60, 1000, 65536 + 100, 200_000} {
		p := writeTemp(t, "xz", generate(t, "xz", size))
		runTool(t, "xz", "-t", p)
		if size >= 56 {
			runTool(t, "7z", "t", p)
		}
	}
}

func TestSQLiteIntegrity(t *testing.T) {
	script := `import sqlite3,sys
c=sqlite3.connect(sys.argv[1])
print(c.execute('pragma integrity_check').fetchone()[0])
print(c.execute("select count(*) from sqlite_schema").fetchone()[0])
try:
    print(c.execute('select length(data) from bbdx').fetchall())
except sqlite3.OperationalError:
    print('no table')`
	for _, size := range []int64{512, 1024, 1536, 2048, 512 * 300} {
		p := writeTemp(t, "sqlite", generate(t, "sqlite", size))
		out := runTool(t, "python3", "-c", script, p)
		if !strings.HasPrefix(out, "ok\n") {
			t.Errorf("size %d: integrity_check: %s", size, out)
		}
	}
}

func TestArchiveAliases(t *testing.T) {
	for alias, ext := range map[string]string{"gzip": "gz", "sqlite3": "sqlite", "db": "sqlite"} {
		if f, ok := Default().Lookup(alias); !ok || f.Ext != ext {
			t.Errorf("alias %s -> %v", alias, f.Ext)
		}
	}
}

func TestZIPContainsMemberAsSoonAsItFits(t *testing.T) {
	for _, size := range []int64{int64(zipMemberFixed), int64(zipMemberFixed) + 1, 10000, 65557} {
		zr, err := zip.NewReader(bytes.NewReader(generate(t, "zip", size)), size)
		if err != nil {
			t.Fatal(err)
		}
		if len(zr.File) != 1 {
			t.Errorf("size %d: %d members, want 1", size, len(zr.File))
		}
	}
}
