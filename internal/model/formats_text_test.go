package model

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"testing"
)

var textExts = []string{
	"txt", "csv", "json", "xml", "md", "yaml", "ini", "log", "rtf", "html", "css",
	"js", "svg", "ics", "vcf", "py", "java", "c", "sh", "sql", "php",
}

func init() {
	registerValidator("txt", validatePrintable)
	registerValidator("csv", validateCSV)
	registerValidator("json", validateJSON)
	registerValidator("xml", validateXMLRoot("content"))
	registerValidator("svg", validateSVG)
	registerValidator("md", validateMarkdown)
	registerValidator("yaml", validateYAML)
	registerValidator("ini", validateINI)
	registerValidator("log", validateLog)
	registerValidator("rtf", validateRTF)
	registerValidator("html", validateHTML)
	registerValidator("css", validateCSS)
	registerValidator("ics", validateFolded("BEGIN:VCALENDAR", "END:VCALENDAR", "VERSION:2.0", "PRODID:", "BEGIN:VEVENT", "UID:", "DTSTAMP:", "DTSTART:"))
	registerValidator("vcf", validateFolded("BEGIN:VCARD", "END:VCARD", "VERSION:4.0", "FN:"))
	for _, ext := range []string{"js", "py", "java", "c", "sh", "sql", "php"} {
		registerValidator(ext, validatePrintable)
	}
}

func TestTextFormatsRegistered(t *testing.T) {
	for _, ext := range textExts {
		f, ok := Default().Lookup(ext)
		if !ok {
			t.Errorf("%s not registered", ext)
			continue
		}
		if f.Ext != ext {
			t.Errorf("%s resolved to %s", ext, f.Ext)
		}
	}
	for alias, ext := range map[string]string{"htm": "html", "yml": "yaml", "text": "txt", "vcard": "vcf", "mjs": "js", "markdown": "md"} {
		if f, ok := Default().Lookup(alias); !ok || f.Ext != ext {
			t.Errorf("alias %s should resolve to %s", alias, ext)
		}
	}
}

func TestTxtMinimumIsEmptyFile(t *testing.T) {
	if f, _ := Default().Lookup("txt"); f.Rule.Min != 0 {
		t.Errorf("an empty file is a valid text file, Min = %d", f.Rule.Min)
	}
}

func TestTextFormatsAreContiguous(t *testing.T) {
	for _, ext := range textExts {
		f, _ := Default().Lookup(ext)
		if f.Rule.Align > 1 || len(f.Rule.Gaps) > 0 {
			t.Errorf("%s: text formats must accept every size >= Min", ext)
		}
	}
}

func TestFoldedLinesRespectLimit(t *testing.T) {
	for _, ext := range []string{"ics", "vcf"} {
		data := generate(t, ext, 5000)
		for _, line := range strings.Split(string(data), "\r\n") {
			if len(line) > 75 {
				t.Fatalf("%s: line exceeds 75 octets: %q", ext, line)
			}
		}
	}
}

// --- external tools ---

func toolCheck(t *testing.T, ext string, sizes []int64, check func(path string)) {
	t.Helper()
	f, _ := Default().Lookup(ext)
	for _, s := range sizes {
		check(writeTemp(t, ext, generate(t, ext, f.Rule.Min+s)))
	}
}

var toolSizes = []int64{0, 1, 2, 63, 64, 65, 66, 67, 500, 70000}

func TestXMLLint(t *testing.T) {
	for _, ext := range []string{"xml", "svg"} {
		toolCheck(t, ext, toolSizes, func(p string) { runTool(t, "xmllint", "--noout", p) })
	}
	toolCheck(t, "html", toolSizes, func(p string) { runTool(t, "xmllint", "--html", "--noout", p) })
}

func TestPythonCompiles(t *testing.T) {
	toolCheck(t, "py", toolSizes, func(p string) { runTool(t, "python3", "-m", "py_compile", p) })
}

func TestPythonRunsAndPrints(t *testing.T) {
	p := writeTemp(t, "py", generate(t, "py", 3000))
	if out := runTool(t, "python3", p); !strings.Contains(out, "bbdx test file") {
		t.Errorf("unexpected output %q", out)
	}
}

func TestShellSyntax(t *testing.T) {
	toolCheck(t, "sh", toolSizes, func(p string) { runTool(t, "sh", "-n", p) })
	p := writeTemp(t, "sh", generate(t, "sh", 3000))
	if out := runTool(t, "sh", p); strings.TrimSpace(out) != "bbdx test file" {
		t.Errorf("unexpected output %q", out)
	}
}

