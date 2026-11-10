package model

import (
	"crypto/md5"
	"encoding/binary"
	"io"
)

func audioFormats() []Format {
	return []Format{
		{
			Ext: "wav", Name: "Waveform Audio (PCM)", MIME: "audio/wav", Category: CategoryAudio,
			Rule:  SizeRule{Min: avWAVHeader, Align: 2, Note: "RIFF files always have an even number of bytes"},
			Write: writeWAV,
		},
		{
			Ext: "mp3", Name: "MPEG-1 Audio Layer III", MIME: "audio/mpeg", Category: CategoryAudio,
			Rule: SizeRule{Min: avMP3Min, Gaps: []Range{{avMP3Min + avMP3MinFrames + 1, avMP3Min + avID3Min - 1}},
				Note: "MP3 consists of 96-byte frames; other sizes need an ID3v2 tag of at least 21 bytes"},
			Write: writeMP3,
		},
		{
			Ext: "flac", Name: "Free Lossless Audio Codec", MIME: "audio/flac", Category: CategoryAudio,
			Rule: SizeRule{Min: avFLACMin, Gaps: []Range{{avFLACMin + 1, avFLACMin + 3}},
				Note: "a FLAC PADDING block occupies at least 4 bytes"},
			Write: writeFLAC,
		},
		{
			Ext: "mid", Aliases: []string{"midi"}, Name: "Standard MIDI File", MIME: "audio/midi", Category: CategoryAudio,
			Rule: SizeRule{Min: avMIDIMin, Gaps: []Range{{avMIDIMin + 1, avMIDIMin + 3}},
				Note: "a MIDI text event occupies at least 4 bytes"},
			Write: writeMIDI,
		},
	}
}

func avLE16(v int) []byte { return binary.LittleEndian.AppendUint16(nil, uint16(v)) }
func avLE32(v int) []byte { return binary.LittleEndian.AppendUint32(nil, uint32(v)) }
func avBE32(v int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(v)) }

func avWrite(w io.Writer, parts ...[]byte) error {
	for _, p := range parts {
		if _, err := w.Write(p); err != nil {
			return err
		}
	}
	return nil
}

// --- WAV: RIFF/WAVE, PCM 8-bit mono 8 kHz, silence ---

const avWAVHeader = 44

func writeWAV(w io.Writer, size int64) error {
	n := int(size - avWAVHeader) // even, as size is even
	err := avWrite(w,
		[]byte("RIFF"), avLE32(int(size-8)), []byte("WAVE"),
		[]byte("fmt "), avLE32(16),
		avLE16(1), avLE16(1), avLE32(8000), avLE32(8000), avLE16(1), avLE16(8),
		[]byte("data"), avLE32(n))
	if err != nil {
		return err
	}
	return Repeat(w, 0x80, int64(n)) // unsigned 8-bit PCM silence
}

// --- MP3: silent MPEG-1 Layer III frames, 32 kbit/s, 48 kHz, mono ---

const (
	avMP3Frame = 96 // 144 * 32000 / 48000
	// Two frames: decoders such as FFmpeg only lock on after two consecutive frames.
	avMP3MinFrames = 2
	avMP3Min       = avMP3MinFrames * avMP3Frame
	avID3Min       = 21 // ID3v2 header + one frame header + 1 byte of frame data
)

func avMP3FrameBytes(padded bool) []byte {
	f := make([]byte, avMP3Frame, avMP3Frame+1)
	f[0], f[1], f[2], f[3] = 0xFF, 0xFB, 0x14, 0xC0 // sync, MPEG-1 L3 no CRC, 32k/48k, mono
	if padded {
		f[2] |= 0x02
		f = f[:avMP3Frame+1]
	}
	return f // zero side information: no main data, decodes to silence
}

func avSyncsafe(v int) []byte {
	return []byte{byte(v >> 21 & 0x7F), byte(v >> 14 & 0x7F), byte(v >> 7 & 0x7F), byte(v & 0x7F)}
}

// writeID3 writes an ID3v2.4 tag of exactly n (>= 21) bytes.
func writeID3(w io.Writer, n int64) error {
	text := []byte{0x03} // UTF-8 encoding byte
	if sig := "bbdx_file_gen"; n-avID3Min >= int64(len(sig)) {
		text = append(text, sig...)
	}
	err := avWrite(w, []byte("ID3"), []byte{4, 0, 0}, avSyncsafe(int(n-10)),
		[]byte("TSSE"), avSyncsafe(len(text)), []byte{0, 0}, text)
	if err != nil {
		return err
	}
	return Zeros(w, n-20-int64(len(text)))
}

