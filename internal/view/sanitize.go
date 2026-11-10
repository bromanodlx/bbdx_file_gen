package view

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// sanitize makes text from outside the program (file and directory names,
// the configuration file, error messages, release versions) safe to print:
// every control character, including ESC, CR and the C1 controls such as CSI,
// and every
// byte that is not valid UTF-8 (a raw 0x9B is CSI on 8-bit terminals) is
// replaced by "?", so the text can neither inject terminal escape sequences
// (retitle the window, clear or overwrite the screen) nor break the layout.
func sanitize(s string) string {
	if utf8.ValidString(s) && strings.IndexFunc(s, unicode.IsControl) < 0 {
		return s
	}
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '?'
		}
		return r
	}, strings.ToValidUTF8(s, "?"))
}

// sanitizeLines is sanitize for multi-line text; the line breaks are kept.
func sanitizeLines(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = sanitize(l)
	}
	return strings.Join(lines, "\n")
}

// sanitized returns a copy of s whose text fields are safe to print. Input is
// left alone: it is rendered by the text input component, which already drops
// control characters, and carries the cursor's styling.
func (s State) sanitized() State {
	for _, f := range []*string{
		&s.Name, &s.Ext, &s.FormatName, &s.TargetPath, &s.MinSize, &s.SizeNote,
		&s.CurrentDir, &s.DownloadDir, &s.SizeLabel, &s.Spinner, &s.Error, &s.Hint,
		&s.UpdateVersion, &s.InstalledVersion,
	} {
		*f = sanitize(*f)
	}
	s.DownloadReason = sanitizeLines(s.DownloadReason)
	if len(s.Suggestions) > 0 {
		sugg := make([]Suggestion, len(s.Suggestions))
		for i, sg := range s.Suggestions {
			sg.Ext, sg.Name, sg.Category, sg.MinSize = sanitize(sg.Ext), sanitize(sg.Name), sanitize(sg.Category), sanitize(sg.MinSize)
			sugg[i] = sg
		}
		s.Suggestions = sugg
	}
	return s
}
