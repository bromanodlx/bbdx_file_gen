package model

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func init() {
	registerValidator("png", validatePNG)
	registerValidator("jpg", validateJPEG)
	registerValidator("gif", validateGIF)
	registerValidator("bmp", validateBMP)
	registerValidator("tiff", validateTIFF)
	registerValidator("ico", validateICO)
	registerValidator("webp", validateWebP)
}

func validatePNG(t *testing.T, data []byte) {
	t.Helper()
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Errorf("png decode: %v", err)
		return
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
		t.Error("png: bad signature")
		return
	}
	p := data[8:]
	var last string
	for len(p) > 0 {
		if len(p) < 12 {
			t.Error("png: truncated chunk")
			return
		}
		n := binary.BigEndian.Uint32(p)
		if int(n)+12 > len(p) {
			t.Error("png: chunk overruns file")
			return
		}
		if crc32.ChecksumIEEE(p[4:8+n]) != binary.BigEndian.Uint32(p[8+n:]) {
			t.Errorf("png: bad CRC in %s", p[4:8])
		}
		last = string(p[4:8])
		p = p[12+n:]
	}
	if last != "IEND" {
		t.Error("png: last chunk is not IEND")
	}
}

func validateJPEG(t *testing.T, data []byte) {
	t.Helper()
	if _, err := jpeg.Decode(bytes.NewReader(data)); err != nil {
		t.Errorf("jpeg decode: %v", err)
	}
	if !bytes.HasPrefix(data, []byte{0xFF, 0xD8}) || !bytes.HasSuffix(data, []byte{0xFF, 0xD9}) {
		t.Error("jpeg: must start with SOI and end with EOI")
	}
}

func validateGIF(t *testing.T, data []byte) {
	t.Helper()
	if _, err := gif.DecodeAll(bytes.NewReader(data)); err != nil {
		t.Errorf("gif decode: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("GIF89a")) || data[len(data)-1] != 0x3B {
		t.Error("gif: bad header or trailer")
	}
}

func validateBMP(t *testing.T, data []byte) {
	t.Helper()
	le := binary.LittleEndian
	if len(data) < 58 || string(data[:2]) != "BM" {
		t.Error("bmp: bad signature")
		return
	}
	if int(le.Uint32(data[2:])) != len(data) {
		t.Error("bmp: bfSize mismatch")
	}
	off := int(le.Uint32(data[10:]))
	if le.Uint32(data[14:]) != 40 || le.Uint16(data[28:]) != 24 {
		t.Error("bmp: expected BITMAPINFOHEADER with 24 bpp")
	}
	w, h := int(le.Uint32(data[18:])), int(le.Uint32(data[22:]))
	stride := (w*3 + 3) &^ 3
	if off < 54 || off+stride*h != len(data) || int(le.Uint32(data[34:])) != stride*h {
		t.Errorf("bmp: pixel array inconsistent (off=%d w=%d h=%d len=%d)", off, w, h, len(data))
	}
}

func validateTIFF(t *testing.T, data []byte) {
	t.Helper()
	le := binary.LittleEndian
	if len(data) < 8 || string(data[:4]) != "II*\x00" {
		t.Error("tiff: bad header")
		return
	}
	ifd := int(le.Uint32(data[4:]))
	if ifd%2 != 0 || ifd+2 > len(data) {
		t.Error("tiff: bad IFD offset")
		return
	}
	n := int(le.Uint16(data[ifd:]))
	tags := map[uint16]uint32{}
	prev := uint16(0)
	for i := 0; i < n; i++ {
		e := data[ifd+2+12*i:]
		tag, typ := le.Uint16(e), le.Uint16(e[2:])
		if tag <= prev {
			t.Error("tiff: tags not sorted")
		}
		prev = tag
		v := le.Uint32(e[8:])
		if typ == 3 { // SHORT
			v = uint32(le.Uint16(e[8:]))
		}
		if typ == 2 && le.Uint32(e[4:]) > 4 { // ASCII stored at offset
			cnt := le.Uint32(e[4:])
			if int(v+cnt) > len(data) || data[v+cnt-1] != 0 {
				t.Error("tiff: ASCII value not NUL terminated")
			}
		}
		tags[tag] = v
	}
	if le.Uint32(data[ifd+2+12*n:]) != 0 {
		t.Error("tiff: more than one IFD")
	}
	for _, req := range []uint16{256, 257, 258, 259, 262, 273, 277, 278, 279, 282, 283, 296} {
		if _, ok := tags[req]; !ok {
			t.Errorf("tiff: missing required tag %d", req)
		}
	}
	w, h := tags[256], tags[257]
	if tags[279] != w*h || int(tags[273]+tags[279]) != len(data) {
		t.Errorf("tiff: strip inconsistent (w=%d h=%d off=%d cnt=%d len=%d)", w, h, tags[273], tags[279], len(data))
	}
}

func validateICO(t *testing.T, data []byte) {
	t.Helper()
	le := binary.LittleEndian
	if len(data) < 22 || le.Uint16(data) != 0 || le.Uint16(data[2:]) != 1 || le.Uint16(data[4:]) != 1 {
		t.Error("ico: bad ICONDIR")
		return
	}
	size, off := int(le.Uint32(data[14:])), int(le.Uint32(data[18:]))
	if off != 22 || off+size != len(data) {
		t.Error("ico: entry size/offset mismatch")
		return
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data[22:]))
	if err != nil {
		t.Errorf("ico: embedded png: %v", err)
		return
	}
	if cfg.Width != int(data[6]) || cfg.Height != int(data[7]) {
		t.Error("ico: entry dimensions differ from png")
	}
	validatePNG(t, data[22:])
}

