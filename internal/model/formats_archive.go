package model

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"time"
)

// archiveEpoch is the fixed timestamp used inside archives for reproducible output.
var archiveEpoch = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func archiveFormats() []Format {
	return []Format{
		{
			Ext: "zip", Name: "ZIP archive", MIME: "application/zip", Category: CategoryArchive,
			Rule:  SizeRule{Min: zipEOCDLen},
			Write: writeZIP,
		},
		{
			Ext: "gz", Aliases: []string{"gzip"}, Name: "gzip compressed data", MIME: "application/gzip",
			Category: CategoryArchive,
			Rule:     SizeRule{Min: gzMin},
			Write:    writeGZ,
		},
		{
			Ext: "tar", Name: "POSIX tar archive (ustar)", MIME: "application/x-tar", Category: CategoryArchive,
			Rule: SizeRule{Min: 1024, Align: 512,
				Note: "tar archives consist of 512-byte blocks, so the size must be a multiple of 512"},
			Write: writeTAR,
		},
		{
			Ext: "xz", Name: "XZ compressed data", MIME: "application/x-xz", Category: CategoryArchive,
			Rule: SizeRule{Min: xzEmptyStreamLen, Align: 4,
				Note: "xz streams and stream padding are always a multiple of 4 bytes"},
			Write: writeXZ,
		},
		{
			Ext: "sqlite", Aliases: []string{"sqlite3", "db"}, Name: "SQLite 3 database",
			MIME: "application/vnd.sqlite3", Category: CategoryArchive,
			Rule: SizeRule{Min: sqlitePageSize, Align: sqlitePageSize,
				Note: "SQLite databases consist of whole pages (512 bytes here)"},
			Write: writeSQLite,
		},
	}
}

func loremCRC(n int64) uint32 {
	h := crc32.NewIEEE()
	_ = loremAt(h, 0, n)
	return h.Sum32()
}

// ---------------------------------------------------------------- ZIP

const (
	zipMemberName  = "bbdx.txt"
	zipMemberFixed = zipLocalHeaderLen + len(zipMemberName) + zipCentralHeaderLen + len(zipMemberName) + zipEOCDLen
)

// writeZIP emits an archive with one stored text member whenever it fits;
// smaller sizes are an empty archive whose EOCD comment absorbs the rest.
func writeZIP(w io.Writer, size int64) error {
	b := &binw{w: w}
	if size < int64(zipMemberFixed) {
		pad := size - zipEOCDLen
		b.raw(zipEOCD(0, 0, 0, uint16(pad))...)
		b.do(func(w io.Writer) error { return Lorem(w, pad, false) })
		return b.err
	}
	n := size - int64(zipMemberFixed)
	e := zipEntry{
		name: zipMemberName, crc: loremCRC(n), size: uint32(n),
		madeBy:       3<<8 | 20, // UNIX, spec 2.0
		internalAttr: 1,         // text file
		externalAttr: 0o100644 << 16,
	}
	lh, cd := zipLocalHeader(e), zipCentralHeader(e)
	b.raw(lh...)
	b.do(func(w io.Writer) error { return loremAt(w, 0, n) })
	b.raw(cd...)
	b.raw(zipEOCD(1, uint32(len(cd)), uint32(int64(len(lh))+n), 0)...)
	return b.err
}

// ---------------------------------------------------------------- GZIP

const gzMin = 10 + 2 + 8 // header + empty fixed-Huffman block + trailer