func writeMP3(w io.Writer, size int64) error {
	if size <= avMP3Min+avMP3MinFrames {
		// Fine-tune with the padding bit, which adds one byte per frame.
		for i := int64(0); i < avMP3MinFrames; i++ {
			if _, err := w.Write(avMP3FrameBytes(i < size-avMP3Min)); err != nil {
				return err
			}
		}
		return nil
	}
	frames := (size - avID3Min) / avMP3Frame
	if err := writeID3(w, size-frames*avMP3Frame); err != nil {
		return err
	}
	frame := avMP3FrameBytes(false)
	for i := int64(0); i < frames; i++ {
		if _, err := w.Write(frame); err != nil {
			return err
		}
	}
	return nil
}

// --- FLAC: STREAMINFO + one CONSTANT frame of 192 silent 16-bit samples ---

const (
	avFLACSamples   = 192
	avFLACFrameSize = 11
	avFLACMin       = 4 + 4 + 34 + avFLACFrameSize
	avFLACMaxBlock  = 4 + 1<<24 - 1
)

func avCRC(data []byte, width uint, poly uint32) uint32 {
	var crc uint32
	top, mask := uint32(1)<<(width-1), uint32(1)<<width-1
	for _, d := range data {
		crc ^= uint32(d) << (width - 8)
		for i := 0; i < 8; i++ {
			if crc&top != 0 {
				crc = crc<<1 ^ poly
			} else {
				crc <<= 1
			}
			crc &= mask
		}
	}
	return crc
}

func avFLACFrame() []byte {
	// sync+fixed blocking, 192 samples | 44.1 kHz, mono | 16 bit, frame number 0
	f := []byte{0xFF, 0xF8, 0x19, 0x08, 0x00}
	f = append(f, byte(avCRC(f, 8, 0x07)))
	f = append(f, 0x00, 0x00, 0x00) // CONSTANT subframe, value 0
	return binary.BigEndian.AppendUint16(f, uint16(avCRC(f, 16, 0x8005)))
}

func avFLACStreamInfo(last bool) []byte {
	b := []byte{0, 0, 0, 34}
	if last {
		b[0] = 0x80
	}
	b = binary.BigEndian.AppendUint16(b, avFLACSamples)
	b = binary.BigEndian.AppendUint16(b, avFLACSamples)
	b = append(b, 0, 0, avFLACFrameSize, 0, 0, avFLACFrameSize)
	// sample rate (20) | channels-1 (3) | bits-1 (5) | total samples (36)
	b = binary.BigEndian.AppendUint64(b, 44100<<44|0<<41|15<<36|avFLACSamples)
	sum := md5.Sum(make([]byte, avFLACSamples*2))
	return append(b, sum[:]...)
}

func writeFLAC(w io.Writer, size int64) error {
	pad := size - avFLACMin
	if err := avWrite(w, []byte("fLaC"), avFLACStreamInfo(pad == 0)); err != nil {
		return err
	}
	for pad > 0 {
		blk := pad
		if blk > avFLACMaxBlock {
			blk = min(avFLACMaxBlock, pad-4) // leave room for one more block header
		}
		pad -= blk
		hdr := avBE32(int(blk - 4))
		hdr[0] = 0x01 // PADDING
		if pad == 0 {
			hdr[0] |= 0x80
		}
		if err := avWrite(w, hdr); err != nil {
			return err
		}
		if err := Zeros(w, blk-4); err != nil {
			return err
		}
	}
	return avWrite(w, avFLACFrame())
}

// --- MIDI: format 0, one track holding text events and end-of-track ---

const (
	avMIDIMin      = 14 + 8 + 4
	avMIDIMaxEvent = 4 + 127 // delta, FF 01, 1-byte length, text
)

func writeMIDI(w io.Writer, size int64) error {
	err := avWrite(w, []byte("MThd"), avBE32(6), []byte{0, 0, 0, 1, 0, 96},
		[]byte("MTrk"), avBE32(int(size-22)))
	if err != nil {
		return err
	}
	for rem := size - avMIDIMin; rem > 0; {
		ev := rem
		if ev > avMIDIMaxEvent {
			ev = avMIDIMaxEvent
			if rem-ev < 4 {
				ev = rem - 4
			}
		}
		rem -= ev
		if err := avWrite(w, []byte{0x00, 0xFF, 0x01, byte(ev - 4)}); err != nil {
			return err
		}
		if err := Lorem(w, ev-4, false); err != nil {
			return err
		}
	}
	return avWrite(w, []byte{0x00, 0xFF, 0x2F, 0x00})
}
