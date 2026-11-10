package model

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func init() {
	registerValidator("pdf", validatePDF)
	registerValidator("docx", validateOOXML("word/document.xml"))
	registerValidator("xlsx", validateOOXML("xl/workbook.xml", "xl/worksheets/sheet1.xml"))
	registerValidator("pptx", validateOOXML("ppt/presentation.xml", "ppt/slides/slide1.xml",
		"ppt/slideMasters/slideMaster1.xml", "ppt/slideLayouts/slideLayout1.xml", "ppt/theme/theme1.xml"))
	registerValidator("odt", validateODF("application/vnd.oasis.opendocument.text"))
	registerValidator("ods", validateODF("application/vnd.oasis.opendocument.spreadsheet"))
	registerValidator("odp", validateODF("application/vnd.oasis.opendocument.presentation"))
	registerValidator("epub", validateEPUB)
}

var (
	docPDFObjRe       = regexp.MustCompile(`^(\d+) 0 obj`)
	docPDFStartxrefRe = regexp.MustCompile(`startxref\r?\n(\d+)\r?\n%%EOF\r?\n$`)
)

func validatePDF(t *testing.T, data []byte) {
	t.Helper()
	if !bytes.HasPrefix(data, []byte("%PDF-1.")) {
		t.Fatal("missing PDF header")
	}
	m := docPDFStartxrefRe.FindSubmatch(data)
	if m == nil {
		t.Fatal("missing startxref / EOF trailer")
	}
	off, _ := strconv.Atoi(string(m[1]))
	if off >= len(data) || !bytes.HasPrefix(data[off:], []byte("xref\n0 ")) {
		t.Fatalf("startxref %d does not point at an xref table", off)
	}
	lines := strings.Split(string(data[off:]), "\n")
	count, err := strconv.Atoi(strings.Fields(lines[1])[1])
	if err != nil || count < 2 {
		t.Fatalf("bad xref subsection header %q", lines[1])
	}
	for i := 0; i < count; i++ {
		e := lines[2+i] + "\n"
		if len(e) != 20 {
			t.Fatalf("xref entry %d is %d bytes, want 20", i, len(e))
		}
		if i == 0 {
			if e != "0000000000 65535 f \n" {
				t.Fatalf("bad free entry %q", e)
			}
			continue
		}
		o, _ := strconv.Atoi(e[:10])
		mm := docPDFObjRe.FindSubmatch(data[o:])
		if mm == nil || string(mm[1]) != strconv.Itoa(i) {
			t.Fatalf("xref entry %d (offset %d) does not point at object %d", i, o, i)
		}
	}
	if !bytes.Contains(data[off:], []byte("trailer\n<< /Size "+strconv.Itoa(count)+" /Root 1 0 R >>")) {
		t.Fatal("trailer missing or /Size mismatch")
	}
}

// docOpenZip opens data as a zip archive and reads every member (verifying CRCs).
func docOpenZip(t *testing.T, data []byte) (*zip.Reader, map[string][]byte) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("not a zip: %v", err)
	}
	parts := map[string][]byte{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			t.Fatalf("%s: %v", f.Name, err)
		}
		parts[f.Name] = b
		if strings.HasSuffix(f.Name, ".xml") || strings.HasSuffix(f.Name, ".rels") ||
			strings.HasSuffix(f.Name, ".xhtml") || strings.HasSuffix(f.Name, ".opf") {
			docCheckXML(t, f.Name, b)
		}
	}
	return zr, parts
}

func docCheckXML(t *testing.T, name string, b []byte) {
	t.Helper()
	d := xml.NewDecoder(bytes.NewReader(b))
	for {
		_, err := d.Token()
		if err == io.EOF {
			return
		}
		if err != nil {
			t.Fatalf("%s is not well-formed XML: %v", name, err)
		}
	}
}

func validateOOXML(required ...string) validator {
	return func(t *testing.T, data []byte) {
		t.Helper()
		_, parts := docOpenZip(t, data)
		for _, r := range append([]string{"[Content_Types].xml", "_rels/.rels"}, required...) {
			if _, ok := parts[r]; !ok {
				t.Fatalf("missing part %s", r)
			}
		}
		var ct struct {
			Defaults []struct {
				Extension string `xml:"Extension,attr"`
			} `xml:"Default"`
			Overrides []struct {
				PartName string `xml:"PartName,attr"`
			} `xml:"Override"`
		}
		if err := xml.Unmarshal(parts["[Content_Types].xml"], &ct); err != nil {
			t.Fatal(err)
		}
		covered := map[string]bool{}
		for _, d := range ct.Defaults {
			covered["ext:"+d.Extension] = true
		}
		for _, o := range ct.Overrides {
			name := strings.TrimPrefix(o.PartName, "/")
			if _, ok := parts[name]; !ok {
				t.Fatalf("content type override for missing part %s", name)
			}
			covered[name] = true
		}
		for name := range parts {
			if name == "[Content_Types].xml" {
				continue
			}
			if !covered[name] && !covered["ext:"+strings.TrimPrefix(path.Ext(name), ".")] {
				t.Fatalf("part %s has no content type", name)
			}
		}
		// every relationship target must exist
		for name, b := range parts {
			if !strings.HasSuffix(name, ".rels") {
				continue
			}
			var rels struct {
				Rels []struct {
					Target string `xml:"Target,attr"`
				} `xml:"Relationship"`
			}
			if err := xml.Unmarshal(b, &rels); err != nil {
				t.Fatal(err)
			}
			src := path.Dir(path.Dir(name)) // "word/_rels/x.rels" -> "word"
			for _, r := range rels.Rels {
				target := path.Clean(path.Join(src, r.Target))
				if _, ok := parts[target]; !ok {
					t.Fatalf("%s: relationship target %s missing", name, target)
				}
			}
		}
	}
}

