package model

import (
	"fmt"
	"io"
)

// Text based formats are a fixed skeleton with a filler slot that accepts any
// number of bytes, so every size >= Min can be produced exactly.

func textFormats() []Format {
	return []Format{
		textTemplate(Format{Ext: "txt", Aliases: []string{"text"}, Name: "Plain text", MIME: "text/plain", Category: CategoryText},
			"", "", true),
		textLines(Format{Ext: "csv", Name: "Comma-separated values (RFC 4180)", MIME: "text/csv", Category: CategoryText},
			"id,text\r\n", func(i int) string { return fmt.Sprintf("%08d,", i+1) }, "\r\n", 0),
		textTemplate(Format{Ext: "json", Name: "JSON (RFC 8259)", MIME: "application/json", Category: CategoryText},
			`{"content":"`, "\"}\n", false),
		textTemplate(Format{Ext: "xml", Name: "XML 1.0 document", MIME: "application/xml", Category: CategoryText},
			"<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<content>", "</content>\n", true),
		textTemplate(Format{Ext: "yaml", Aliases: []string{"yml"}, Name: "YAML 1.2 document", MIME: "application/yaml", Category: CategoryText},
			"---\ncontent: \"", "\"\n", false),
		textTemplate(Format{Ext: "ini", Aliases: []string{"cfg", "conf"}, Name: "INI configuration", MIME: "text/plain", Category: CategoryText},
			"; bbdx test file\n[bbdx]\ncontent = ", "\n", false),
		textLines(Format{Ext: "log", Name: "Log file", MIME: "text/plain", Category: CategoryText},
			"", func(int) string { return "2026-01-01T00:00:00Z INFO " }, "\n", 1),
		textFolded(Format{Ext: "ics", Aliases: []string{"ical"}, Name: "iCalendar (RFC 5545)", MIME: "text/calendar", Category: CategoryText},
			"BEGIN:VCALENDAR\r\nVERSION:2.0\r\nPRODID:-//bbdx//bbdx_file_gen//EN\r\nBEGIN:VEVENT\r\n"+
				"UID:bbdx-test-file@bbdx.invalid\r\nDTSTAMP:20260101T000000Z\r\nDTSTART:20260101T090000Z\r\n"+
				"SUMMARY:bbdx test file\r\n",
			"DESCRIPTION:", "\r\nEND:VEVENT\r\nEND:VCALENDAR\r\n"),
		textFolded(Format{Ext: "vcf", Aliases: []string{"vcard"}, Name: "vCard 4.0 (RFC 6350)", MIME: "text/vcard", Category: CategoryText},
			"BEGIN:VCARD\r\nVERSION:4.0\r\nFN:bbdx test file\r\n", "NOTE:", "\r\nEND:VCARD\r\n"),

		textTemplate(Format{Ext: "md", Aliases: []string{"markdown"}, Name: "Markdown (CommonMark)", MIME: "text/markdown", Category: CategoryDocument},
			"# bbdx test file\n", "", true),
		textTemplate(Format{Ext: "rtf", Name: "Rich Text Format 1.9", MIME: "application/rtf", Category: CategoryDocument},
			`{\rtf1\ansi\deff0{\fonttbl{\f0 Helvetica;}}\f0\fs24 `, "\\par}\n", false),

		textTemplate(Format{Ext: "html", Aliases: []string{"htm"}, Name: "HTML5 document", MIME: "text/html", Category: CategoryWeb},
			"<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n<title>bbdx test file</title>\n</head>\n<body>\n<p>",
			"</p>\n</body>\n</html>\n", true),
		textTemplate(Format{Ext: "css", Name: "Cascading Style Sheet", MIME: "text/css", Category: CategoryWeb},
			"/* ", " */\nbody { margin: 0; }\n", true),
		textTemplate(Format{Ext: "js", Aliases: []string{"mjs"}, Name: "JavaScript (ECMAScript)", MIME: "text/javascript", Category: CategoryWeb},
			"/* ", " */\nconsole.log(\"bbdx test file\");\n", true),
		textTemplate(Format{Ext: "svg", Name: "Scalable Vector Graphics 1.1", MIME: "image/svg+xml", Category: CategoryImage},
			`<svg xmlns="http://www.w3.org/2000/svg" width="16" height="16" viewBox="0 0 16 16">`+
				`<rect width="16" height="16" fill="#3b82f6"/><desc>`,
			"</desc></svg>\n", true),

		textTemplate(Format{Ext: "py", Name: "Python 3 source", MIME: "text/x-python", Category: CategoryCode},
			"#!/usr/bin/env python3\n\"\"\"", "\"\"\"\nprint(\"bbdx test file\")\n", true),
		textTemplate(Format{Ext: "java", Name: "Java source", MIME: "text/x-java-source", Category: CategoryCode},
			"/* ", " */\nclass BbdxTestFile {\n    public static void main(String[] args) {\n"+
				"        System.out.println(\"bbdx test file\");\n    }\n}\n", true),
		textTemplate(Format{Ext: "c", Name: "C source (ISO C11)", MIME: "text/x-c", Category: CategoryCode},
			"/* ", " */\n#include <stdio.h>\n\nint main(void)\n{\n    puts(\"bbdx test file\");\n    return 0;\n}\n", true),
		textTemplate(Format{Ext: "sh", Name: "POSIX shell script", MIME: "application/x-sh", Category: CategoryCode},
			"#!/bin/sh\n: <<'BBDX'\n", "\nBBDX\necho \"bbdx test file\"\n", true),
		textTemplate(Format{Ext: "sql", Name: "SQL script", MIME: "application/sql", Category: CategoryCode},
			"/* ", " */\nCREATE TABLE bbdx (id INTEGER PRIMARY KEY, content TEXT);\n"+
				"INSERT INTO bbdx (content) VALUES ('bbdx test file');\n", true),
		textTemplate(Format{Ext: "php", Name: "PHP script", MIME: "application/x-httpd-php", Category: CategoryCode},
			"<?php\n/* ", " */\necho \"bbdx test file\\n\";\n", true),
	}
}

