package model

import (
	"bytes"
	"io"
	"strings"
)

var loremWords = strings.Fields(`lorem ipsum dolor sit amet consectetur adipiscing elit sed do
eiusmod tempor incididunt ut labore et dolore magna aliqua ut enim ad minim veniam quis
nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat duis aute
irure dolor in reprehenderit in voluptate velit esse cillum dolore eu fugiat nulla
pariatur excepteur sint occaecat cupidatat non proident sunt in culpa qui officia
deserunt mollit anim id est laborum`)

const loremLineWidth = 80

// loremBlocks are built once; they repeat seamlessly.
var loremSingle, loremLines = buildLorem()

func buildLorem() ([]byte, []byte) {
	var single, lines bytes.Buffer
	line := 0
	for i := 0; single.Len() < 32<<10; i++ {
		w := loremWords[(i*7+i/len(loremWords))%len(loremWords)]
		if i%12 == 0 {
			w = strings.ToUpper(w[:1]) + w[1:]
		}
		if i%12 == 11 {
			w += "."
		}
		single.WriteString(w + " ")
		if line > 0 && line+1+len(w) > loremLineWidth {
			lines.WriteByte('\n')
			line = 0
		}
		if line > 0 {
			lines.WriteByte(' ')
			line++
		}
		lines.WriteString(w)
		line += len(w)
	}
	lines.WriteByte('\n')
	return single.Bytes(), lines.Bytes()
}

// Lorem writes exactly n bytes of readable placeholder text consisting of
// ASCII letters, spaces and periods. With newlines it is wrapped at 80 columns.
func Lorem(w io.Writer, n int64, newlines bool) error {
	block := loremSingle
	if newlines {
		block = loremLines
	}
	return writeCycle(w, block, 0, n)
}

// Repeat writes n copies of b.
func Repeat(w io.Writer, b byte, n int64) error {
	return writeCycle(w, bytes.Repeat([]byte{b}, 32<<10), 0, n)
}

// Zeros writes n zero bytes.
func Zeros(w io.Writer, n int64) error { return Repeat(w, 0, n) }

// writeCycle writes n bytes of block repeated endlessly, starting at byte off
// of that repetition.
func writeCycle(w io.Writer, block []byte, off, n int64) error {
	for n > 0 {
		chunk := block[off%int64(len(block)):]
		if int64(len(chunk)) > n {
			chunk = chunk[:n]
		}
		if _, err := w.Write(chunk); err != nil {
			return err
		}
		n -= int64(len(chunk))
		off += int64(len(chunk))
	}
	return nil
}

// loremAt writes n bytes of the wrapped lorem text starting at offset off,
// so that text split across several containers still reads continuously.
func loremAt(w io.Writer, off, n int64) error { return writeCycle(w, loremLines, off, n) }

// loremStream continues single-line Lorem text across several writes.
type loremStream struct{ pos int64 }

func (s *loremStream) write(w io.Writer, n int64) error {
	err := writeCycle(w, loremSingle, s.pos, n)
	s.pos += n
	return err
}

// CountingWriter counts the bytes written through it; W may be nil.
type CountingWriter struct {
	W io.Writer
	N int64
}

func (c *CountingWriter) Write(p []byte) (int, error) {
	if c.W == nil {
		c.N += int64(len(p))
		return len(p), nil
	}
	n, err := c.W.Write(p)
	c.N += int64(n)
	return n, err
}