func docCheckMimetype(t *testing.T, zr *zip.Reader, parts map[string][]byte, mime string) {
	t.Helper()
	first := zr.File[0]
	if first.Name != "mimetype" || first.Method != zip.Store || len(first.Extra) != 0 {
		t.Fatalf("first member must be an uncompressed mimetype without extra field, got %s", first.Name)
	}
	if string(parts["mimetype"]) != mime {
		t.Fatalf("mimetype = %q, want %q", parts["mimetype"], mime)
	}
}

func validateODF(mime string) validator {
	return func(t *testing.T, data []byte) {
		t.Helper()
		zr, parts := docOpenZip(t, data)
		docCheckMimetype(t, zr, parts, mime)
		var mf struct {
			Entries []struct {
				Path string `xml:"full-path,attr"`
				Type string `xml:"media-type,attr"`
			} `xml:"file-entry"`
		}
		if err := xml.Unmarshal(parts["META-INF/manifest.xml"], &mf); err != nil {
			t.Fatalf("manifest: %v", err)
		}
		listed := map[string]bool{}
		for _, e := range mf.Entries {
			listed[e.Path] = true
			if e.Path == "/" {
				if e.Type != mime {
					t.Fatalf("manifest root media type %q", e.Type)
				}
				continue
			}
			if _, ok := parts[e.Path]; !ok {
				t.Fatalf("manifest lists missing %s", e.Path)
			}
		}
		if !listed["/"] || !listed["content.xml"] {
			t.Fatal("manifest must list / and content.xml")
		}
		for name := range parts {
			if name != "mimetype" && name != "META-INF/manifest.xml" && !listed[name] {
				t.Fatalf("%s not listed in manifest", name)
			}
		}
	}
}

func validateEPUB(t *testing.T, data []byte) {
	t.Helper()
	zr, parts := docOpenZip(t, data)
	docCheckMimetype(t, zr, parts, "application/epub+zip")
	var c struct {
		Rootfiles []struct {
			Path string `xml:"full-path,attr"`
		} `xml:"rootfiles>rootfile"`
	}
	if err := xml.Unmarshal(parts["META-INF/container.xml"], &c); err != nil || len(c.Rootfiles) != 1 {
		t.Fatalf("container.xml: %v", err)
	}
	opfPath := c.Rootfiles[0].Path
	var opf struct {
		Version string `xml:"version,attr"`
		Items   []struct {
			ID         string `xml:"id,attr"`
			Href       string `xml:"href,attr"`
			Properties string `xml:"properties,attr"`
		} `xml:"manifest>item"`
		Spine []struct {
			IDRef string `xml:"idref,attr"`
		} `xml:"spine>itemref"`
		Identifier string `xml:"metadata>identifier"`
		Title      string `xml:"metadata>title"`
		Language   string `xml:"metadata>language"`
	}
	if err := xml.Unmarshal(parts[opfPath], &opf); err != nil {
		t.Fatalf("opf: %v", err)
	}
	if opf.Version != "3.0" || opf.Identifier == "" || opf.Title == "" || opf.Language == "" {
		t.Fatalf("opf metadata incomplete: %+v", opf)
	}
	ids, nav := map[string]bool{}, false
	for _, it := range opf.Items {
		ids[it.ID] = true
		nav = nav || it.Properties == "nav"
		if _, ok := parts[path.Join(path.Dir(opfPath), it.Href)]; !ok {
			t.Fatalf("manifest item %s missing", it.Href)
		}
	}
	if !nav || len(opf.Spine) == 0 {
		t.Fatal("EPUB 3 needs a nav document and a spine")
	}
	for _, s := range opf.Spine {
		if !ids[s.IDRef] {
			t.Fatalf("spine references unknown item %s", s.IDRef)
		}
	}
}

// --- tests specific to the document family ---