// writeGZ emits one gzip member. The two sizes just above the minimum are
// reached with an FCOMMENT field; everything else uses stored deflate blocks
// carrying lorem text.
func writeGZ(w io.Writer, size int64) error {
	b := &binw{w: w}
	b.raw(0x1f, 0x8b, 8) // magic, deflate
	comment := size == gzMin+1 || size == gzMin+2
	if comment {
		b.raw(0x10) // FCOMMENT
	} else {
		b.raw(0)
	}
	b.u32le(uint32(archiveEpoch.Unix()))
	b.raw(0, 3) // no extra flags, OS: Unix
	if comment {
		b.raw(bytes.Repeat([]byte("x"), int(size-gzMin-1))...)
		b.raw(0)
	}
	if size <= gzMin+2 {
		b.raw(0x03, 0x00) // final empty fixed block
		b.u32le(0)        // CRC-32
		b.u32le(0)        // input size
		return b.err
	}
	deflated := size - 18
	blocks := (deflated + 65539) / 65540
	data := deflated - 5*blocks
	h := crc32.NewIEEE()
	out := io.MultiWriter(w, h)
	var off int64
	for i := int64(0); i < blocks; i++ {
		n := data / blocks
		if i < data%blocks {
			n++
		}
		final := uint8(0)
		if i == blocks-1 {
			final = 1
		}
		b.raw(final) // BFINAL + BTYPE=00 (stored)
		b.u16le(uint16(n))
		b.u16le(^uint16(n))
		b.do(func(io.Writer) error { return loremAt(out, off, n) })
		off += n
	}
	b.u32le(h.Sum32())
	b.u32le(uint32(data))
	return b.err
}

// ---------------------------------------------------------------- TAR

func writeTAR(w io.Writer, size int64) error {
	if size > 1024 {
		data := size - 1536
		hdr := make([]byte, 512)
		copy(hdr[0:], zipMemberName)
		copy(hdr[100:], "0000644\x00")
		copy(hdr[108:], "0000000\x00")
		copy(hdr[116:], "0000000\x00")
		copy(hdr[124:], fmt.Sprintf("%011o\x00", data))
		copy(hdr[136:], fmt.Sprintf("%011o\x00", archiveEpoch.Unix()))
		copy(hdr[148:], "        ")
		hdr[156] = '0'
		copy(hdr[257:], "ustar\x0000")
		copy(hdr[265:], "bbdx")
		copy(hdr[297:], "bbdx")
		sum := 0
		for _, c := range hdr {
			sum += int(c)
		}
		copy(hdr[148:], fmt.Sprintf("%06o\x00 ", sum))
		if _, err := w.Write(hdr); err != nil {
			return err
		}
		if err := loremAt(w, 0, data); err != nil {
			return err
		}
	}
	return Zeros(w, 1024)
}

// ---------------------------------------------------------------- XZ

const (
	xzEmptyStreamLen = 32
	xzChunkMax       = 65536
	xzBlockHeaderLen = 12
)

func vliLen(v uint64) int64 {
	n := int64(1)
	for v >= 0x80 {
		v >>= 7
		n++
	}
	return n
}

func appendVLI(b []byte, v uint64) []byte {
	for v >= 0x80 {
		b = append(b, byte(v)|0x80)
		v >>= 7
	}
	return append(b, byte(v))
}

func roundUp4(n int64) int64 { return (n + 3) &^ 3 }

// xzLayout returns the sizes of a single-block stream holding d bytes.
func xzLayout(d int64) (compressed, unpadded, total int64) {
	chunks := (d + xzChunkMax - 1) / xzChunkMax
	compressed = 3*chunks + d + 1
	unpadded = xzBlockHeaderLen + compressed + 4
	index := roundUp4(2+vliLen(uint64(unpadded))+vliLen(uint64(d))) + 4
	total = 12 + xzBlockHeaderLen + roundUp4(compressed) + 4 + index + 12
	return
}

var xzFlags = []byte{0x00, 0x01} // check type CRC32

func xzStreamHeader() []byte {
	h := append([]byte{0xFD, '7', 'z', 'X', 'Z', 0}, xzFlags...)
	return binary.LittleEndian.AppendUint32(h, crc32.ChecksumIEEE(xzFlags))
}

func xzStreamFooter(indexLen int64) []byte {
	body := binary.LittleEndian.AppendUint32(nil, uint32(indexLen/4-1))
	body = append(body, xzFlags...)
	f := binary.LittleEndian.AppendUint32(nil, crc32.ChecksumIEEE(body))
	return append(append(f, body...), 'Y', 'Z')
}

