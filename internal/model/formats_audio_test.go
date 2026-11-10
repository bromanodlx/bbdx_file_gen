package model

import (
	"bytes"
	"crypto/md5"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

func init() {
	registerValidator("wav", validateWAV)
	registerValidator("mp3", validateMP3)
	registerValidator("flac", validateFLAC)
	registerValidator("mid", validateMIDI)
}

// --- RIFF (shared with AVI) ---

type avtChunk struct {
	id, list string
	data     []byte
	kids     []avtChunk
}

func avtParseRIFF(t *testing.T, data []byte, form string) []avtChunk {
	t.Helper()
	if len(data) < 12 || string(data[:4]) != "RIFF" {
		t.Fatalf("missing RIFF header")
	}
	if got := int(binary.LittleEndian.Uint32(data[4:])) + 8; got != len(data) {
		t.Fatalf("RIFF size field says %d bytes, file has %d", got, len(data))
	}
	if string(data[8:12]) != form {
		t.Fatalf("RIFF form %q, want %q", data[8:12], form)
	}
	return avtParseChunks(t, data[12:])
}

func avtParseChunks(t *testing.T, b []byte) []avtChunk {
	t.Helper()
	var out []avtChunk
	for len(b) > 0 {
		if len(b) < 8 {
			t.Fatalf("truncated chunk header (%d bytes left)", len(b))
		}
		id := string(b[:4])
		size := int(binary.LittleEndian.Uint32(b[4:]))
		padded := size + size&1
		if 8+padded > len(b) {
			t.Fatalf("chunk %q size %d exceeds parent (%d left)", id, size, len(b)-8)
		}
		c := avtChunk{id: id, data: b[8 : 8+size]}
		if id == "LIST" {
			if size < 4 {
				t.Fatalf("LIST chunk too small")
			}
			c.list = string(c.data[:4])
			c.kids = avtParseChunks(t, c.data[4:])
		}
		out = append(out, c)
		b = b[8+padded:]
	}
	return out
}

func avtFind(cs []avtChunk, id string) *avtChunk {
	for i := range cs {
		if cs[i].id == id || (cs[i].id == "LIST" && cs[i].list == id) {
			return &cs[i]
		}
	}
	return nil
}

func validateWAV(t *testing.T, data []byte) {
	cs := avtParseRIFF(t, data, "WAVE")
	fmtc, dat := avtFind(cs, "fmt "), avtFind(cs, "data")
	if fmtc == nil || dat == nil {
		t.Fatalf("WAV needs fmt and data chunks")
	}
	if len(fmtc.data) < 16 || binary.LittleEndian.Uint16(fmtc.data) != 1 {
		t.Fatalf("fmt chunk is not PCM")
	}
	ch := binary.LittleEndian.Uint16(fmtc.data[2:])
	rate := binary.LittleEndian.Uint32(fmtc.data[4:])
	byteRate := binary.LittleEndian.Uint32(fmtc.data[8:])
	align := binary.LittleEndian.Uint16(fmtc.data[12:])
	bits := binary.LittleEndian.Uint16(fmtc.data[14:])
	if align != ch*bits/8 || byteRate != rate*uint32(align) {
		t.Fatalf("inconsistent fmt chunk")
	}
	if len(dat.data)%int(align) != 0 {
		t.Fatalf("data chunk is not a whole number of sample frames")
	}
}

// --- MP3 ---

func validateMP3(t *testing.T, data []byte) {
	b := data
	if len(b) >= 10 && string(b[:3]) == "ID3" {
		sz := avtSyncsafe(b[6:10])
		if 10+sz > len(b) {
			t.Fatalf("ID3 tag larger than file")
		}
		tag := b[10 : 10+sz]
		if len(tag) < 11 {
			t.Fatalf("ID3 tag must contain at least one frame")
		}
		fsz := avtSyncsafe(tag[4:8])
		for _, c := range tag[:4] {
			if !(c >= 'A' && c <= 'Z' || c >= '0' && c <= '9') {
				t.Fatalf("bad ID3 frame id %q", tag[:4])
			}
		}
		if fsz < 1 || 10+fsz > len(tag) {
			t.Fatalf("bad ID3 frame size %d", fsz)
		}
		for _, p := range tag[10+fsz:] {
			if p != 0 {
				t.Fatalf("ID3 padding must be zero bytes")
			}
		}
		b = b[10+sz:]
	}
	frames := 0
	for len(b) > 0 {
		if len(b) < 4 || b[0] != 0xFF || b[1]&0xE0 != 0xE0 {
			t.Fatalf("expected MPEG frame sync, %d bytes left", len(b))
		}
		if b[1] != 0xFB {
			t.Fatalf("expected MPEG-1 Layer III without CRC, got %#x", b[1])
		}
		bitrates := []int{0, 32, 40, 48, 56, 64, 80, 96, 112, 128, 160, 192, 224, 256, 320}
		rates := []int{44100, 48000, 32000}
		bi, si := int(b[2]>>4), int(b[2]>>2&3)
		if bi == 0 || bi >= len(bitrates) || si >= len(rates) {
			t.Fatalf("invalid bitrate/sample-rate index")
		}
		n := 144*bitrates[bi]*1000/rates[si] + int(b[2]>>1&1)
		if n > len(b) {
			t.Fatalf("truncated frame")
		}
		b = b[n:]
		frames++
	}
	if frames == 0 {
		t.Fatalf("no MPEG audio frames")
	}
}

func avtSyncsafe(b []byte) int {
	return int(b[0])<<21 | int(b[1])<<14 | int(b[2])<<7 | int(b[3])
}

// --- FLAC ---

func avtCRC(data []byte, width int, poly uint32) uint32 {
	var crc uint32
	top := uint32(1) << (width - 1)
	mask := uint32(1)<<width - 1
	for _, d := range data {
		crc ^= uint32(d) << (width - 8)
		for i := 0; i < 8; i++ {
			if crc&top != 0 {
				crc = (crc << 1) ^ poly
			} else {
				crc <<= 1
			}
			crc &= mask
		}
	}
	return crc
}

func validateFLAC(t *testing.T, data []byte) {
	if len(data) < 42 || string(data[:4]) != "fLaC" {
		t.Fatalf("missing fLaC marker")
	}
	b := data[4:]
	first := true
	for {
		if len(b) < 4 {
			t.Fatalf("truncated metadata block header")
		}
		last, typ := b[0]&0x80 != 0, b[0]&0x7F
		n := int(b[1])<<16 | int(b[2])<<8 | int(b[3])
		if 4+n > len(b) {
			t.Fatalf("metadata block exceeds file")
		}
		if first {
			if typ != 0 || n != 34 {
				t.Fatalf("first block must be a 34-byte STREAMINFO")
			}
			si := b[4 : 4+34]
			total := binary.BigEndian.Uint64(si[10:18]) & (1<<36 - 1)
			want := md5.Sum(make([]byte, total*2))
			if !bytes.Equal(si[18:34], want[:]) {
				t.Fatalf("STREAMINFO MD5 does not match the silent samples")
			}
		} else if typ != 1 {
			t.Fatalf("unexpected metadata block type %d", typ)
		} else if bytes.Count(b[4:4+n], []byte{0}) != n {
			t.Fatalf("PADDING must be zero bytes")
		}
		first = false
		b = b[4+n:]
		if last {
			break
		}
	}
	if len(b) != 11 {
		t.Fatalf("expected exactly one 11-byte frame after metadata, have %d bytes", len(b))
	}
	if b[0] != 0xFF || b[1] != 0xF8 {
		t.Fatalf("missing frame sync")
	}
	if avtCRC(b[:5], 8, 0x07) != uint32(b[5]) {
		t.Fatalf("frame header CRC-8 mismatch")
	}
	if avtCRC(b[:9], 16, 0x8005) != uint32(binary.BigEndian.Uint16(b[9:])) {
		t.Fatalf("frame CRC-16 mismatch")
	}
}

// --- MIDI ---

func validateMIDI(t *testing.T, data []byte) {
	if len(data) < 14 || string(data[:4]) != "MThd" || binary.BigEndian.Uint32(data[4:]) != 6 {
		t.Fatalf("missing MThd header")
	}
	if f := binary.BigEndian.Uint16(data[8:]); f != 0 || binary.BigEndian.Uint16(data[10:]) != 1 {
		t.Fatalf("expected format 0 with one track")
	}
	b := data[14:]
	if len(b) < 8 || string(b[:4]) != "MTrk" {
		t.Fatalf("missing MTrk chunk")
	}
	trk := b[8:]
	if int(binary.BigEndian.Uint32(b[4:])) != len(trk) {
		t.Fatalf("MTrk length does not reach end of file")
	}
	vlq := func() int {
		v := 0
		for i := 0; i < 4; i++ {
			if len(trk) == 0 {
				t.Fatalf("truncated VLQ")
			}
			c := trk[0]
			trk = trk[1:]
			v = v<<7 | int(c&0x7F)
			if c&0x80 == 0 {
				return v
			}
		}
		t.Fatalf("VLQ longer than 4 bytes")
		return 0
	}
	for {
		vlq()
		if len(trk) < 2 || trk[0] != 0xFF {
			t.Fatalf("expected a meta event")
		}
		typ := trk[1]
		trk = trk[2:]
		n := vlq()
		if n > len(trk) {
			t.Fatalf("meta event exceeds track")
		}
		trk = trk[n:]
		if typ == 0x2F {
			if n != 0 || len(trk) != 0 {
				t.Fatalf("end-of-track must be the final event")
			}
			return
		}
	}
}

// --- external tools ---

// avtValidSizes picks a spread of sizes the format's rule accepts.
func avtValidSizes(t *testing.T, ext string) []int64 {
	f, _ := Default().Lookup(ext)
	out := []int64{f.Rule.Min}
	for _, s := range []int64{f.Rule.Min + 1, f.Rule.Min + 30, 1000, 65537, 1<<20 + 3} {
		if err := f.Rule.Check(s); err != nil {
			s = err.(*SizeError).Above
		}
		out = append(out, s)
	}
	return out
}

// avtRun runs a tool and fails when it reports anything on stderr.
func avtRun(t *testing.T, name string, args ...string) string {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Skipf("%s not installed", name)
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil || strings.TrimSpace(stderr.String()) != "" {
		t.Fatalf("%s %v: %v\n%s", name, args, err, stderr.String())
	}
	return stdout.String()
}

func avtProbe(t *testing.T, ext string, size int64, wantCodec string) {
	t.Helper()
	p := writeTemp(t, ext, generate(t, ext, size))
	out := avtRun(t, "ffprobe", "-v", "error", "-show_format", "-show_streams", p)
	if !strings.Contains(out, "codec_name="+wantCodec) {
		t.Fatalf("%s/%d: ffprobe did not report codec %s:\n%s", ext, size, wantCodec, out)
	}
	avtRun(t, "ffmpeg", "-nostdin", "-v", "error", "-i", p, "-f", "null", "-")
}

func TestAudioFormatsAcceptedByFFmpeg(t *testing.T) {
	for ext, codec := range map[string]string{"wav": "pcm_u8", "mp3": "mp3", "flac": "flac"} {
		for _, size := range avtValidSizes(t, ext) {
			t.Run(fmt.Sprintf("%s/%d", ext, size), func(t *testing.T) { avtProbe(t, ext, size, codec) })
		}
	}
}

func TestMIDIRecognizedByFile(t *testing.T) {
	for _, size := range avtValidSizes(t, "mid") {
		p := writeTemp(t, "mid", generate(t, "mid", size))
		if out := runTool(t, "file", "-b", p); !strings.Contains(out, "Standard MIDI data") {
			t.Errorf("mid/%d: file says %q", size, out)
		}
	}
}

func TestAudioSizeRules(t *testing.T) {
	cases := map[string]struct {
		min  int64
		bad  []int64
		good []int64
	}{
		"wav":  {44, []int64{45, 47}, []int64{44, 46, 1000}},
		"mp3":  {192, []int64{191, 195, 212}, []int64{192, 193, 194, 213, 500}},
		"flac": {53, []int64{54, 56}, []int64{53, 57, 58}},
		"mid":  {26, []int64{27, 29}, []int64{26, 30, 31, 160, 300}},
	}
	for ext, c := range cases {
		f, ok := Default().Lookup(ext)
		if !ok {
			t.Fatalf("%s not registered", ext)
		}
		if f.Rule.Min != c.min {
			t.Errorf("%s: min %d, want %d", ext, f.Rule.Min, c.min)
		}
		for _, s := range c.bad {
			if f.Rule.Check(s) == nil {
				t.Errorf("%s: size %d should be rejected", ext, s)
			}
		}
		for _, s := range c.good {
			if err := f.Rule.Check(s); err != nil {
				t.Errorf("%s: size %d should be accepted: %v", ext, s, err)
			}
		}
	}
	if f, _ := Default().Lookup("midi"); f.Ext != "mid" {
		t.Error("midi alias missing")
	}
}

// Large files exercise multiple FLAC PADDING blocks (24-bit length limit) and
// long runs of MP3 frames.
func TestAudioLargeFilesDecode(t *testing.T) {
	if testing.Short() {
		t.Skip("large files")
	}
	for ext, codec := range map[string]string{"flac": "flac", "mp3": "mp3"} {
		size := int64(40<<20 + 7)
		t.Run(ext, func(t *testing.T) {
			data := generate(t, ext, size)
			validators[ext](t, data)
			p := writeTemp(t, ext, data)
			out := avtRun(t, "ffprobe", "-v", "error", "-show_streams", p)
			if !strings.Contains(out, "codec_name="+codec) {
				t.Fatalf("ffprobe: %s", out)
			}
			avtRun(t, "ffmpeg", "-nostdin", "-v", "error", "-i", p, "-f", "null", "-")
		})
	}
}
