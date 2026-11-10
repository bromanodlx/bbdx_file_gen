package model

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"strconv"
	"strings"
)

func documentFormats() []Format {
	return []Format{
		pdfFormat(),
		zipDocFormat("docx", "Microsoft Word document (OOXML)",
			"application/vnd.openxmlformats-officedocument.wordprocessingml.document", docxPackage()),
		zipDocFormat("xlsx", "Microsoft Excel workbook (OOXML)",
			"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", xlsxPackage()),
		zipDocFormat("pptx", "Microsoft PowerPoint presentation (OOXML)",
			"application/vnd.openxmlformats-officedocument.presentationml.presentation", pptxPackage()),
		zipDocFormat("odt", "OpenDocument text", odfMime+"text", odtPackage()),
		zipDocFormat("ods", "OpenDocument spreadsheet", odfMime+"spreadsheet", odsPackage()),
		zipDocFormat("odp", "OpenDocument presentation", odfMime+"presentation", odpPackage()),
		zipDocFormat("epub", "EPUB 3 e-book", "application/epub+zip", epubPackage()),
	}
}

// ---------------------------------------------------------------- PDF

const (
	pdfHeader  = "%PDF-1.4\n%\xe2\xe3\xcf\xd3\n"
	pdfContent = "BT /F1 24 Tf 72 760 Td (bbdx test file) Tj ET\n"
)

var pdfObjects = []string{
	"<< /Type /Catalog /Pages 2 0 R >>",
	"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
	"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>",
	"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	fmt.Sprintf("<< /Length %d >>\nstream\n%sendstream", len(pdfContent), pdfContent),
}