func TestDocumentFormatsMetadata(t *testing.T) {
	want := map[string]string{
		"pdf":  "application/pdf",
		"docx": "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"xlsx": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		"pptx": "application/vnd.openxmlformats-officedocument.presentationml.presentation",
		"odt":  "application/vnd.oasis.opendocument.text",
		"ods":  "application/vnd.oasis.opendocument.spreadsheet",
		"odp":  "application/vnd.oasis.opendocument.presentation",
		"epub": "application/epub+zip",
	}
	for ext, mime := range want {
		f, ok := Default().Lookup(ext)
		if !ok {
			t.Errorf("%s not registered", ext)
			continue
		}
		if f.MIME != mime || f.Category != CategoryDocument {
			t.Errorf("%s: MIME %q category %q", ext, f.MIME, f.Category)
		}
	}
}

func TestPDFSizesAcrossDigitBoundaries(t *testing.T) {
	f, _ := Default().Lookup("pdf")
	// startxref grows a digit around these offsets; every size must still be exact.
	for _, base := range []int64{1000, 10000, 100000} {
		for s := base - 40; s <= base+40; s++ {
			if s < f.Rule.Min {
				continue
			}
			data := generate(t, "pdf", s)
			if int64(len(data)) != s {
				t.Fatalf("size %d: got %d", s, len(data))
			}
			validatePDF(t, data)
		}
	}
}

func TestXLSXRowsSplitAcrossCellLimit(t *testing.T) {
	data := generate(t, "xlsx", 200_000)
	_, parts := docOpenZip(t, data)
	sheet := string(parts["xl/worksheets/sheet1.xml"])
	if n := strings.Count(sheet, "<row "); n < 7 {
		t.Fatalf("expected filler split over several rows, got %d", n)
	}
	for _, m := range regexp.MustCompile(`<t xml:space="preserve">([^<]*)</t>`).FindAllStringSubmatch(sheet, -1) {
		if len(m[1]) > 32767 {
			t.Fatalf("cell text of %d chars exceeds the Excel limit", len(m[1]))
		}
	}
}

func TestDocumentsPdfinfo(t *testing.T) {
	f, _ := Default().Lookup("pdf")
	for _, s := range []int64{f.Rule.Min, f.Rule.Min + 1, 5000, 200_000} {
		out := runTool(t, "pdfinfo", writeTemp(t, "pdf", generate(t, "pdf", s)))
		if !regexp.MustCompile(`Pages:\s+1\n`).MatchString(out) {
			t.Fatalf("pdfinfo for %d bytes:\n%s", s, out)
		}
	}
}

func TestDocumentsUnzip(t *testing.T) {
	for _, ext := range []string{"docx", "xlsx", "pptx", "odt", "ods", "odp", "epub"} {
		f, _ := Default().Lookup(ext)
		for _, s := range []int64{f.Rule.Min, f.Rule.Min + 777, 100_000} {
			runTool(t, "unzip", "-tq", writeTemp(t, ext, generate(t, ext, s)))
		}
	}
}

func TestDocumentsXmllint(t *testing.T) {
	for _, ext := range []string{"docx", "xlsx", "pptx", "odt", "ods", "odp", "epub"} {
		_, parts := docOpenZip(t, generate(t, ext, 20_000))
		for name, b := range parts {
			if name == "mimetype" {
				continue
			}
			runTool(t, "xmllint", "--noout", writeTemp(t, "xml", b))
		}
	}
}

// TestDocumentsLibreOfficeRoundTrip converts every office document to PDF.
func TestDocumentsLibreOfficeRoundTrip(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: runs LibreOffice")
	}
	if _, err := exec.LookPath("libreoffice"); err != nil {
		t.Skip("libreoffice not installed")
	}
	for _, ext := range []string{"docx", "xlsx", "pptx", "odt", "ods", "odp"} {
		t.Run(ext, func(t *testing.T) {
			f, _ := Default().Lookup(ext)
			for _, s := range []int64{f.Rule.Min, 50_000} {
				src := writeTemp(t, ext, generate(t, ext, s))
				outDir, profile := t.TempDir(), t.TempDir()
				runTool(t, "libreoffice", "-env:UserInstallation=file://"+profile,
					"--headless", "--convert-to", "pdf", "--outdir", outDir, src)
				pdf := filepath.Join(outDir, "sample.pdf")
				if _, err := os.Stat(pdf); err != nil {
					t.Fatalf("LibreOffice did not convert %s (%d bytes)", ext, s)
				}
				runTool(t, "pdfinfo", pdf)
				if s > f.Rule.Min {
					if _, err := exec.LookPath("pdftotext"); err == nil {
						text, _ := exec.Command("pdftotext", pdf, "-").Output()
						if !bytes.Contains(bytes.ToLower(text), []byte("lorem")) {
							t.Errorf("%s: filler text not rendered", ext)
						}
					}
				}
			}
		})
	}
}
