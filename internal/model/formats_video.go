package model

import (
	"bytes"
	"encoding/binary"
	"io"
	"math"
)

func videoFormats() []Format {
	mp4Min := int64(len(avISOFile(avMP4Brand, 0)))
	movMin := int64(len(avISOFile(avMOVBrand, 0)))
	return []Format{
		{
			Ext: "avi", Name: "Audio Video Interleave (uncompressed RGB)", MIME: "video/x-msvideo", Category: CategoryVideo,
			Rule: SizeRule{Min: avAVIMin, Align: 2, Gaps: []Range{{avAVIMin + 1, avAVIMin + 7}},
				Note: "RIFF files always have an even number of bytes and a JUNK chunk needs at least 8"},
			Write: writeAVI,
		},
		{
			Ext: "mp4", Aliases: []string{"m4v"}, Name: "MPEG-4 Part 14 (Motion JPEG)", MIME: "video/mp4", Category: CategoryVideo,
			Rule:  SizeRule{Min: mp4Min},
			Write: func(w io.Writer, size int64) error { return writeISO(w, size, avMP4Brand, mp4Min) },
		},
		{
			Ext: "mov", Name: "QuickTime Movie (Photo JPEG)", MIME: "video/quicktime", Category: CategoryVideo,
			Rule:  SizeRule{Min: movMin},
			Write: func(w io.Writer, size int64) error { return writeISO(w, size, avMOVBrand, movMin) },
		},
		{
			Ext: "mkv", Name: "Matroska Video (Motion JPEG)", MIME: "video/x-matroska", Category: CategoryVideo,
			Rule:  SizeRule{Min: avMKV.min()},
			Write: avMKV.write,
		},
		{
			Ext: "webm", Name: "WebM (Opus audio track)", MIME: "video/webm", Category: CategoryVideo,
			Rule:  SizeRule{Min: avWebM.min()},
			Write: avWebM.write,
		},
	}
}

func avBE16(v int) []byte { return binary.BigEndian.AppendUint16(nil, uint16(v)) }

func avConcat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// avJPEG returns the 8x8 baseline JPEG of the image family with pad extra
// bytes (marker fill bytes or a comment segment).
func avJPEG(pad int) []byte {
	var b bytes.Buffer
	_ = writeJPEG(&b, jpegMin+int64(pad)) // writes to memory, cannot fail
	return b.Bytes()
}

// --- AVI: one uncompressed 1x1 BGR frame ---

const avAVIMin = 260

func avChunk(id string, data ...[]byte) []byte {
	payload := avConcat(data...)
	out := avConcat([]byte(id), avLE32(len(payload)), payload)
	if len(payload)%2 == 1 {
		out = append(out, 0)
	}
	return out
}

func avList(typ string, kids ...[]byte) []byte {
	return avChunk("LIST", append([][]byte{[]byte(typ)}, kids...)...)
}

func writeAVI(w io.Writer, size int64) error {
	avih := avConcat(avLE32(1_000_000), avLE32(4), avLE32(0), avLE32(0x10), // 1 fps, AVIF_HASINDEX
		avLE32(1), avLE32(0), avLE32(1), avLE32(4), avLE32(1), avLE32(1), make([]byte, 16))
	strh := avConcat([]byte("vids"), avLE32(0), avLE32(0), avLE16(0), avLE16(0), avLE32(0),
		avLE32(1), avLE32(1), avLE32(0), avLE32(1), avLE32(4), avLE32(0xFFFFFFFF), avLE32(0),
		avLE16(0), avLE16(0), avLE16(1), avLE16(1))
	strf := avConcat(avLE32(40), avLE32(1), avLE32(1), avLE16(1), avLE16(24), avLE32(0),
		avLE32(4), avLE32(0), avLE32(0), avLE32(0), avLE32(0))
	hdrl := avList("hdrl", avChunk("avih", avih), avList("strl", avChunk("strh", strh), avChunk("strf", strf)))
	movi := avList("movi", avChunk("00db", []byte{0xFF, 0x80, 0x00, 0x00})) // BGR pixel + row padding
	idx1 := avChunk("idx1", []byte("00db"), avLE32(0x10), avLE32(4), avLE32(4))

	if err := avWrite(w, []byte("RIFF"), avLE32(int(size-8)), []byte("AVI "), hdrl, movi, idx1); err != nil {
		return err
	}
	// Padding goes last: demuxers distrust huge chunks inside the header area.
	junk := size - avAVIMin
	if junk == 0 {
		return nil
	}
	if err := avWrite(w, []byte("JUNK"), avLE32(int(junk-8))); err != nil {
		return err
	}
	return Zeros(w, junk-8)
}

// --- ISO base media file format: MP4 and QuickTime with one JPEG frame ---

type avBrand struct {
	ftyp       []byte
	sampleType string
	mov        bool
}