func validateWebP(t *testing.T, data []byte) {
	t.Helper()
	le := binary.LittleEndian
	if len(data) < 20 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		t.Error("webp: bad RIFF header")
		return
	}
	if int(le.Uint32(data[4:]))+8 != len(data) {
		t.Error("webp: RIFF size mismatch")
	}
	var ids []string
	p := data[12:]
	for len(p) > 0 {
		if len(p) < 8 {
			t.Error("webp: truncated chunk")
			return
		}
		n := int(le.Uint32(p[4:]))
		total := 8 + n + n&1
		if total > len(p) {
			t.Error("webp: chunk overruns file")
			return
		}
		ids = append(ids, string(p[:4]))
		if string(p[:4]) == "VP8L" && p[8] != 0x2f {
			t.Error("webp: bad VP8L signature")
		}
		p = p[total:]
	}
	switch {
	case len(ids) == 1 && ids[0] == "VP8L":
	case len(ids) >= 2 && ids[0] == "VP8X" && ids[1] == "VP8L":
	default:
		t.Errorf("webp: unexpected chunk layout %v", ids)
	}
}

func TestImageAliases(t *testing.T) {
	for alias, ext := range map[string]string{"jpeg": "jpg", "tif": "tiff"} {
		if f, ok := Default().Lookup(alias); !ok || f.Ext != ext {
			t.Errorf("alias %s does not resolve to %s", alias, ext)
		}
	}
	for _, ext := range []string{"png", "jpg", "gif", "bmp", "tiff", "ico", "webp"} {
		f, ok := Default().Lookup(ext)
		if !ok || f.Category != CategoryImage {
			t.Errorf("%s missing or not an image", ext)
		}
	}
}

func TestImageContiguousSizes(t *testing.T) {
	// Formats without alignment constraints must not have gaps beyond what
	// is documented; png, jpg, bmp, tiff, ico and gif are fully contiguous.
	for _, ext := range []string{"png", "jpg", "gif", "bmp", "tiff", "ico"} {
		f, _ := Default().Lookup(ext)
		if len(f.Rule.Gaps) != 0 || f.Rule.Align > 1 {
			t.Errorf("%s should be contiguous from its minimum", ext)
		}
	}
}

// imageToolSizes are sizes checked with external decoders.
func imageToolSizes(f Format) []int64 {
	var out []int64
	for _, s := range []int64{0, 1, 2, 3, 4, 5, 6, 19, 20, 21, 26, 100, 70001, 200000} {
		if f.Rule.Check(f.Rule.Min+s) == nil {
			out = append(out, f.Rule.Min+s)
		}
	}
	return out
}

func TestImageIdentify(t *testing.T) {
	for _, ext := range []string{"png", "jpg", "gif", "bmp", "tiff", "ico", "webp"} {
		f, _ := Default().Lookup(ext)
		for _, size := range imageToolSizes(f) {
			p := writeTemp(t, ext, generate(t, ext, size))
			out := runTool(t, "identify", "-regard-warnings", p)
			if !bytes.Contains(bytes.ToUpper([]byte(out)), bytes.ToUpper([]byte(map[string]string{
				"jpg": "JPEG", "tiff": "TIFF", "ico": "ICO",
			}[ext]))) {
				t.Errorf("%s/%d: unexpected identify output %q", ext, size, out)
			}
		}
	}
}

func TestImageFFprobeWebP(t *testing.T) {
	f, _ := Default().Lookup("webp")
	for _, size := range imageToolSizes(f) {
		p := writeTemp(t, "webp", generate(t, "webp", size))
		out := runTool(t, "ffprobe", "-v", "error", "-show_entries", "stream=codec_name,width,height", "-of", "csv=p=0", p)
		if !bytes.Contains([]byte(out), []byte("webp")) {
			t.Errorf("webp/%d: ffprobe output %q", size, out)
		}
	}
}
