package model

import (
	"encoding/binary"
	"io"
)

// binw writes binary fields and remembers the first error.
type binw struct {
	w   io.Writer
	err error
}

func (b *binw) raw(p ...byte) {
	if b.err == nil {
		_, b.err = b.w.Write(p)
	}
}
func (b *binw) str(s string) { b.raw([]byte(s)...) }
func (b *binw) u16le(v uint16) {
	b.raw(byte(v), byte(v>>8))
}
func (b *binw) u32le(v uint32) {
	b.raw(binary.LittleEndian.AppendUint32(nil, v)...)
}
func (b *binw) u16be(v uint16) { b.raw(byte(v>>8), byte(v)) }
func (b *binw) u32be(v uint32) { b.raw(binary.BigEndian.AppendUint32(nil, v)...) }
func (b *binw) do(f func(io.Writer) error) {
	if b.err == nil {
		b.err = f(b.w)
	}
}