var (
	avMP4Brand = avBrand{
		ftyp:       avConcat([]byte("isom"), avBE32(0x200), []byte("isomiso2mp41")),
		sampleType: "mp4v",
	}
	avMOVBrand = avBrand{
		ftyp:       avConcat([]byte("qt  "), avBE32(0x20050300), []byte("qt  ")),
		sampleType: "jpeg", mov: true,
	}
)

func avBox(typ string, parts ...[]byte) []byte {
	payload := avConcat(parts...)
	return avConcat(avBE32(8+len(payload)), []byte(typ), payload)
}

func avFullBox(typ string, flags int, parts ...[]byte) []byte {
	return avBox(typ, append([][]byte{avBE32(flags)}, parts...)...)
}

var avMatrix = avConcat(avBE32(0x10000), avBE32(0), avBE32(0), avBE32(0), avBE32(0x10000),
	avBE32(0), avBE32(0), avBE32(0), avBE32(0x40000000))

// avISOFile builds ftyp + moov + mdat around a JPEG sample padded by jpegPad bytes.
func avISOFile(b avBrand, jpegPad int) []byte {
	jpeg := avJPEG(jpegPad)
	ftyp := avBox("ftyp", b.ftyp)
	build := func(chunkOffset int) []byte {
		mvhd := avFullBox("mvhd", 0, avBE32(0), avBE32(0), avBE32(1000), avBE32(1000),
			avBE32(0x10000), avBE16(0x100), make([]byte, 10), avMatrix, make([]byte, 24), avBE32(2))
		tkhd := avFullBox("tkhd", 3, avBE32(0), avBE32(0), avBE32(1), avBE32(0), avBE32(1000),
			make([]byte, 8), avBE16(0), avBE16(0), avBE16(0), avBE16(0), avMatrix, avBE32(8<<16), avBE32(8<<16))
		mdhd := avFullBox("mdhd", 0, avBE32(0), avBE32(0), avBE32(1000), avBE32(1000), avBE16(0x55C4), avBE16(0))
		hdlr := avFullBox("hdlr", 0, avBE32(0), []byte("vide"), make([]byte, 12), []byte("VideoHandler\x00"))
		dref := avFullBox("dref", 0, avBE32(1), avFullBox("url ", 1))
		name := make([]byte, 32)
		if b.mov {
			hdlr = avFullBox("hdlr", 0, []byte("mhlrvide"), make([]byte, 12), []byte("\x0cVideoHandler"))
			dref = avFullBox("dref", 0, avBE32(1), avFullBox("alis", 1))
			copy(name, "\x0cPhoto - JPEG")
		}
		entry := [][]byte{make([]byte, 6), avBE16(1), make([]byte, 16), avBE16(8), avBE16(8),
			avBE32(0x480000), avBE32(0x480000), avBE32(0), avBE16(1), name, avBE16(24), avBE16(0xFFFF)}
		if !b.mov {
			// ES descriptor: objectTypeIndication 0x6C = JPEG (ISO/IEC 10918-1), visual stream
			dcd := avConcat([]byte{0x04, 13, 0x6C, 0x11, 0, 0, 0}, avBE32(0), avBE32(0))
			es := avConcat([]byte{0x03, 21, 0, 0, 0}, dcd, []byte{0x06, 1, 2})
			entry = append(entry, avFullBox("esds", 0, es))
		}
		stbl := avBox("stbl",
			avFullBox("stsd", 0, avBE32(1), avBox(b.sampleType, entry...)),
			avFullBox("stts", 0, avBE32(1), avBE32(1), avBE32(1000)),
			avFullBox("stsc", 0, avBE32(1), avBE32(1), avBE32(1), avBE32(1)),
			avFullBox("stsz", 0, avBE32(0), avBE32(1), avBE32(len(jpeg))),
			avFullBox("stco", 0, avBE32(1), avBE32(chunkOffset)))
		minf := avBox("minf", avFullBox("vmhd", 1, make([]byte, 8)), avBox("dinf", dref), stbl)
		trak := avBox("trak", tkhd, avBox("mdia", mdhd, hdlr, minf))
		return avBox("moov", mvhd, trak)
	}
	moov := build(0)
	moov = build(len(ftyp) + len(moov) + 8) // sizes do not depend on the offset value
	return avConcat(ftyp, moov, avBox("mdat", jpeg))
}

func writeISO(w io.Writer, size int64, b avBrand, min int64) error {
	pad := size - min
	if pad < 8 {
		_, err := w.Write(avISOFile(b, int(pad)))
		return err
	}
	if err := avWrite(w, avISOFile(b, 0), avBE32(int(pad)), []byte("free")); err != nil {
		return err
	}
	return Zeros(w, pad-8)
}

// --- EBML: Matroska and WebM ---

