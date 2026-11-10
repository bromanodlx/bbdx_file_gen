package model

import (
	"encoding/binary"
	"hash/adler32"
	"hash/crc32"
	"io"
)

func imageFormats() []Format {
	return []Format{
		{
			Ext: "png", Name: "Portable Network Graphics", MIME: "image/png", Category: CategoryImage,
			Rule:  SizeRule{Min: pngMin},
			Write: writePNG,
		},
		{
			Ext: "jpg", Aliases: []string{"jpeg", "jpe"}, Name: "JPEG image (baseline JFIF-compatible)",
			MIME: "image/jpeg", Category: CategoryImage,
			Rule:  SizeRule{Min: jpegMin},
			Write: writeJPEG,
		},
		{
			Ext: "gif", Name: "Graphics Interchange Format (GIF89a)", MIME: "image/gif", Category: CategoryImage,
			Rule:  SizeRule{Min: gifMin},
			Write: writeGIF,
		},
		{
			Ext: "bmp", Name: "Windows bitmap", MIME: "image/bmp", Category: CategoryImage,
			Rule:  SizeRule{Min: bmpMin},
			Write: writeBMP,
		},
		{
			Ext: "tiff", Aliases: []string{"tif"}, Name: "Tagged Image File Format (baseline)",
			MIME: "image/tiff", Category: CategoryImage,
			Rule:  SizeRule{Min: tiffMin},
			Write: writeTIFF,
		},
		{
			Ext: "ico", Name: "Windows icon (PNG payload)", MIME: "image/vnd.microsoft.icon", Category: CategoryImage,
			Rule:  SizeRule{Min: icoHeader + pngMin},
			Write: writeICO,
		},
		{
			Ext: "webp", Name: "WebP image (lossless)", MIME: "image/webp", Category: CategoryImage,
			Rule: SizeRule{
				Min: webpSimple, Align: 2,
				Gaps: []Range{{webpSimple + 1, webpExtended - 1}, {webpExtended + 1, webpExtended + 7}},
				Note: "WebP is a RIFF container (always an even number of bytes); padding needs the extended layout (+18 bytes) and an 8-byte chunk header",
			},
			Write: writeWebP,
		},
	}
}

// ---------------------------------------------------------------- PNG

// A 1-row, 8-bit grayscale image with a single stored-deflate IDAT:
// signature 8 + IHDR 25 + IDAT (24+width) + IEND 12 = 69 + width.
const (
	pngMin          = 70
	pngTextOverhead = 12 + len("Comment\x00")
)

// pngPlan returns the image width and the tEXt payload length (-1 = none).
// Small padding widens the image by one pixel per byte; larger padding goes
// into a tEXt "Comment" chunk, so every size >= pngMin is reachable.
func pngPlan(size int64) (width int, text int64) {
	p := size - pngMin
	if p < int64(pngTextOverhead) {
		return 1 + int(p), -1
	}
	return 1, p - int64(pngTextOverhead)
}

func pngChunk(b *binw, typ string, length int64, body func(io.Writer) error) {
	b.u32be(uint32(length))
	crc := crc32.NewIEEE()
	mw := io.MultiWriter(b.w, crc)
	if b.err == nil {
		_, b.err = io.WriteString(mw, typ)
	}
	if b.err == nil && body != nil {
		b.err = body(mw)
	}
	b.u32be(crc.Sum32())
}