func xzIndex(records [][2]uint64) []byte {
	idx := []byte{0}
	idx = appendVLI(idx, uint64(len(records)))
	for _, r := range records {
		idx = appendVLI(idx, r[0])
		idx = appendVLI(idx, r[1])
	}
	for len(idx)%4 != 0 {
		idx = append(idx, 0)
	}
	return binary.LittleEndian.AppendUint32(idx, crc32.ChecksumIEEE(idx))
}

// writeXZ emits an .xz stream whose single block stores lorem text in LZMA2
// uncompressed chunks; leftover bytes become standard stream padding.
func writeXZ(w io.Writer, size int64) error {
	_, _, smallest := xzLayout(1)
	if size < smallest {
		idx := xzIndex(nil)
		if err := avWrite(w, xzStreamHeader(), idx, xzStreamFooter(int64(len(idx)))); err != nil {
			return err
		}
		return Zeros(w, size-xzEmptyStreamLen)
	}
	d := max(1, size-60)
	for {
		_, _, total := xzLayout(d)
		if total <= size {
			break
		}
		d -= total - size
	}
	for {
		if _, _, total := xzLayout(d + 1); total > size {
			break
		}
		d++
	}
	compressed, unpadded, total := xzLayout(d)

	bh := []byte{xzBlockHeaderLen/4 - 1, 0x00, 0x21, 0x01, 0x10, 0, 0, 0} // LZMA2, 1 MiB dictionary
	bh = binary.LittleEndian.AppendUint32(bh, crc32.ChecksumIEEE(bh))
	if _, err := w.Write(append(xzStreamHeader(), bh...)); err != nil {
		return err
	}
	h := crc32.NewIEEE()
	out := io.MultiWriter(w, h)
	for off := int64(0); off < d; off += xzChunkMax {
		n := min(int64(xzChunkMax), d-off)
		ctl := byte(0x02)
		if off == 0 {
			ctl = 0x01 // first chunk resets the dictionary
		}
		if _, err := w.Write([]byte{ctl, byte((n - 1) >> 8), byte(n - 1)}); err != nil {
			return err
		}
		if err := loremAt(out, off, n); err != nil {
			return err
		}
	}
	tail := []byte{0x00} // LZMA2 end marker
	tail = append(tail, make([]byte, roundUp4(compressed)-compressed)...)
	tail = binary.LittleEndian.AppendUint32(tail, h.Sum32())
	idx := xzIndex([][2]uint64{{uint64(unpadded), uint64(d)}})
	tail = append(tail, idx...)
	tail = append(tail, xzStreamFooter(int64(len(idx)))...)
	if _, err := w.Write(tail); err != nil {
		return err
	}
	return Zeros(w, size-total)
}

// ---------------------------------------------------------------- SQLite

const (
	sqlitePageSize = 512
	sqliteTableSQL = "CREATE TABLE bbdx(data TEXT)"
	// local payload kept on a table leaf when the rest spills to overflow pages:
	// M = ((U-12)*32/255)-23 with U = 512.
	sqliteMinLocal    = (sqlitePageSize-12)*32/255 - 23
	sqliteOverflowCap = sqlitePageSize - 4
)

func appendSQLiteVarint(b []byte, v uint64) []byte {
	if v > 0x00ffffffffffffff {
		panic("varint too large")
	}
	var tmp []byte
	for {
		tmp = append(tmp, byte(v&0x7f))
		v >>= 7
		if v == 0 {
			break
		}
	}
	for i := len(tmp) - 1; i >= 0; i-- {
		c := tmp[i]
		if i > 0 {
			c |= 0x80
		}
		b = append(b, c)
	}
	return b
}

func sqliteRecordHeader(serials ...uint64) []byte {
	var body []byte
	for _, s := range serials {
		body = appendSQLiteVarint(body, s)
	}
	for h := uint64(1); ; h++ {
		hdr := appendSQLiteVarint(nil, h)
		if uint64(len(hdr)+len(body)) == h {
			return append(hdr, body...)
		}
	}
}

func sqliteTextSerial(n int) uint64 { return uint64(2*n + 13) }