func TestCCompiles(t *testing.T) {
	toolCheck(t, "c", toolSizes, func(p string) {
		runTool(t, "cc", "-std=c11", "-pedantic", "-Wall", "-Werror", "-fsyntax-only", p)
	})
}

func TestJavaScriptSyntax(t *testing.T) {
	toolCheck(t, "js", toolSizes, func(p string) { runTool(t, "node", "--check", p) })
}

func TestJavaCompiles(t *testing.T) {
	toolCheck(t, "java", toolSizes, func(p string) { runTool(t, "javac", "-d", t.TempDir(), p) })
}

func TestJavaRunsAndPrints(t *testing.T) {
	p := writeTemp(t, "java", generate(t, "java", 3000))
	if out := runTool(t, "java", p); strings.TrimSpace(out) != "bbdx test file" {
		t.Errorf("unexpected output %q", out)
	}
}

func TestPHPRunsAndPrints(t *testing.T) {
	p := writeTemp(t, "php", generate(t, "php", 3000))
	if out := runTool(t, "php", p); strings.TrimSpace(out) != "bbdx test file" {
		t.Errorf("unexpected output %q", out)
	}
}

func TestPHPSyntax(t *testing.T) {
	toolCheck(t, "php", toolSizes, func(p string) { runTool(t, "php", "-l", p) })
}

func TestSQLExecutesInSQLite(t *testing.T) {
	script := `import sqlite3,sys; sqlite3.connect(":memory:").executescript(open(sys.argv[1]).read())`
	toolCheck(t, "sql", toolSizes, func(p string) { runTool(t, "python3", "-c", script, p) })
}

func TestYAMLParsesWithPython(t *testing.T) {
	if exec.Command("python3", "-c", "import yaml").Run() != nil {
		t.Skip("PyYAML not installed")
	}
	script := `import yaml,sys; d=yaml.safe_load(open(sys.argv[1])); assert isinstance(d["content"], str)`
	toolCheck(t, "yaml", toolSizes, func(p string) { runTool(t, "python3", "-c", script, p) })
}

func TestINIParsesWithPython(t *testing.T) {
	script := `import configparser,sys; c=configparser.ConfigParser(); c.read(sys.argv[1]); assert c["bbdx"]["content"] is not None`
	toolCheck(t, "ini", toolSizes, func(p string) { runTool(t, "python3", "-c", script, p) })
}

func TestICalendarParsesWithPython(t *testing.T) {
	// Independent check of RFC 5545 unfolding with Python's universal-newline-free reader.
	script := `import sys,re; d=open(sys.argv[1],newline="").read(); u=re.sub("\r\n ","",d); assert u.endswith("END:VCALENDAR\r\n")`
	toolCheck(t, "ics", toolSizes, func(p string) { runTool(t, "python3", "-c", script, p) })
}

// --- structural validators ---

func validatePrintable(t *testing.T, data []byte) {
	for i, c := range data {
		if !(c == '\n' || c == '\r' || c == '\t' || (c >= 0x20 && c < 0x7f)) {
			t.Errorf("non-printable byte %#x at %d", c, i)
			return
		}
	}
}

func validateCSV(t *testing.T, data []byte) {
	validatePrintable(t, data)
	if !bytes.HasPrefix(data, []byte("id,text\r\n")) || !bytes.HasSuffix(data, []byte("\r\n")) {
		t.Error("csv must start with the header and end with CRLF")
	}
	if bytes.Count(data, []byte("\n")) != bytes.Count(data, []byte("\r\n")) {
		t.Error("csv records must be CRLF terminated (RFC 4180)")
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = 2
	recs, err := r.ReadAll()
	if err != nil {
		t.Errorf("csv: %v", err)
		return
	}
	if len(recs) < 2 {
		t.Errorf("csv needs a header and at least one record, got %d", len(recs))
	}
}

func validateJSON(t *testing.T, data []byte) {
	var v struct {
		Content *string `json:"content"`
	}
	if err := json.Unmarshal(data, &v); err != nil || v.Content == nil {
		t.Errorf("json: %v (content=%v)", err, v.Content)
	}
}

func checkWellFormed(t *testing.T, data []byte) (root xml.Name) {
	d := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := d.Token()
		if err == io.EOF {
			return root
		}
		if err != nil {
			t.Errorf("xml: %v", err)
			return root
		}
		if se, ok := tok.(xml.StartElement); ok && root.Local == "" {
			root = se.Name
		}
	}
}

