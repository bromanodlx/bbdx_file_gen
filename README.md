# bbdx_file_gen

A terminal UI for creating **standard-compliant test files** of an exact size.
It asks four questions:

1. **Name**: the file name, which may include a directory, e.g. `uploads/report`.
2. **File type**: an extension or alias, e.g. `pdf`, `.JPEG`, `yml`. A live, filterable list is shown.
3. **Size**: e.g. `2048`, `64KiB`, `1.5MB`. Leave it empty to get the smallest standard-compliant file of that type.
4. **Location**: *Create the file in the current directory?* **Yes** puts it in the
   current directory, **No** puts it in your standard download folder. The download
   folder comes from `$XDG_DOWNLOAD_DIR`, then `XDG_DOWNLOAD_DIR` in
   `~/.config/user-dirs.dirs` (so localized names like `~/Herunterladen` work),
   then `~/Downloads`. It is created if it doesn't exist. Absolute names such as
   `/tmp/x` skip this question.

Every generated file is a real, valid file of its type: PDFs open, images decode,
media plays, archives test OK, and source files compile or run. It is also
exactly the requested number of bytes. Padding always goes into a structure the
format defines for it, such as comments, padding blocks, `free` boxes, `JUNK`
chunks, stored archive members or document text. It is never junk appended to
the end of the file.

## Build & run

```sh
go build -o bbdx_file_gen .
./bbdx_file_gen          # interactive TUI
./bbdx_file_gen -list    # print all supported types with their minimum sizes
```

Keys: `enter` next · `esc` back · `tab` complete the type, or apply the suggested size ·
`↑/↓` choose a type · `y`/`n` (or `←/→` + `enter`) answer the location question · `ctrl+c` quit. Existing files are only replaced after a
`y/N` confirmation. Files are written to a temporary name and renamed into place,
so an aborted run never leaves a partial file behind.

Size units: `B`; `KB`/`MB`/`GB` = ×1000 (SI); `KiB`/`MiB`/`GiB` or `K`/`M`/`G` = ×1024 (IEC).
Fractions are accepted when they come out to whole bytes (`1.5KiB` = 1536). The maximum is 1 GiB.

## Architecture (MVC)

| Layer | Package | Responsibility |
|---|---|---|
| **Model** | `internal/model` | Domain logic with no UI code. `Format` (metadata, `SizeRule`, streaming writer), `Registry` (lookup by extension or alias, prefix search), `Service` (validation, atomic file creation), download-folder lookup, size parsing, and the 50 generators grouped by family in `formats_*.go`. |
| **View** | `internal/view` | `Render(State) string` and `RenderList`. Pure functions with no state and no model access. |
| **Controller** | `internal/controller` | Implements Bubble Tea's `tea.Model`. Translates key presses into model calls, runs generation asynchronously with a spinner, and builds the `view.State`. |

`main.go` only wires the three together.

Generators stream their output and compute checksums (CRC-32, CRC-8/16, MD5)
incrementally, so a 1 GiB file never sits in memory.

## Minimum size and unreachable sizes

The minimum is the smallest file that is valid under the format's specification
and recognizable as that type. `txt` is the one exception: an empty file is a
valid text file, so its minimum is 0 B.

Some specifications make certain sizes impossible. When that happens the TUI
explains why, offers the nearest valid sizes, and `tab` applies the suggestion:

| Type | Constraint |
|---|---|
| wav | even sizes only (RIFF word alignment) |
| avi | even sizes only; 261–267 unreachable (a JUNK chunk needs ≥ 8 bytes) |
| webp | even sizes only; 35–51 and 53–59 unreachable (padding needs the extended VP8X layout) |
| tar | multiples of 512 (tar blocks) |
| sqlite | multiples of 512 (database pages) |
| xz | multiples of 4 (xz streams and stream padding) |
| mp3 | 195–212 unreachable (96-byte frames; an ID3v2 tag needs ≥ 21 bytes) |
| flac | 54–56 unreachable (a PADDING block needs ≥ 4 bytes) |
| mid | 27–29 unreachable (a text meta event needs ≥ 4 bytes) |

Every other type accepts every size from its minimum up to 1 GiB.

## Supported file types (50)

| Category | Types (minimum size) |
|---|---|
| Documents | pdf (595 B), docx (1.43 KiB), xlsx (2.22 KiB), pptx (8.91 KiB), odt (1.35 KiB), ods (1.54 KiB), odp (2.62 KiB), epub (2 KiB), rtf (58 B), md (17 B) |
| Text & data | txt (0 B), csv (20 B), json (15 B), xml (59 B), yaml (16 B), ini (35 B), log (28 B), ics (229 B), vcf (63 B) |
| Web | html (132 B), css (27 B), js (38 B) |
| Source code | py (54 B), java (127 B), c (88 B), sh (49 B), sql (119 B), php (38 B) |
| Images | png (70 B), jpg (141 B), gif (35 B), bmp (58 B), tiff (175 B), ico (92 B), webp (34 B), svg (148 B) |
| Audio | wav (44 B), mp3 (192 B), flac (53 B), mid (26 B) |
| Video | mp4 (783 B), mov (740 B), avi (260 B), mkv (303 B), webm (192 B) |
| Archives & databases | zip (22 B), gz (20 B), tar (1 KiB), xz (32 B), sqlite (512 B) |

Aliases: `jpeg`/`jpe`, `tif`, `htm`, `yml`, `markdown`, `text`, `mjs`, `cfg`/`conf`,
`ical`, `vcard`, `midi`, `m4v`, `gzip`, `sqlite3`/`db`.

## Tests

The project was developed test-first. Run the tests with:

```sh
go test ./...          # full suite (~35 s, includes 1–3 MiB sweeps and LibreOffice round trips)
go test -short ./...   # quick suite
```

- **Conformance harness** (`formats_test.go`): every format is generated at every
  size from its minimum to minimum+160, plus sizes around 64 KiB, 128 KiB, 1 MiB
  and 3 MiB. Each file must be exactly the requested size and must pass a
  structural validator written in Go for that format. Sizes the format cannot
  reach must return a `SizeError`. Output must be deterministic.
- **External validators**, each skipped if the tool is missing: `ffprobe`/`ffmpeg`
  (decode), ImageMagick `identify -regard-warnings`, `pdfinfo`, `unzip -t`,
  `7z t`, `gzip -t`, `xz -t`, `tar -tvf`, `xmllint`, LibreOffice headless
  conversion, SQLite `integrity_check`, `python3 -m py_compile`, `cc -pedantic -Werror`,
  `node --check`, `sh -n`, `javac`, `php -l`.
- **Controller tests** run complete wizard flows: minimum size, units,
  unreachable sizes with the tab suggestion, aliases, both location answers, back navigation, overwrite
  confirmation, errors and quitting.
- **View tests** check the rendered content and that every line fits in a 40-column terminal.