// pdfBody returns the objects and xref table for a body starting at offset start.
func pdfBody(start int64) (body string, xrefOffset int64) {
	var b strings.Builder
	offsets := make([]int64, len(pdfObjects))
	for i, o := range pdfObjects {
		offsets[i] = start + int64(b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xrefOffset = start + int64(b.Len())
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(pdfObjects)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\n", len(pdfObjects)+1)
	return b.String(), xrefOffset
}

// pdfLayout finds the comment padding and startxref line ending that make the file exactly size bytes.
func pdfLayout(size int64) (pad int64, eol string, ok bool) {
	body, _ := pdfBody(0)
	for _, eol := range []string{"\n", "\r\n"} {
		fixed := int64(len(pdfHeader) + len(body) + len("startxref") + len(eol) + len("\n%%EOF\n"))
		for d := int64(1); d <= 10; d++ {
			pad := size - fixed - d
			if pad < 0 {
				continue
			}
			if int64(len(strconv.FormatInt(int64(len(pdfHeader))+pad+int64(len(body))-pdfXrefLen(), 10))) == d {
				return pad, eol, true
			}
		}
	}
	return 0, "", false
}

// pdfXrefLen is the length of the xref table and trailer (independent of offsets).
func pdfXrefLen() int64 {
	body, xref := pdfBody(0)
	return int64(len(body)) - xref
}

// writePDFComment writes n bytes of PDF comment lines ("%lorem...\n", at most 80 bytes each).
func writePDFComment(w io.Writer, n int64) error {
	if n == 1 {
		_, err := io.WriteString(w, "\n")
		return err
	}
	pos := 0
	for n > 0 {
		l := min(n, 80)
		if n-l == 1 {
			l--
		}
		line := make([]byte, 0, l)
		line = append(line, '%')
		for int64(len(line)) < l-1 {
			line = append(line, loremSingle[pos%len(loremSingle)])
			pos++
		}
		line = append(line, '\n')
		if _, err := w.Write(line); err != nil {
			return err
		}
		n -= l
	}
	return nil
}

func pdfFormat() Format {
	min := int64(-1)
	for s := int64(0); min < 0; s++ {
		if _, _, ok := pdfLayout(s); ok {
			min = s
		}
	}
	return Format{
		Ext: "pdf", Name: "Portable Document Format 1.4", MIME: "application/pdf",
		Category: CategoryDocument, Rule: SizeRule{Min: min},
		Write: func(w io.Writer, size int64) error {
			pad, eol, ok := pdfLayout(size)
			if !ok {
				return fmt.Errorf("no PDF layout for %d bytes", size)
			}
			if _, err := io.WriteString(w, pdfHeader); err != nil {
				return err
			}
			if err := writePDFComment(w, pad); err != nil {
				return err
			}
			body, xref := pdfBody(int64(len(pdfHeader)) + pad)
			_, err := fmt.Fprintf(w, "%sstartxref%s%d\n%%%%EOF\n", body, eol, xref)
			return err
		},
	}
}

// ---------------------------------------------------------------- stored ZIP packages

// docPart is one member of a zip based document. Either Data is static, or
// the part carries the filler text: Prefix, chunks of text, Suffix.
type docPart struct {
	Name   string
	Data   string
	Filled bool
	Prefix string
	Suffix string
	// Open returns the markup opening chunk i (1-based); Close ends a chunk.
	Open  func(i int) string
	Close string
	// MaxChunk caps the text per chunk (e.g. the 32767 char cell limit).
	MaxChunk int64
}

type docPackage []docPart

// layout splits budget bytes (chunk markup plus text) into k chunks.
func (p docPart) layout(budget int64) (k int, text int64, ok bool) {
	var overhead int64
	for k = 1; ; k++ {
		overhead += int64(len(p.Open(k)) + len(p.Close))
		text = budget - overhead
		if text < 0 {
			return 0, 0, false
		}
		if text <= p.MaxChunk*int64(k) {
			return k, text, true
		}
	}
}

func (p docPart) writeFilled(w io.Writer, k int, text int64) error {
	if _, err := io.WriteString(w, p.Prefix); err != nil {
		return err
	}
	for i := 1; i <= k; i++ {
		n := text / int64(k)
		if int64(i) <= text%int64(k) {
			n++
		}
		if _, err := io.WriteString(w, p.Open(i)); err != nil {
			return err
		}
		if err := Lorem(w, n, false); err != nil {
			return err
		}
		if _, err := io.WriteString(w, p.Close); err != nil {
			return err
		}
	}
	_, err := io.WriteString(w, p.Suffix)
	return err
}

const (
	zipLocalHeader   = 30
	zipCentralHeader = 46
	zipEOCD          = 22
	// 2026-01-01 00:00:00 in MS-DOS format.
	zipDosDate = (2026-1980)<<9 | 1<<5 | 1
	zipDosTime = 0
)

// fixedSize is the archive size excluding the filled part's data.
func (pkg docPackage) fixedSize() int64 {
	n := int64(zipEOCD)
	for _, p := range pkg {
		n += int64(zipLocalHeader + zipCentralHeader + 2*len(p.Name))
		if !p.Filled {
			n += int64(len(p.Data))
		}
	}
	return n
}

func (pkg docPackage) filled() docPart {
	for _, p := range pkg {
		if p.Filled {
			return p
		}
	}
	panic("package without filled part")
}

// budget is the chunk budget for a document of size bytes.
func (pkg docPackage) budget(size int64) int64 {
	f := pkg.filled()
	return size - pkg.fixedSize() - int64(len(f.Prefix)+len(f.Suffix))
}

func (pkg docPackage) minSize() int64 {
	f := pkg.filled()
	return pkg.fixedSize() + int64(len(f.Prefix)+len(f.Suffix)+len(f.Open(1))+len(f.Close))
}

// write emits a zip archive whose members are all stored (uncompressed),
// so the archive size is a linear function of the filler length.
func (pkg docPackage) write(w io.Writer, size int64) error {
	fp := pkg.filled()
	k, text, ok := fp.layout(pkg.budget(size))
	if !ok {
		return fmt.Errorf("size %d below minimum", size)
	}
	type entry struct {
		name      string
		crc, size uint32
		offset    uint32
	}
	var entries []entry
	var offset int64
	for _, p := range pkg {
		var crc uint32
		var n int64
		if p.Filled {
			h := crc32.NewIEEE()
			cw := &CountingWriter{W: h}
			if err := p.writeFilled(cw, k, text); err != nil {
				return err
			}
			crc, n = h.Sum32(), cw.N
		} else {
			crc, n = crc32.ChecksumIEEE([]byte(p.Data)), int64(len(p.Data))
		}
		e := entry{name: p.Name, crc: crc, size: uint32(n), offset: uint32(offset)}
		entries = append(entries, e)
		hdr := make([]byte, zipLocalHeader)
		binary.LittleEndian.PutUint32(hdr[0:], 0x04034b50)
		binary.LittleEndian.PutUint16(hdr[4:], 10) // version needed: stored
		binary.LittleEndian.PutUint16(hdr[10:], zipDosTime)
		binary.LittleEndian.PutUint16(hdr[12:], zipDosDate)
		binary.LittleEndian.PutUint32(hdr[14:], e.crc)
		binary.LittleEndian.PutUint32(hdr[18:], e.size)
		binary.LittleEndian.PutUint32(hdr[22:], e.size)
		binary.LittleEndian.PutUint16(hdr[26:], uint16(len(p.Name)))
		if _, err := w.Write(append(hdr, p.Name...)); err != nil {
			return err
		}
		var err error
		if p.Filled {
			err = p.writeFilled(w, k, text)
		} else {
			_, err = io.WriteString(w, p.Data)
		}
		if err != nil {
			return err
		}
		offset += int64(zipLocalHeader+len(p.Name)) + n
	}
	cdStart := offset
	for _, e := range entries {
		hdr := make([]byte, zipCentralHeader)
		binary.LittleEndian.PutUint32(hdr[0:], 0x02014b50)
		binary.LittleEndian.PutUint16(hdr[4:], 20) // made by: MS-DOS, spec 2.0
		binary.LittleEndian.PutUint16(hdr[6:], 10)
		binary.LittleEndian.PutUint16(hdr[12:], zipDosTime)
		binary.LittleEndian.PutUint16(hdr[14:], zipDosDate)
		binary.LittleEndian.PutUint32(hdr[16:], e.crc)
		binary.LittleEndian.PutUint32(hdr[20:], e.size)
		binary.LittleEndian.PutUint32(hdr[24:], e.size)
		binary.LittleEndian.PutUint16(hdr[28:], uint16(len(e.name)))
		binary.LittleEndian.PutUint32(hdr[42:], e.offset)
		if _, err := w.Write(append(hdr, e.name...)); err != nil {
			return err
		}
		offset += int64(zipCentralHeader + len(e.name))
	}
	eocd := make([]byte, zipEOCD)
	binary.LittleEndian.PutUint32(eocd[0:], 0x06054b50)
	binary.LittleEndian.PutUint16(eocd[8:], uint16(len(entries)))
	binary.LittleEndian.PutUint16(eocd[10:], uint16(len(entries)))
	binary.LittleEndian.PutUint32(eocd[12:], uint32(offset-cdStart))
	binary.LittleEndian.PutUint32(eocd[16:], uint32(cdStart))
	_, err := w.Write(eocd)
	return err
}

func zipDocFormat(ext, name, mime string, pkg docPackage) Format {
	return Format{
		Ext: ext, Name: name, MIME: mime, Category: CategoryDocument,
		Rule:  SizeRule{Min: pkg.minSize()},
		Write: pkg.write,
	}
}

func constChunk(s string) func(int) string { return func(int) string { return s } }

// ---------------------------------------------------------------- OOXML

const (
	xmlDecl     = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` + "\n"
	nsRelPkg    = "http://schemas.openxmlformats.org/package/2006/relationships"
	nsRelDoc    = "http://schemas.openxmlformats.org/officeDocument/2006/relationships"
	relTypeBase = nsRelDoc + "/"
	ctBase      = "application/vnd.openxmlformats-officedocument."
)

func contentTypes(overrides ...string) string {
	var b strings.Builder
	b.WriteString(xmlDecl + `<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">` +
		`<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>` +
		`<Default Extension="xml" ContentType="application/xml"/>`)
	for i := 0; i < len(overrides); i += 2 {
		fmt.Fprintf(&b, `<Override PartName="/%s" ContentType="%s"/>`, overrides[i], overrides[i+1])
	}
	b.WriteString(`</Types>`)
	return b.String()
}

// rels builds a relationships part from (type, target) pairs; ids are rId1...
func rels(pairs ...string) string {
	var b strings.Builder
	b.WriteString(xmlDecl + `<Relationships xmlns="` + nsRelPkg + `">`)
	for i := 0; i < len(pairs); i += 2 {
		fmt.Fprintf(&b, `<Relationship Id="rId%d" Type="%s%s" Target="%s"/>`, i/2+1, relTypeBase, pairs[i], pairs[i+1])
	}
	b.WriteString(`</Relationships>`)
	return b.String()
}

func docxPackage() docPackage {
	return docPackage{
		{Name: "[Content_Types].xml", Data: contentTypes(
			"word/document.xml", ctBase+"wordprocessingml.document.main+xml")},
		{Name: "_rels/.rels", Data: rels("officeDocument", "word/document.xml")},
		{Name: "word/document.xml", Filled: true,
			Prefix: xmlDecl + `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>`,
			Open:   constChunk(`<w:p><w:r><w:t xml:space="preserve">`), Close: `</w:t></w:r></w:p>`,
			Suffix:   `<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr></w:body></w:document>`,
			MaxChunk: 4096},
	}
}

func xlsxPackage() docPackage {
	return docPackage{
		{Name: "[Content_Types].xml", Data: contentTypes(
			"xl/workbook.xml", ctBase+"spreadsheetml.sheet.main+xml",
			"xl/worksheets/sheet1.xml", ctBase+"spreadsheetml.worksheet+xml")},
		{Name: "_rels/.rels", Data: rels("officeDocument", "xl/workbook.xml")},
		{Name: "xl/workbook.xml", Data: xmlDecl +
			`<workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="` + nsRelDoc + `">` +
			`<sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`},
		{Name: "xl/_rels/workbook.xml.rels", Data: rels("worksheet", "worksheets/sheet1.xml")},
		{Name: "xl/worksheets/sheet1.xml", Filled: true,
			Prefix: xmlDecl + `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData>`,
			Open: func(i int) string {
				return fmt.Sprintf(`<row r="%d"><c r="A%d" t="inlineStr"><is><t xml:space="preserve">`, i, i)
			},
			Close: `</t></is></c></row>`, Suffix: `</sheetData></worksheet>`,
			MaxChunk: 32767},
	}
}

const (
	nsA = "http://schemas.openxmlformats.org/drawingml/2006/main"
	nsP = "http://schemas.openxmlformats.org/presentationml/2006/main"
	// pmlNS declares the namespaces used by every PresentationML part.
	pmlNS        = `xmlns:a="` + nsA + `" xmlns:r="` + nsRelDoc + `" xmlns:p="` + nsP + `"`
	emptySpTree  = `<p:nvGrpSpPr><p:cNvPr id="1" name=""/><p:cNvGrpSpPr/><p:nvPr/></p:nvGrpSpPr><p:grpSpPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="0" cy="0"/><a:chOff x="0" y="0"/><a:chExt cx="0" cy="0"/></a:xfrm></p:grpSpPr>`
	pptThemeName = "bbdx"
)

func pptTheme() string {
	clr := func(name, rgb string) string {
		return fmt.Sprintf(`<a:%s><a:srgbClr val="%s"/></a:%s>`, name, rgb, name)
	}
	solid := `<a:solidFill><a:schemeClr val="phClr"/></a:solidFill>`
	ln := `<a:ln w="9525"><a:solidFill><a:schemeClr val="phClr"/></a:solidFill></a:ln>`
	fonts := `<a:latin typeface="Calibri"/><a:ea typeface=""/><a:cs typeface=""/>`
	return xmlDecl + `<a:theme xmlns:a="` + nsA + `" name="` + pptThemeName + `"><a:themeElements>` +
		`<a:clrScheme name="` + pptThemeName + `">` +
		`<a:dk1><a:sysClr val="windowText" lastClr="000000"/></a:dk1><a:lt1><a:sysClr val="window" lastClr="FFFFFF"/></a:lt1>` +
		clr("dk2", "44546A") + clr("lt2", "E7E6E6") + clr("accent1", "4472C4") + clr("accent2", "ED7D31") +
		clr("accent3", "A5A5A5") + clr("accent4", "FFC000") + clr("accent5", "5B9BD5") + clr("accent6", "70AD47") +
		clr("hlink", "0563C1") + clr("folHlink", "954F72") + `</a:clrScheme>` +
		`<a:fontScheme name="` + pptThemeName + `"><a:majorFont>` + fonts + `</a:majorFont><a:minorFont>` + fonts + `</a:minorFont></a:fontScheme>` +
		`<a:fmtScheme name="` + pptThemeName + `"><a:fillStyleLst>` + solid + solid + solid + `</a:fillStyleLst>` +
		`<a:lnStyleLst>` + ln + ln + ln + `</a:lnStyleLst>` +
		`<a:effectStyleLst><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle><a:effectStyle><a:effectLst/></a:effectStyle></a:effectStyleLst>` +
		`<a:bgFillStyleLst>` + solid + solid + solid + `</a:bgFillStyleLst></a:fmtScheme>` +
		`</a:themeElements></a:theme>`
}

func pptxPackage() docPackage {
	pml := ctBase + "presentationml."
	return docPackage{
		{Name: "[Content_Types].xml", Data: contentTypes(
			"ppt/presentation.xml", pml+"presentation.main+xml",
			"ppt/slideMasters/slideMaster1.xml", pml+"slideMaster+xml",
			"ppt/slideLayouts/slideLayout1.xml", pml+"slideLayout+xml",
			"ppt/slides/slide1.xml", pml+"slide+xml",
			"ppt/theme/theme1.xml", ctBase+"theme+xml")},
		{Name: "_rels/.rels", Data: rels("officeDocument", "ppt/presentation.xml")},
		{Name: "ppt/presentation.xml", Data: xmlDecl + `<p:presentation ` + pmlNS + `>` +
			`<p:sldMasterIdLst><p:sldMasterId id="2147483648" r:id="rId1"/></p:sldMasterIdLst>` +
			`<p:sldIdLst><p:sldId id="256" r:id="rId2"/></p:sldIdLst>` +
			`<p:sldSz cx="9144000" cy="6858000"/><p:notesSz cx="6858000" cy="9144000"/></p:presentation>`},
		{Name: "ppt/_rels/presentation.xml.rels", Data: rels(
			"slideMaster", "slideMasters/slideMaster1.xml", "slide", "slides/slide1.xml", "theme", "theme/theme1.xml")},
		{Name: "ppt/slideMasters/slideMaster1.xml", Data: xmlDecl + `<p:sldMaster ` + pmlNS + `>` +
			`<p:cSld><p:spTree>` + emptySpTree + `</p:spTree></p:cSld>` +
			`<p:clrMap bg1="lt1" tx1="dk1" bg2="lt2" tx2="dk2" accent1="accent1" accent2="accent2" accent3="accent3" accent4="accent4" accent5="accent5" accent6="accent6" hlink="hlink" folHlink="folHlink"/>` +
			`<p:sldLayoutIdLst><p:sldLayoutId id="2147483649" r:id="rId1"/></p:sldLayoutIdLst></p:sldMaster>`},
		{Name: "ppt/slideMasters/_rels/slideMaster1.xml.rels", Data: rels(
			"slideLayout", "../slideLayouts/slideLayout1.xml", "theme", "../theme/theme1.xml")},
		{Name: "ppt/slideLayouts/slideLayout1.xml", Data: xmlDecl + `<p:sldLayout ` + pmlNS + ` type="blank">` +
			`<p:cSld name="Blank"><p:spTree>` + emptySpTree + `</p:spTree></p:cSld>` +
			`<p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sldLayout>`},
		{Name: "ppt/slideLayouts/_rels/slideLayout1.xml.rels", Data: rels("slideMaster", "../slideMasters/slideMaster1.xml")},
		{Name: "ppt/theme/theme1.xml", Data: pptTheme()},
		{Name: "ppt/slides/_rels/slide1.xml.rels", Data: rels("slideLayout", "../slideLayouts/slideLayout1.xml")},
		{Name: "ppt/slides/slide1.xml", Filled: true,
			Prefix: xmlDecl + `<p:sld ` + pmlNS + `><p:cSld><p:spTree>` + emptySpTree +
				`<p:sp><p:nvSpPr><p:cNvPr id="2" name="Text"/><p:cNvSpPr txBox="1"/><p:nvPr/></p:nvSpPr>` +
				`<p:spPr><a:xfrm><a:off x="457200" y="457200"/><a:ext cx="8229600" cy="5943600"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></p:spPr>` +
				`<p:txBody><a:bodyPr wrap="square"/><a:lstStyle/>`,
			Open:     constChunk(`<a:p><a:r><a:rPr lang="en-US"/><a:t>`),
			Close:    `</a:t></a:r></a:p>`,
			Suffix:   `</p:txBody></p:sp></p:spTree></p:cSld><p:clrMapOvr><a:masterClrMapping/></p:clrMapOvr></p:sld>`,
			MaxChunk: 4096},
	}
}

// ---------------------------------------------------------------- OpenDocument

const (
	odfMime = "application/vnd.oasis.opendocument."
	odfNS   = `xmlns:office="urn:oasis:names:tc:opendocument:xmlns:office:1.0" ` +
		`xmlns:style="urn:oasis:names:tc:opendocument:xmlns:style:1.0" ` +
		`xmlns:text="urn:oasis:names:tc:opendocument:xmlns:text:1.0" ` +
		`xmlns:table="urn:oasis:names:tc:opendocument:xmlns:table:1.0" ` +
		`xmlns:draw="urn:oasis:names:tc:opendocument:xmlns:drawing:1.0" ` +
		`xmlns:fo="urn:oasis:names:tc:opendocument:xmlns:xsl-fo-compatible:1.0" ` +
		`xmlns:svg="urn:oasis:names:tc:opendocument:xmlns:svg-compatible:1.0" office:version="1.3"`
	odfXMLDecl = `<?xml version="1.0" encoding="UTF-8"?>` + "\n"
)

func odfManifest(mime string, files ...string) string {
	var b strings.Builder
	b.WriteString(odfXMLDecl + `<manifest:manifest xmlns:manifest="urn:oasis:names:tc:opendocument:xmlns:manifest:1.0" manifest:version="1.3">`)
	fmt.Fprintf(&b, `<manifest:file-entry manifest:full-path="/" manifest:version="1.3" manifest:media-type="%s"/>`, mime)
	for _, f := range files {
		fmt.Fprintf(&b, `<manifest:file-entry manifest:full-path="%s" manifest:media-type="text/xml"/>`, f)
	}
	b.WriteString(`</manifest:manifest>`)
	return b.String()
}

func odfPackage(kind, body string, open, close, suffix string, extra ...docPart) docPackage {
	mime := odfMime + kind
	files := []string{"content.xml"}
	for _, e := range extra {
		files = append(files, e.Name)
	}
	pkg := docPackage{
		{Name: "mimetype", Data: mime},
		{Name: "META-INF/manifest.xml", Data: odfManifest(mime, files...)},
	}
	pkg = append(pkg, extra...)
	return append(pkg, docPart{Name: "content.xml", Filled: true,
		Prefix: odfXMLDecl + `<office:document-content ` + odfNS + `><office:body>` + body,
		Open:   constChunk(open), Close: close,
		Suffix:   suffix + `</office:body></office:document-content>`,
		MaxChunk: 4096})
}

func odtPackage() docPackage {
	return odfPackage("text", `<office:text>`, `<text:p>`, `</text:p>`, `</office:text>`)
}

func odsPackage() docPackage {
	return odfPackage("spreadsheet",
		`<office:spreadsheet><table:table table:name="Sheet1"><table:table-column/>`,
		`<table:table-row><table:table-cell office:value-type="string"><text:p>`,
		`</text:p></table:table-cell></table:table-row>`,
		`</table:table></office:spreadsheet>`)
}

func odpPackage() docPackage {
	styles := docPart{Name: "styles.xml", Data: odfXMLDecl + `<office:document-styles ` + odfNS + `>` +
		`<office:automatic-styles><style:page-layout style:name="PM1">` +
		`<style:page-layout-properties fo:page-width="28cm" fo:page-height="15.75cm" style:print-orientation="landscape"/>` +
		`</style:page-layout></office:automatic-styles>` +
		`<office:master-styles><style:master-page style:name="Default" style:page-layout-name="PM1"/></office:master-styles>` +
		`</office:document-styles>`}
	return odfPackage("presentation",
		`<office:presentation><draw:page draw:name="page1" draw:master-page-name="Default">`+
			`<draw:frame svg:x="1cm" svg:y="1cm" svg:width="26cm" svg:height="13.75cm"><draw:text-box>`,
		`<text:p>`, `</text:p>`,
		`</draw:text-box></draw:frame></draw:page></office:presentation>`, styles)
}

// ---------------------------------------------------------------- EPUB 3

func epubPackage() docPackage {
	xhtml := func(title, body string) string {
		return `<?xml version="1.0" encoding="UTF-8"?>` + "\n<!DOCTYPE html>\n" +
			`<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" xml:lang="en" lang="en">` +
			`<head><title>` + title + `</title></head><body>` + body
	}
	return docPackage{
		{Name: "mimetype", Data: "application/epub+zip"},
		{Name: "META-INF/container.xml", Data: odfXMLDecl +
			`<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container"><rootfiles>` +
			`<rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`},
		{Name: "OEBPS/content.opf", Data: odfXMLDecl +
			`<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid" xml:lang="en">` +
			`<metadata xmlns:dc="http://purl.org/dc/elements/1.1/">` +
			`<dc:identifier id="uid">urn:uuid:6b9e0c2a-5f0b-4d4e-9c1a-bbd0000f11e5</dc:identifier>` +
			`<dc:title>bbdx test file</dc:title><dc:language>en</dc:language>` +
			`<meta property="dcterms:modified">2026-01-01T00:00:00Z</meta></metadata>` +
			`<manifest><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>` +
			`<item id="chapter" href="chapter.xhtml" media-type="application/xhtml+xml"/></manifest>` +
			`<spine><itemref idref="chapter"/></spine></package>`},
		{Name: "OEBPS/nav.xhtml", Data: xhtml("Contents",
			`<nav epub:type="toc"><h1>Contents</h1><ol><li><a href="chapter.xhtml">bbdx test file</a></li></ol></nav></body></html>`)},
		{Name: "OEBPS/chapter.xhtml", Filled: true,
			Prefix: xhtml("bbdx test file", `<h1>bbdx test file</h1>`),
			Open:   constChunk(`<p>`), Close: `</p>`, Suffix: `</body></html>`,
			MaxChunk: 4096},
	}
}