func writePNG(w io.Writer, size int64) error {
	width, text := pngPlan(size)
	b := &binw{w: w}
	b.str("\x89PNG\r\n\x1a\n")
	pngChunk(b, "IHDR", 13, func(w io.Writer) error {
		_, err := w.Write(binary.BigEndian.AppendUint32(binary.BigEndian.AppendUint32(nil, uint32(width)), 1))
		if err == nil {
			_, err = w.Write([]byte{8, 0, 0, 0, 0}) // depth 8, grayscale, deflate, no filter, no interlace
		}
		return err
	})
	if text >= 0 {
		pngChunk(b, "tEXt", int64(len("Comment\x00"))+text, func(w io.Writer) error {
			if _, err := io.WriteString(w, "Comment\x00"); err != nil {
				return err
			}
			return Lorem(w, text, true)
		})
	}
	// Scanline: filter type 0 followed by width mid-gray pixels.
	raw := make([]byte, 1+width)
	for i := 1; i < len(raw); i++ {
		raw[i] = 0x80
	}
	z := []byte{0x78, 0x01, 0x01, byte(len(raw)), byte(len(raw) >> 8), ^byte(len(raw)), ^byte(len(raw) >> 8)}
	z = append(z, raw...)
	z = binary.BigEndian.AppendUint32(z, adler32.Checksum(raw))
	pngChunk(b, "IDAT", int64(len(z)), func(w io.Writer) error { _, err := w.Write(z); return err })
	pngChunk(b, "IEND", 0, nil)
	return b.err
}

// ---------------------------------------------------------------- JPEG

// jpegBody is a complete baseline 8x8 grayscale JPEG without its SOI:
// DQT (all ones), SOF0, minimal DC/AC Huffman tables with a single 1-bit
// code each (DC category 0, AC EOB), SOS, one entropy-coded byte, EOI.
var jpegBody = func() []byte {
	var p []byte
	p = append(p, 0xFF, 0xDB, 0x00, 67, 0x00)
	for i := 0; i < 64; i++ {
		p = append(p, 1)
	}
	p = append(p, 0xFF, 0xC0, 0x00, 11, 8, 0x00, 8, 0x00, 8, 1, 1, 0x11, 0)
	dht := func(class byte) []byte {
		seg := []byte{0xFF, 0xC4, 0x00, 20, class, 1}
		seg = append(seg, make([]byte, 15)...)
		return append(seg, 0x00)
	}
	p = append(p, dht(0x00)...)
	p = append(p, dht(0x10)...)
	p = append(p, 0xFF, 0xDA, 0x00, 8, 1, 1, 0x00, 0, 63, 0)
	p = append(p, 0x3F) // bits "0" (DC diff 0) "0" (EOB), padded with ones
	return append(p, 0xFF, 0xD9)
}()

const (
	jpegMin       = 2 + 139
	jpegComMax    = 65533 // data bytes per COM segment
	jpegComHeader = 4
)

func writeJPEG(w io.Writer, size int64) error {
	b := &binw{w: w}
	b.raw(0xFF, 0xD8)
	p := size - jpegMin
	if p >= jpegComHeader {
		// Split p bytes into n COM segments of 4 header + <= 65533 data bytes.
		n := (p + jpegComMax + jpegComHeader - 1) / (jpegComMax + jpegComHeader)
		data := p - n*jpegComHeader
		for i := int64(0); i < n; i++ {
			d := data / n
			if i < data%n {
				d++
			}
			b.raw(0xFF, 0xFE)
			b.u16be(uint16(d + 2))
			b.do(func(w io.Writer) error { return Lorem(w, d, true) })
		}
	} else {
		// B.1.1.2: any marker may be preceded by fill bytes 0xFF.
		b.do(func(w io.Writer) error { return Repeat(w, 0xFF, p) })
	}
	b.raw(jpegBody...)
	return b.err
}

// ---------------------------------------------------------------- GIF

// Header 6 + logical screen 7 + 2-colour table 6 + image descriptor 10 +
// image data (min code size, sub-blocks, terminator) + trailer.
const gifMin = 35

// gifLZW encodes one pixel of colour 0 with code size 3, preceded by
// `clears` clear codes (each clear is legal and only costs 3 bits).
func gifLZW(clears int) []byte {
	codes := make([]uint, 0, clears+2)
	for i := 0; i < clears; i++ {
		codes = append(codes, 4)
	}
	codes = append(codes, 0, 5)
	var out []byte
	var acc, nbits uint
	for _, c := range codes {
		acc |= c << nbits
		nbits += 3
		for nbits >= 8 {
			out = append(out, byte(acc))
			acc >>= 8
			nbits -= 8
		}
	}
	if nbits > 0 {
		out = append(out, byte(acc))
	}
	return out
}