// sqliteLeafPage builds a table b-tree leaf page holding one cell (or none).
// offset is 100 for page 1, which starts with the database header.
func sqliteLeafPage(page []byte, offset int, cell []byte) {
	ncells, start := 0, sqlitePageSize
	if cell != nil {
		ncells, start = 1, sqlitePageSize-len(cell)
		copy(page[start:], cell)
		binary.BigEndian.PutUint16(page[offset+8:], uint16(start))
	}
	page[offset] = 0x0D
	binary.BigEndian.PutUint16(page[offset+3:], uint16(ncells))
	binary.BigEndian.PutUint16(page[offset+5:], uint16(start))
}

// writeSQLite emits a database with 512-byte pages: one page is an empty
// database, two pages add table bbdx, and every further page is an overflow
// page of a single TEXT row in that table.
func writeSQLite(w io.Writer, size int64) error {
	pages := size / sqlitePageSize
	page1 := make([]byte, sqlitePageSize)
	copy(page1, "SQLite format 3\x00")
	binary.BigEndian.PutUint16(page1[16:], sqlitePageSize)
	page1[18], page1[19] = 1, 1 // legacy journal mode
	page1[21], page1[22], page1[23] = 64, 32, 32
	binary.BigEndian.PutUint32(page1[24:], 1) // change counter
	binary.BigEndian.PutUint32(page1[28:], uint32(pages))
	binary.BigEndian.PutUint32(page1[44:], 4) // schema format
	binary.BigEndian.PutUint32(page1[56:], 1) // UTF-8
	binary.BigEndian.PutUint32(page1[92:], 1) // version-valid-for
	binary.BigEndian.PutUint32(page1[96:], 3046000)
	if pages == 1 {
		sqliteLeafPage(page1, 100, nil)
		_, err := w.Write(page1)
		return err
	}
	binary.BigEndian.PutUint32(page1[40:], 1) // schema cookie
	rec := sqliteRecordHeader(sqliteTextSerial(5), sqliteTextSerial(4), sqliteTextSerial(4), 1,
		sqliteTextSerial(len(sqliteTableSQL)))
	rec = append(rec, "tablebbdxbbdx\x02"+sqliteTableSQL...)
	cell := appendSQLiteVarint(nil, uint64(len(rec)))
	cell = appendSQLiteVarint(cell, 1)
	sqliteLeafPage(page1, 100, append(cell, rec...))
	if _, err := w.Write(page1); err != nil {
		return err
	}

	page2 := make([]byte, sqlitePageSize)
	overflow := pages - 2
	if overflow == 0 {
		sqliteLeafPage(page2, 0, nil)
		_, err := w.Write(page2)
		return err
	}
	// Payload of exactly M + k*(U-4) bytes keeps M bytes local and fills k
	// overflow pages completely.
	payload := sqliteMinLocal + overflow*sqliteOverflowCap
	var hdr []byte
	var text int64
	for h := int64(2); ; h++ {
		text = payload - h
		hdr = sqliteRecordHeader(sqliteTextSerial(int(text)))
		if int64(len(hdr)) == h {
			break
		}
	}
	var local bytes.Buffer
	local.Write(hdr)
	_ = loremAt(&local, 0, sqliteMinLocal-int64(len(hdr)))
	cell = appendSQLiteVarint(nil, uint64(payload))
	cell = appendSQLiteVarint(cell, 1)
	cell = append(cell, local.Bytes()...)
	cell = binary.BigEndian.AppendUint32(cell, 3)
	sqliteLeafPage(page2, 0, cell)
	if _, err := w.Write(page2); err != nil {
		return err
	}
	off := sqliteMinLocal - int64(len(hdr))
	for i := int64(0); i < overflow; i++ {
		next := uint32(0)
		if i < overflow-1 {
			next = uint32(i + 4)
		}
		if _, err := w.Write(binary.BigEndian.AppendUint32(nil, next)); err != nil {
			return err
		}
		if err := loremAt(w, off, sqliteOverflowCap); err != nil {
			return err
		}
		off += sqliteOverflowCap
	}
	return nil
}
