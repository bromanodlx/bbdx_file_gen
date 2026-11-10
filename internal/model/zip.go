package model

import "encoding/binary"

// ZIP records (APPNOTE.TXT §4.3) shared by the zip archive and the zip based
// document formats. Every member is stored uncompressed, so record sizes do
// not depend on the content.

const (
	zipLocalHeaderLen   = 30
	zipCentralHeaderLen = 46
	zipEOCDLen          = 22
	// zipDosDate is archiveEpoch (2026-01-01) in MS-DOS date format; the time is 00:00:00.
	zipDosDate = (2026-1980)<<9 | 1<<5 | 1
	zipDosTime = 0
	// zipVersionStored is "version needed to extract" for stored members (1.0).
	zipVersionStored = 10
)

// zipEntry describes one stored member.
type zipEntry struct {
	name      string
	crc, size uint32
	offset    uint32 // of the local header
	madeBy    uint16 // high byte: host system, low byte: spec version
	// Attributes for the central directory: internal bit 0 = text file,
	// external high 16 bits = Unix mode when madeBy says Unix.
	internalAttr uint16
	externalAttr uint32
}

// zipLocalHeader returns the local file header followed by the name.
func zipLocalHeader(e zipEntry) []byte {
	h := make([]byte, zipLocalHeaderLen, zipLocalHeaderLen+len(e.name))
	binary.LittleEndian.PutUint32(h[0:], 0x04034b50)
	binary.LittleEndian.PutUint16(h[4:], zipVersionStored)
	// flags (6) and method (8) stay 0: no flags, stored
	binary.LittleEndian.PutUint16(h[10:], zipDosTime)
	binary.LittleEndian.PutUint16(h[12:], zipDosDate)
	binary.LittleEndian.PutUint32(h[14:], e.crc)
	binary.LittleEndian.PutUint32(h[18:], e.size) // compressed
	binary.LittleEndian.PutUint32(h[22:], e.size) // uncompressed
	binary.LittleEndian.PutUint16(h[26:], uint16(len(e.name)))
	return append(h, e.name...)
}

// zipCentralHeader returns the central directory header followed by the name.
func zipCentralHeader(e zipEntry) []byte {
	h := make([]byte, zipCentralHeaderLen, zipCentralHeaderLen+len(e.name))
	binary.LittleEndian.PutUint32(h[0:], 0x02014b50)
	binary.LittleEndian.PutUint16(h[4:], e.madeBy)
	binary.LittleEndian.PutUint16(h[6:], zipVersionStored)
	binary.LittleEndian.PutUint16(h[12:], zipDosTime)
	binary.LittleEndian.PutUint16(h[14:], zipDosDate)
	binary.LittleEndian.PutUint32(h[16:], e.crc)
	binary.LittleEndian.PutUint32(h[20:], e.size)
	binary.LittleEndian.PutUint32(h[24:], e.size)
	binary.LittleEndian.PutUint16(h[28:], uint16(len(e.name)))
	binary.LittleEndian.PutUint16(h[36:], e.internalAttr)
	binary.LittleEndian.PutUint32(h[38:], e.externalAttr)
	binary.LittleEndian.PutUint32(h[42:], e.offset)
	return append(h, e.name...)
}

// zipEOCD returns the end of central directory record; the comment of
// commentLen bytes is written by the caller after it.
func zipEOCD(entries uint16, cdSize, cdOffset uint32, commentLen uint16) []byte {
	h := make([]byte, zipEOCDLen)
	binary.LittleEndian.PutUint32(h[0:], 0x06054b50)
	binary.LittleEndian.PutUint16(h[8:], entries)
	binary.LittleEndian.PutUint16(h[10:], entries)
	binary.LittleEndian.PutUint32(h[12:], cdSize)
	binary.LittleEndian.PutUint32(h[16:], cdOffset)
	binary.LittleEndian.PutUint16(h[20:], commentLen)
	return h
}