func validateXMLRoot(name string) validator {
	return func(t *testing.T, data []byte) {
		if !bytes.HasPrefix(data, []byte(`<?xml version="1.0" encoding="UTF-8"?>`)) {
			t.Error("missing XML declaration")
		}
		if root := checkWellFormed(t, data); root.Local != name {
			t.Errorf("root element %q, want %q", root.Local, name)
		}
	}
}

func validateSVG(t *testing.T, data []byte) {
	if root := checkWellFormed(t, data); root.Local != "svg" || root.Space != "http://www.w3.org/2000/svg" {
		t.Errorf("root element %+v is not an SVG element", root)
	}
}

func validateMarkdown(t *testing.T, data []byte) {
	validatePrintable(t, data)
	if !bytes.HasPrefix(data, []byte("# ")) {
		t.Error("markdown must start with a heading")
	}
}

var yamlPattern = regexp.MustCompile(`\A---\ncontent: "[A-Za-z .]*"\n\z`)

func validateYAML(t *testing.T, data []byte) {
	if !yamlPattern.Match(data) {
		t.Errorf("unexpected yaml layout: %.80q", data)
	}
}

func validateINI(t *testing.T, data []byte) {
	validatePrintable(t, data)
	section := false
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		switch {
		case strings.HasPrefix(line, ";"):
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			section = true
		case section && strings.Contains(line, "="):
		default:
			t.Errorf("invalid ini line %.80q", line)
			return
		}
	}
}

var logLine = regexp.MustCompile(`^\d{4}-\d\d-\d\dT\d\d:\d\d:\d\dZ (INFO|DEBUG) \S.*$`)

func validateLog(t *testing.T, data []byte) {
	if !bytes.HasSuffix(data, []byte("\n")) {
		t.Error("log must end with a newline")
	}
	for _, line := range strings.Split(strings.TrimSuffix(string(data), "\n"), "\n") {
		if !logLine.MatchString(line) {
			t.Errorf("invalid log line %q", line)
			return
		}
	}
}

func validateRTF(t *testing.T, data []byte) {
	validatePrintable(t, data)
	if !bytes.HasPrefix(data, []byte(`{\rtf1`)) {
		t.Error(`rtf must start with {\rtf1`)
	}
	depth := 0
	for i, c := range data {
		switch c {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 && i != len(data)-1 && strings.TrimSpace(string(data[i+1:])) != "" {
				t.Error("content after the closing brace")
				return
			}
		}
		if depth < 0 {
			t.Error("unbalanced braces")
			return
		}
	}
	if depth != 0 {
		t.Error("unbalanced braces")
	}
}

func validateHTML(t *testing.T, data []byte) {
	validatePrintable(t, data)
	s := string(data)
	for _, want := range []string{"<!DOCTYPE html>", `<meta charset="utf-8">`, "<title>", "<body>", "</html>"} {
		if !strings.Contains(s, want) {
			t.Errorf("html lacks %s", want)
		}
	}
}

func validateCSS(t *testing.T, data []byte) {
	validatePrintable(t, data)
	s := string(data)
	if strings.Count(s, "/*") != strings.Count(s, "*/") || strings.Count(s, "{") != strings.Count(s, "}") {
		t.Error("unbalanced comments or braces")
	}
}

func validateFolded(begin, end string, required ...string) validator {
	return func(t *testing.T, data []byte) {
		validatePrintable(t, data)
		s := string(data)
		if bytes.Count(data, []byte("\n")) != bytes.Count(data, []byte("\r\n")) {
			t.Error("lines must be CRLF terminated")
		}
		if !strings.HasPrefix(s, begin+"\r\n") || !strings.HasSuffix(s, end+"\r\n") {
			t.Errorf("must be wrapped in %s/%s", begin, end)
		}
		for _, line := range strings.Split(s, "\r\n") {
			if len(line) > 75 {
				t.Errorf("line longer than 75 octets: %.80q", line)
				return
			}
		}
		unfolded := strings.ReplaceAll(s, "\r\n ", "")
		for _, req := range required {
			if !strings.Contains(unfolded, "\r\n"+req) && !strings.HasPrefix(unfolded, req) {
				t.Errorf("missing required property %s", req)
			}
		}
	}
}