func writeGIF(w io.Writer, size int64) error {
	b := &binw{w: w}
	b.str("GIF89a")
	b.u16le(1)
	b.u16le(1)
	b.raw(0x80, 0, 0)                // global colour table, 2 entries
	b.raw(0, 0, 0, 0xFF, 0xFF, 0xFF) // black, white
	p := size - gifMin
	// Paddings of 1..4 bytes come from longer LZW streams (extra clear
	// codes) split into more sub-blocks; larger ones from a comment extension.
	variants := [5]struct{ clears, blocks int }{{1, 1}, {1, 2}, {4, 2}, {4, 3}, {7, 3}}
	v := variants[0]
	if p < int64(len(variants)) {
		v = variants[p]
	} else {
		writeGIFComment(b, p)
	}
	b.raw(0x2C, 0, 0, 0, 0, 1, 0, 1, 0, 0)
	b.raw(2) // LZW minimum code size
	data := gifLZW(v.clears)
	for i := 0; i < v.blocks; i++ {
		n := len(data) / (v.blocks - i)
		b.raw(byte(n))
		b.raw(data[:n]...)
		data = data[n:]
	}
	b.raw(0x00, 0x3B)
	return b.err
}

// writeGIFComment writes a comment extension of exactly total (>= 5) bytes.
func writeGIFComment(b *binw, total int64) {
	b.raw(0x21, 0xFE)
	c := total - 3 // bytes taken by sub-blocks (length byte + 1..255 data)
	for c > 0 {
		n := c
		switch {
		case n > 256 && n-256 < 2:
			n = 255 // keep at least 2 bytes for the last sub-block
		case n > 256:
			n = 256
		}
		b.raw(byte(n - 1))
		b.do(func(w io.Writer) error { return Lorem(w, n-1, false) })
		c -= n
	}
	b.raw(0x00)
}

// ---------------------------------------------------------------- BMP

// 14-byte file header + 40-byte BITMAPINFOHEADER + 1x1 24-bit pixel row
// (3 bytes + 1 alignment byte). Padding is a gap before the pixel array,
// which the format allows via bfOffBits.
const bmpMin = 14 + 40 + 4

func writeBMP(w io.Writer, size int64) error {
	gap := size - bmpMin
	b := &binw{w: w}
	b.str("BM")
	b.u32le(uint32(size))
	b.u32le(0)
	b.u32le(uint32(54 + gap))
	b.u32le(40)
	b.u32le(1) // width
	b.u32le(1) // height
	b.u16le(1) // planes
	b.u16le(24)
	b.u32le(0) // BI_RGB
	b.u32le(4) // image size
	b.u32le(2835)
	b.u32le(2835)
	b.u32le(0)
	b.u32le(0)
	b.do(func(w io.Writer) error { return Zeros(w, gap) })
	b.raw(0xFF, 0xFF, 0xFF, 0x00)
	return b.err
}

// ---------------------------------------------------------------- TIFF

// Little-endian baseline grayscale TIFF: header, one IFD with the 12
// required baseline tags, X/YResolution rationals, then a single
// uncompressed strip. Larger files get a near-square image, with the
// remainder carried by an ImageDescription string.
const (
	tiffBaseTags = 12
	tiffMin      = 8 + 2 + 12*tiffBaseTags + 4 + 16 + 1
)

func isqrt(n int64) int64 {
	if n <= 0 {
		return 0
	}
	x := int64(1)
	for x*x <= n {
		x <<= 1
	}
	lo, hi := x>>1, x
	for lo+1 < hi {
		m := (lo + hi) / 2
		if m*m <= n {
			lo = m
		} else {
			hi = m
		}
	}
	return lo
}

