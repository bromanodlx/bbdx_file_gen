package model

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
)

func init() {
	registerValidator("avi", validateAVI)
	registerValidator("mp4", func(t *testing.T, d []byte) { validateISOBMFF(t, d, "isom") })
	registerValidator("mov", func(t *testing.T, d []byte) { validateISOBMFF(t, d, "qt  ") })
	registerValidator("mkv", func(t *testing.T, d []byte) { validateEBML(t, d, "matroska") })
	registerValidator("webm", func(t *testing.T, d []byte) { validateEBML(t, d, "webm") })
}

// --- AVI ---

func validateAVI(t *testing.T, data []byte) {
	cs := avtParseRIFF(t, data, "AVI ")
	hdrl, movi, idx := avtFind(cs, "hdrl"), avtFind(cs, "movi"), avtFind(cs, "idx1")
	if hdrl == nil || movi == nil || idx == nil {
		t.Fatalf("AVI needs hdrl, movi and idx1")
	}
	avih, strl := avtFind(hdrl.kids, "avih"), avtFind(hdrl.kids, "strl")
	if avih == nil || len(avih.data) != 56 || strl == nil {
		t.Fatalf("hdrl needs a 56-byte avih and a strl list")
	}
	strh, strf := avtFind(strl.kids, "strh"), avtFind(strl.kids, "strf")
	if strh == nil || string(strh.data[:4]) != "vids" || strf == nil || len(strf.data) != 40 {
		t.Fatalf("strl needs a vids strh and a BITMAPINFOHEADER strf")
	}
	frames := int(binary.LittleEndian.Uint32(avih.data[16:]))
	if len(idx.data) != 16*frames {
		t.Fatalf("idx1 has %d bytes for %d frames", len(idx.data), frames)
	}
	// idx1 offsets are relative to the 'movi' list type field.
	off := binary.LittleEndian.Uint32(idx.data[8:])
	size := binary.LittleEndian.Uint32(idx.data[12:])
	moviData := movi.data // starts with "movi"
	if int(off)+8+int(size) > len(moviData) || string(moviData[off:off+4]) != "00db" {
		t.Fatalf("idx1 entry does not point at the 00db frame")
	}
	if binary.LittleEndian.Uint32(moviData[off+4:]) != size {
		t.Fatalf("idx1 size mismatch")
	}
}

// --- ISO base media (MP4 / QuickTime) ---

type avtBox struct {
	typ  string
	off  int // absolute offset of the payload
	data []byte
	kids []avtBox
}

var avtContainers = map[string]bool{"moov": true, "trak": true, "mdia": true, "minf": true, "dinf": true, "stbl": true}

func avtParseBoxes(t *testing.T, b []byte, base int) []avtBox {
	t.Helper()
	var out []avtBox
	pos := 0
	for pos < len(b) {
		if len(b)-pos < 8 {
			t.Fatalf("truncated box header at %d", base+pos)
		}
		size := int(binary.BigEndian.Uint32(b[pos:]))
		typ := string(b[pos+4 : pos+8])
		hdr := 8
		if size == 1 {
			size = int(binary.BigEndian.Uint64(b[pos+8:]))
			hdr = 16
		} else if size == 0 {
			size = len(b) - pos
		}
		if size < hdr || pos+size > len(b) {
			t.Fatalf("box %q at %d has bad size %d", typ, base+pos, size)
		}
		bx := avtBox{typ: typ, off: base + pos + hdr, data: b[pos+hdr : pos+size]}
		if avtContainers[typ] {
			bx.kids = avtParseBoxes(t, bx.data, bx.off)
		}
		out = append(out, bx)
		pos += size
	}
	return out
}

func avtBoxPath(boxes []avtBox, path ...string) *avtBox {
	for i := range boxes {
		if boxes[i].typ == path[0] {
			if len(path) == 1 {
				return &boxes[i]
			}
			return avtBoxPath(boxes[i].kids, path[1:]...)
		}
	}
	return nil
}

func validateISOBMFF(t *testing.T, data []byte, brand string) {
	boxes := avtParseBoxes(t, data, 0)
	if boxes[0].typ != "ftyp" || string(boxes[0].data[:4]) != brand {
		t.Fatalf("first box must be ftyp with major brand %q", brand)
	}
	if avtBoxPath(boxes, "moov", "mvhd") == nil {
		t.Fatalf("moov/mvhd missing")
	}
	stbl := []string{"moov", "trak", "mdia", "minf", "stbl"}
	stsz := avtBoxPath(boxes, append(stbl, "stsz")...)
	stco := avtBoxPath(boxes, append(stbl, "stco")...)
	stsd := avtBoxPath(boxes, append(stbl, "stsd")...)
	mdat := avtBoxPath(boxes, "mdat")
	if stsz == nil || stco == nil || stsd == nil || mdat == nil {
		t.Fatalf("sample tables or mdat missing")
	}
	sampleSize := int(binary.BigEndian.Uint32(stsz.data[12:]))
	chunkOff := int(binary.BigEndian.Uint32(stco.data[8:]))
	if chunkOff < mdat.off || chunkOff+sampleSize > mdat.off+len(mdat.data) {
		t.Fatalf("sample at %d+%d lies outside mdat", chunkOff, sampleSize)
	}
	sample := data[chunkOff : chunkOff+sampleSize]
	if !bytes.HasPrefix(sample, []byte{0xFF, 0xD8}) || !bytes.HasSuffix(sample, []byte{0xFF, 0xD9}) {
		t.Fatalf("sample is not a JPEG image")
	}
	for _, b := range boxes[1:] {
		if b.typ != "moov" && b.typ != "mdat" && b.typ != "free" {
			t.Fatalf("unexpected top-level box %q", b.typ)
		}
	}
}