// textTemplate fills the gap between head and tail with Lorem text.
func textTemplate(f Format, head, tail string, multiline bool) Format {
	fixed := int64(len(head) + len(tail))
	f.Rule = SizeRule{Min: fixed}
	f.Write = func(w io.Writer, size int64) error {
		if _, err := io.WriteString(w, head); err != nil {
			return err
		}
		if err := Lorem(w, size-fixed, multiline); err != nil {
			return err
		}
		_, err := io.WriteString(w, tail)
		return err
	}
	return f
}

// textLines writes head followed by records of prefix(i)+text+suffix. prefix must
// return strings of constant length; every record holds at least minText bytes.
func textLines(f Format, head string, prefix func(int) string, suffix string, minText int64) Format {
	const maxText = 64
	overhead := int64(len(prefix(0)) + len(suffix))
	f.Rule = SizeRule{Min: int64(len(head)) + overhead + minText}
	f.Write = func(w io.Writer, size int64) error {
		if _, err := io.WriteString(w, head); err != nil {
			return err
		}
		total := size - int64(len(head))
		n := (total + overhead + maxText - 1) / (overhead + maxText) // number of records
		extra := total - n*(overhead+minText)                        // text beyond the minimum
		for i := int64(0); i < n; i++ {
			text := minText + min(extra, maxText-minText)
			extra -= text - minText
			if _, err := io.WriteString(w, prefix(int(i))); err != nil {
				return err
			}
			if err := Lorem(w, text, false); err != nil {
				return err
			}
			if _, err := io.WriteString(w, suffix); err != nil {
				return err
			}
		}
		return nil
	}
	return f
}

// textFolded writes one content line "name:value" folded at 75 octets as required
// by RFC 5545 §3.1 / RFC 6350 §3.2 (CRLF followed by a single space).
func textFolded(f Format, head, name, tail string) Format {
	const lineMax, foldLen = 75, 3 // "\r\n "
	fixed := int64(len(head) + len(name) + len(tail))
	first := int64(lineMax - len(name))
	f.Rule = SizeRule{Min: fixed}
	f.Write = func(w io.Writer, size int64) error {
		if _, err := io.WriteString(w, head+name); err != nil {
			return err
		}
		fill := size - fixed
		folds := int64(0) // fewest folds that make the value fit
		if fill > first {
			folds = (fill - first + lineMax + foldLen - 2) / (lineMax - 1 + foldLen)
		}
		value := fill - foldLen*folds
		seg := min(first, value-folds) // every continuation line carries >= 1 byte
		text := &loremStream{}
		if err := text.write(w, seg); err != nil {
			return err
		}
		rest := value - seg
		for i := folds; i > 0; i-- {
			seg = min(lineMax-1, rest-(i-1))
			if _, err := io.WriteString(w, "\r\n "); err != nil {
				return err
			}
			if err := text.write(w, seg); err != nil {
				return err
			}
			rest -= seg
		}
		_, err := io.WriteString(w, tail)
		return err
	}
	return f
}
