package selfupdate

import (
	"strconv"
	"strings"
)

// Newer reports whether version a is later than version b. Both are
// "MAJOR.MINOR.PATCH", optionally prefixed with "v"; anything else, such as
// a pre-release, is never newer.
func Newer(a, b string) bool {
	va, vb := parseVersion(a), parseVersion(b)
	if va == nil || vb == nil {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] > vb[i]
		}
	}
	return false
}

// parseVersion splits "1.2.3" or "v1.2.3" into its numbers; nil if it is
// not such a version.
func parseVersion(v string) []int {
	parts := strings.Split(strings.TrimPrefix(v, "v"), ".")
	if len(parts) != 3 {
		return nil
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil
		}
		nums[i] = n
	}
	return nums
}
