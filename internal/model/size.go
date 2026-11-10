package model

import (
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// MaxSize is the largest file the generator will produce (1 GiB). It keeps
// every format clear of 32-bit length fields and of zip64 territory.
const MaxSize int64 = 1 << 30

var sizePattern = regexp.MustCompile(`^(\d{1,19})(?:\.(\d{1,9}))?\s*([A-Za-z]*)$`)

var unitMultipliers = map[string]int64{
	"": 1, "b": 1,
	"k": 1 << 10, "kib": 1 << 10, "kb": 1_000,
	"m": 1 << 20, "mib": 1 << 20, "mb": 1_000_000,
	"g": 1 << 30, "gib": 1 << 30, "gb": 1_000_000_000,
}

// ParseSize parses a byte count such as "1024", "10KB" (SI, 1000) or
// "1.5 MiB" / "2M" (IEC, 1024). Fractions are allowed as long as they
// resolve to a whole number of bytes.
func ParseSize(s string) (int64, error) {
	m := sizePattern.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, fmt.Errorf("invalid size %q (examples: 2048, 64KiB, 1.5MB)", s)
	}
	mult, ok := unitMultipliers[strings.ToLower(m[3])]
	if !ok {
		return 0, fmt.Errorf("unknown size unit %q (use B, KB, KiB, MB, MiB, GB, GiB)", m[3])
	}
	num, ok := new(big.Int).SetString(m[1]+m[2], 10)
	if !ok {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	den := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(len(m[2]))), nil)
	num.Mul(num, big.NewInt(mult))
	q, r := new(big.Int).QuoRem(num, den, new(big.Int))
	if r.Sign() != 0 {
		return 0, fmt.Errorf("size %q is not a whole number of bytes", s)
	}
	if !q.IsInt64() {
		return 0, fmt.Errorf("size %q is too large", s)
	}
	return q.Int64(), nil
}

// HumanSize renders n with binary units, e.g. 1536 -> "1.5 KiB".
func HumanSize(n int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB"}
	v := float64(n)
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%d B", n)
	}
	s := strconv.FormatFloat(v, 'f', 2, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	return s + " " + units[i]
}

// Range is an inclusive interval of byte sizes.
type Range struct{ Lo, Hi int64 }

// SizeRule describes which exact file sizes a format can be produced in.
type SizeRule struct {
	// Min is the smallest standard-compliant file; used when no size is given.
	Min int64
	// Align, when > 1, requires sizes to be a multiple of it (e.g. RIFF: 2, tar: 512).
	Align int64
	// Gaps lists sizes above Min that the format's padding mechanism cannot reach.
	Gaps []Range
	// Note explains alignment or gaps to the user.
	Note string
}

func (r SizeRule) inGap(size int64) (Range, bool) {
	for _, g := range r.Gaps {
		if size >= g.Lo && size <= g.Hi {
			return g, true
		}
	}
	return Range{}, false
}

func (r SizeRule) align() int64 {
	if r.Align > 1 {
		return r.Align
	}
	return 1
}

func (r SizeRule) valid(size int64) bool {
	_, gap := r.inGap(size)
	return size >= r.Min && size <= MaxSize && size%r.align() == 0 && !gap
}

// below returns the largest valid size < size, or -1.
func (r SizeRule) below(size int64) int64 {
	a := r.align()
	s := size - 1
	if s > MaxSize {
		s = MaxSize
	}
	for s >= r.Min {
		s -= s % a
		if g, ok := r.inGap(s); ok {
			s = g.Lo - 1
			continue
		}
		if s >= r.Min {
			return s
		}
	}
	return -1
}

// above returns the smallest valid size > size, or -1.
func (r SizeRule) above(size int64) int64 {
	a := r.align()
	s := size + 1
	if s < r.Min {
		s = r.Min
	}
	for s <= MaxSize {
		if rem := s % a; rem != 0 {
			s += a - rem
			continue
		}
		if g, ok := r.inGap(s); ok {
			s = g.Hi + 1
			continue
		}
		return s
	}
	return -1
}

// Check returns nil when a file of exactly size bytes can be produced,
// otherwise a *SizeError carrying the nearest valid sizes.
func (r SizeRule) Check(size int64) error {
	if r.valid(size) {
		return nil
	}
	se := &SizeError{Requested: size, Below: r.below(size), Above: r.above(size)}
	switch {
	case size < r.Min:
		se.Reason = fmt.Sprintf("the minimum standard-compliant size is %d bytes", r.Min)
	case size > MaxSize:
		se.Reason = fmt.Sprintf("the maximum supported size is %s", HumanSize(MaxSize))
	default:
		se.Reason = r.Note
	}
	return se
}

// SizeError reports a size that a format cannot be produced in.
type SizeError struct {
	Ext       string
	Requested int64
	Reason    string
	// Below and Above are the nearest valid sizes, -1 if none exists.
	Below, Above int64
}

func (e *SizeError) Error() string {
	what := "the file"
	if e.Ext != "" {
		what = "a ." + e.Ext + " file"
	}
	msg := fmt.Sprintf("%s cannot be exactly %d bytes", what, e.Requested)
	if e.Reason != "" {
		msg += ": " + e.Reason
	}
	var sugg []string
	for _, s := range []int64{e.Below, e.Above} {
		if s >= 0 {
			sugg = append(sugg, strconv.FormatInt(s, 10))
		}
	}
	if len(sugg) > 0 {
		msg += " (nearest valid: " + strings.Join(sugg, " or ") + " bytes)"
	}
	return msg
}

// Suggestion is the nearest valid size to offer instead of the requested one:
// the next larger size, else the next smaller one, else -1.
func (e *SizeError) Suggestion() int64 {
	if e.Above >= 0 {
		return e.Above
	}
	return e.Below
}