// avVINTWidth is the minimal EBML VINT width able to hold v (all-ones is reserved).
func avVINTWidth(v uint64) int {
	w := 1
	for w < 8 && v > 1<<(7*w)-2 {
		w++
	}
	return w
}

func avVINT(v uint64, w int) []byte {
	b := make([]byte, w)
	v |= 1 << (7 * w)
	for i := w - 1; i >= 0; i-- {
		b[i] = byte(v)
		v >>= 8
	}
	return b
}

func avEBMLID(id uint64) []byte {
	b := binary.BigEndian.AppendUint32(nil, uint32(id))
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return b
}

func avEl(id uint64, parts ...[]byte) []byte {
	payload := avConcat(parts...)
	return avConcat(avEBMLID(id), avVINT(uint64(len(payload)), avVINTWidth(uint64(len(payload)))), payload)
}

func avElUint(id, v uint64) []byte {
	b := binary.BigEndian.AppendUint64(nil, v)
	for len(b) > 1 && b[0] == 0 {
		b = b[1:]
	}
	return avEl(id, b)
}

func avElFloat(id uint64, f float64) []byte {
	return avEl(id, binary.BigEndian.AppendUint64(nil, math.Float64bits(f)))
}

func avElStr(id uint64, s string) []byte { return avEl(id, []byte(s)) }

type avMatroska struct {
	header []byte // EBML header
	body   []byte // Segment children: Info, Tracks, Cluster
}

const avSegmentID = 0x18538067

func newAVMatroska(docType string, durationMS float64, track, block []byte) avMatroska {
	return avMatroska{
		header: avEl(0x1A45DFA3, avElUint(0x4286, 1), avElUint(0x42F7, 1), avElUint(0x42F2, 4),
			avElUint(0x42F3, 8), avElStr(0x4282, docType), avElUint(0x4287, 4), avElUint(0x4285, 2)),
		body: avConcat(
			avEl(0x1549A966, avElUint(0x2AD7B1, 1_000_000), avElStr(0x4D80, "bbdx_file_gen"),
				avElStr(0x5741, "bbdx_file_gen"), avElFloat(0x4489, durationMS)),
			avEl(0x1654AE6B, avEl(0xAE, track)),
			avEl(0x1F43B675, avElUint(0xE7, 0), avEl(0xA3, []byte{0x81, 0, 0, 0x80}, block))),
	}
}

var (
	avMKV = newAVMatroska("matroska", 1000, avConcat(
		avElUint(0xD7, 1), avElUint(0x73C5, 1), avElUint(0x83, 1), avElUint(0x9C, 0),
		avElStr(0x86, "V_MJPEG"), avElUint(0x23E383, 1_000_000_000),
		avEl(0xE0, avElUint(0xB0, 8), avElUint(0xBA, 8))), avJPEG(0))

	avOpusHead = avConcat([]byte("OpusHead"), []byte{1, 1}, avLE16(312), avLE32(48000), avLE16(0), []byte{0})
	avWebM     = newAVMatroska("webm", 20, avConcat(
		avElUint(0xD7, 1), avElUint(0x73C5, 1), avElUint(0x83, 2), avElUint(0x9C, 0),
		avElStr(0x86, "A_OPUS"), avEl(0x63A2, avOpusHead),
		avElUint(0x56AA, 6_500_000), avElUint(0x56BB, 80_000_000),
		avEl(0xE1, avElFloat(0xB5, 48000), avElUint(0x9F, 1))),
		[]byte{0xF8, 0xFF, 0xFE}) // one 20 ms CELT silence frame
)

func (m avMatroska) min() int64 {
	return int64(len(m.header) + 4 + avVINTWidth(uint64(len(m.body))) + len(m.body))
}

// write emits the file with a Void element after the Cluster. Since EBML
// sizes may be stored wider than necessary, a 1-byte remainder is absorbed by
// widening the Segment size field, so every size >= min is reachable.
func (m avMatroska) write(w io.Writer, size int64) error {
	fixed := int64(len(m.header) + 4 + len(m.body))
	for ws := avVINTWidth(uint64(len(m.body))); ws <= 8; ws++ {
		void := size - fixed - int64(ws)
		if void < 0 {
			break
		}
		if void == 1 || avVINTWidth(uint64(int64(len(m.body))+void)) > ws {
			continue
		}
		if err := avWrite(w, m.header, avEBMLID(avSegmentID),
			avVINT(uint64(int64(len(m.body))+void), ws), m.body); err != nil {
			return err
		}
		if void == 0 {
			return nil
		}
		vw := 1
		for void-1-int64(vw) > 1<<(7*vw)-2 {
			vw++
		}
		if err := avWrite(w, []byte{0xEC}, avVINT(uint64(void-1-int64(vw)), vw)); err != nil {
			return err
		}
		return Zeros(w, void-1-int64(vw))
	}
	return io.ErrShortWrite
}