// --- EBML (Matroska / WebM) ---

var avtEBMLMasters = map[uint64]bool{
	0x1A45DFA3: true, 0x18538067: true, 0x1549A966: true, 0x1654AE6B: true,
	0xAE: true, 0xE0: true, 0xE1: true, 0x1F43B675: true,
}

type avtElem struct {
	id   uint64
	data []byte
	kids []avtElem
}

func avtVINT(t *testing.T, b []byte, keepMarker bool) (uint64, int) {
	t.Helper()
	if len(b) == 0 || b[0] == 0 {
		t.Fatalf("invalid VINT")
	}
	w := 1
	for b[0]&(0x80>>(w-1)) == 0 {
		w++
	}
	if w > len(b) {
		t.Fatalf("truncated VINT")
	}
	v := uint64(b[0])
	if !keepMarker {
		v &= 0xFF >> w
	}
	for i := 1; i < w; i++ {
		v = v<<8 | uint64(b[i])
	}
	if !keepMarker && v == 1<<(7*w)-1 {
		t.Fatalf("unknown-size elements are not expected")
	}
	return v, w
}

func avtParseEBML(t *testing.T, b []byte) []avtElem {
	t.Helper()
	var out []avtElem
	for len(b) > 0 {
		id, iw := avtVINT(t, b, true)
		size, sw := avtVINT(t, b[iw:], false)
		start := iw + sw
		if uint64(len(b)-start) < size {
			t.Fatalf("element %#x size %d exceeds parent", id, size)
		}
		e := avtElem{id: id, data: b[start : start+int(size)]}
		if avtEBMLMasters[id] {
			e.kids = avtParseEBML(t, e.data)
		}
		out = append(out, e)
		b = b[start+int(size):]
	}
	return out
}

func avtElemFind(es []avtElem, id uint64) *avtElem {
	for i := range es {
		if es[i].id == id {
			return &es[i]
		}
	}
	return nil
}

func validateEBML(t *testing.T, data []byte, docType string) {
	top := avtParseEBML(t, data)
	if len(top) != 2 || top[0].id != 0x1A45DFA3 || top[1].id != 0x18538067 {
		t.Fatalf("file must be an EBML header followed by one Segment")
	}
	dt := avtElemFind(top[0].kids, 0x4282)
	if dt == nil || string(dt.data) != docType {
		t.Fatalf("DocType must be %q", docType)
	}
	seg := top[1].kids
	tracks := avtElemFind(seg, 0x1654AE6B)
	cluster := avtElemFind(seg, 0x1F43B675)
	if avtElemFind(seg, 0x1549A966) == nil || tracks == nil || cluster == nil {
		t.Fatalf("Segment needs Info, Tracks and Cluster")
	}
	entry := avtElemFind(tracks.kids, 0xAE)
	if entry == nil || avtElemFind(entry.kids, 0x86) == nil {
		t.Fatalf("Tracks needs a TrackEntry with a CodecID")
	}
	if avtElemFind(cluster.kids, 0xA3) == nil {
		t.Fatalf("Cluster needs a SimpleBlock")
	}
	for _, e := range seg {
		switch e.id {
		case 0x1549A966, 0x1654AE6B, 0x1F43B675:
		case 0xEC:
			// Void
		default:
			t.Fatalf("unexpected Segment child %#x", e.id)
		}
	}
}

// --- external tools ---

func TestVideoFormatsAcceptedByFFmpeg(t *testing.T) {
	codecs := map[string]string{"avi": "rawvideo", "mp4": "mjpeg", "mov": "mjpeg", "mkv": "mjpeg", "webm": "opus"}
	for ext, codec := range codecs {
		for _, size := range avtValidSizes(t, ext) {
			t.Run(fmt.Sprintf("%s/%d", ext, size), func(t *testing.T) { avtProbe(t, ext, size, codec) })
		}
	}
}

func TestVideoSizeRules(t *testing.T) {
	avi, _ := Default().Lookup("avi")
	if avi.Rule.Min != 260 || avi.Rule.Check(261) == nil || avi.Rule.Check(262) == nil || avi.Rule.Check(268) != nil {
		t.Errorf("unexpected AVI rule %+v", avi.Rule)
	}
	// The ISO and EBML containers can be produced in every size above the minimum.
	for _, ext := range []string{"mp4", "mov", "mkv", "webm"} {
		f, ok := Default().Lookup(ext)
		if !ok {
			t.Fatalf("%s not registered", ext)
		}
		if f.Rule.Align > 1 || len(f.Rule.Gaps) != 0 {
			t.Errorf("%s: expected contiguous sizes, got %+v", ext, f.Rule)
		}
	}
	if f, _ := Default().Lookup("m4v"); f.Ext != "mp4" {
		t.Error("m4v alias missing")
	}
}

// Small paddings use JPEG fill bytes / comments (ISO) or a widened Segment
// size field (EBML); each variant must still decode.
func TestVideoSmallPaddingsDecode(t *testing.T) {
	codecs := map[string]string{"mp4": "mjpeg", "mov": "mjpeg", "mkv": "mjpeg", "webm": "opus"}
	for ext, codec := range codecs {
		f, _ := Default().Lookup(ext)
		for pad := int64(1); pad <= 9; pad++ {
			size := f.Rule.Min + pad
			t.Run(fmt.Sprintf("%s/%d", ext, size), func(t *testing.T) { avtProbe(t, ext, size, codec) })
		}
	}
}
