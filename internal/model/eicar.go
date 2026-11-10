package model

import (
	"path/filepath"
	"strings"
)

// EICAR is the EICAR Standard Anti-Malware Test File: a harmless, published
// 68-byte printable-ASCII string that anti-virus products recognize, so it can
// be used to check that scanners and file pipelines react without ever handling
// real malware. See https://www.eicar.org/download-anti-malware-testfile/.
const EICAR = `X5O!P%@AP[4\PZX54(P^)7CC)7}$EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*`

// IsEICARName reports whether name (ignoring directory, extension and case)
// is "eicar". Such a file is filled with the EICAR test string instead of the
// format's normal content.
func IsEICARName(name string) bool {
	base := filepath.Base(strings.TrimSpace(name))
	base = strings.TrimSuffix(base, filepath.Ext(base))
	return strings.EqualFold(base, "eicar")
}