func writeTIFF(w io.Writer, size int64) error {
	tags := tiffBaseTags
	width, height, desc := size-(tiffMin-1), int64(1), int64(0)
	if width > 64 {
		tags++
		a := size - (8 + 2 + 12*int64(tags) + 4 + 16)
		width = isqrt(a - 5)
		height = (a - 5) / width
		desc = a - width*height // >= 5 so it never fits inline
	}
	ifdEnd := int64(8 + 2 + 12*tags + 4)
	resOff := ifdEnd
	descOff := resOff + 16
	stripOff := descOff + desc

	b := &binw{w: w}
	b.str("II*\x00")
	b.u32le(8)
	b.u16le(uint16(tags))
	entry := func(tag, typ uint16, count, value uint32) {
		b.u16le(tag)
		b.u16le(typ)
		b.u32le(count)
		b.u32le(value)
	}
	const short, ascii, long, rational = 3, 2, 4, 5
	entry(256, long, 1, uint32(width))
	entry(257, long, 1, uint32(height))
	entry(258, short, 1, 8)
	entry(259, short, 1, 1) // no compression
	entry(262, short, 1, 1) // BlackIsZero
	if desc > 0 {
		entry(270, ascii, uint32(desc), uint32(descOff))
	}
	entry(273, long, 1, uint32(stripOff))
	entry(277, short, 1, 1)
	entry(278, long, 1, uint32(height))
	entry(279, long, 1, uint32(width*height))
	entry(282, rational, 1, uint32(resOff))
	entry(283, rational, 1, uint32(resOff+8))
	entry(296, short, 1, 2) // inch
	b.u32le(0)              // no next IFD
	for i := 0; i < 2; i++ {
		b.u32le(72)
		b.u32le(1)
	}
	if desc > 0 {
		b.do(func(w io.Writer) error { return Lorem(w, desc-1, true) })
		b.raw(0)
	}
	b.do(func(w io.Writer) error { return Repeat(w, 0xC0, width*height) })
	return b.err
}

// ---------------------------------------------------------------- ICO

// ICONDIR (6) + one ICONDIRENTRY (16) followed by a PNG image, which
// Windows Vista and later (and all common readers) accept as icon payload.
const icoHeader = 22

func writeICO(w io.Writer, size int64) error {
	pngSize := size - icoHeader
	width, _ := pngPlan(pngSize)
	b := &binw{w: w}
	b.u16le(0)
	b.u16le(1) // icon
	b.u16le(1) // one image
	b.raw(byte(width), 1, 0, 0)
	b.u16le(1) // colour planes
	b.u16le(8) // bits per pixel
	b.u32le(uint32(pngSize))
	b.u32le(icoHeader)
	b.do(func(w io.Writer) error { return writePNG(w, pngSize) })
	return b.err
}

// ---------------------------------------------------------------- WebP

// vp8lPixel is a lossless VP8L bitstream for a single 1x1 pixel.
var vp8lPixel = []byte{0x2f, 0x00, 0x00, 0x00, 0x10, 0x07, 0x10, 0x11, 0x11, 0x88, 0x88, 0xfe, 0x07}

const (
	webpSimple   = 12 + 8 + 14      // RIFF header + VP8L chunk (13 bytes + pad)
	webpExtended = webpSimple + 18  // + VP8X chunk
	webpPadded   = webpExtended + 8 // + header of a padding chunk
)

func writeWebP(w io.Writer, size int64) error {
	b := &binw{w: w}
	b.str("RIFF")
	b.u32le(uint32(size - 8))
	b.str("WEBP")
	if size > webpSimple {
		b.str("VP8X")
		b.u32le(10)
		b.raw(0x10, 0, 0, 0)    // flags: alpha (the VP8L stream declares alpha)
		b.raw(0, 0, 0, 0, 0, 0) // canvas width-1, height-1 (24 bit each)
	}
	b.str("VP8L")
	b.u32le(uint32(len(vp8lPixel)))
	b.raw(vp8lPixel...)
	b.raw(0) // RIFF pad byte
	if size >= webpPadded {
		// Unknown chunks after the image data are allowed and ignored by readers.
		d := size - webpPadded
		b.str("BBDX")
		b.u32le(uint32(d))
		b.do(func(w io.Writer) error { return Lorem(w, d, true) })
	}
	return b.err
}
