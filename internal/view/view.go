// Package view renders the TUI. It is a pure function of State: it holds no
// state of its own and never talks to the model.
//
// Everything is drawn inside a single rounded "card" whose width tracks the
// terminal. The brand sits in the top edge, a progress rail and the active
// step's fields fill the body, and keycap-styled shortcuts run underneath.
// In plain mode the glyphs fall back to ASCII and all color is stripped, for
// terminals without Unicode or ANSI support.
package view

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Step is the wizard step currently shown.
type Step int

const (
	StepName Step = iota
	StepExt
	StepSize
	StepLocation
	StepConfirm
	StepGenerating
	StepDone
	// StepDownloadDir is the settings screen for the standard download
	// directory; it is not one of the numbered wizard steps.
	StepDownloadDir
	// StepUpdate asks whether to install a newer release; it pops up over
	// whatever step was active when the background check found one.
	StepUpdate
)

// Suggestion is one entry of the file type list.
type Suggestion struct {
	Ext, Name, Category, MinSize string
	Selected                     bool
}

// State is everything the view needs to draw a frame.
type State struct {
	Step          Step
	Width, Height int
	Plain         bool // ASCII-only glyphs and no color

	Input string // the rendered text input of the active step

	Name       string
	Ext        string
	FormatName string
	TargetPath string

	Suggestions     []Suggestion
	MoreSuggestions int

	MinSize  string
	MinBytes int64
	SizeNote string

	CurrentDir  string
	DownloadDir string
	LocationYes bool // "Yes" (current directory) is highlighted

	DownloadReason string // why the download directory is being asked for
	DownloadCancel bool   // esc returns instead of quitting

	EICAR bool // the file is an EICAR anti-malware test file

	UpdateVersion    string // newer release offered in the update question
	InstalledVersion string

	SizeLabel string // human readable size of the file being/just generated
	Bytes     int64
	Spinner   string

	Error string
	Hint  string
}

const (
	defaultWidth = 74
	maxWidth     = 88
	minWidth     = 30
)

var (
	accent = lipgloss.AdaptiveColor{Light: "#4f46e5", Dark: "#7aa2f7"}
	orange = lipgloss.AdaptiveColor{Light: "#c2410c", Dark: "#ff9e64"}
	muted  = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#737aa2"}
	faint  = lipgloss.AdaptiveColor{Light: "#94a3b8", Dark: "#3b4261"}
	good   = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#9ece6a"}
	bad    = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f7768e"}
	amber  = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#e0af68"}
	paper  = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#1a1b26"}

	brandSt   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	frameSt   = lipgloss.NewStyle().Foreground(faint)
	activeSt  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	doneSt    = lipgloss.NewStyle().Foreground(good)
	mutedSt   = lipgloss.NewStyle().Foreground(muted)
	labelSt   = lipgloss.NewStyle().Bold(true).Foreground(orange)
	barSt     = lipgloss.NewStyle().Foreground(orange)
	errorSt   = lipgloss.NewStyle().Foreground(bad)
	successSt = lipgloss.NewStyle().Bold(true).Foreground(good)
	extSt     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	selectSt  = lipgloss.NewStyle().Bold(true).Foreground(paper).Background(accent)
	keySt     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	eicarSt   = lipgloss.NewStyle().Foreground(amber)
)

// Banner is "bbdx" in the FIGlet "Standard" font, one string per row.
var Banner = func() []string {
	b := []string{" _     ", "| |__  ", "| '_ \\ ", "| |_) |", "|_.__/ "}
	d := []string{"     _ ", "  __| |", " / _` |", "| (_| |", " \\__,_|"}
	x := []string{"      ", "__  __", "\\ \\/ /", " >  < ", "/_/\\_\\"}
	rows := make([]string, len(b))
	for i := range rows {
		rows[i] = b[i] + b[i] + d[i] + x[i]
	}
	return rows
}()

// Command is the tool's command name, shown in the card and in CLI messages.
const Command = "bbdxfg"

// bannerBlock renders the FIGlet banner and subtitle, falling back to a plain
// wordmark when the art would not fit the card.
func bannerBlock(inner int, g glyphs) []string {
	var out []string
	if ansi.StringWidth(Banner[0]) <= inner {
		for _, row := range Banner {
			out = append(out, brandSt.Render(row))
		}
	} else {
		out = append(out, brandSt.Render(Command))
	}
	return append(out,
		mutedSt.Render(Command+" "+g.dash+" standard-compliant test files"),
		"",
	)
}

// glyphs is the character set used to draw the frame and markers.
type glyphs struct {
	tl, tr, bl, br, h, v         string // card corners, horizontal, vertical
	bar, done, active, pending   string // headings and the progress rail
	sel, warn, gear, arrow, hint string // markers
	sep, dash, ell               string // middle dot, em dash, truncation tail
	up                           string // update marker
}

var unicodeGlyphs = glyphs{
	tl: "╭", tr: "╮", bl: "╰", br: "╯", h: "─", v: "│",
	bar: "▍", done: "✓", active: "●", pending: "○",
	sel: "▸", warn: "⚠", gear: "⚙", arrow: "→", hint: "➜",
	sep: "·", dash: "—", ell: "…", up: "⬆",
}

var asciiGlyphs = glyphs{
	tl: "+", tr: "+", bl: "+", br: "+", h: "-", v: "|",
	bar: "|", done: "v", active: "*", pending: "o",
	sel: ">", warn: "!", gear: "*", arrow: "->", hint: "->",
	sep: "-", dash: "-", ell: "...", up: "^",
}

// Render draws the whole screen.
func Render(s State) string {
	s = s.sanitized()
	g := unicodeGlyphs
	if s.Plain {
		g = asciiGlyphs
	}
	total := frameWidth(s.Width)
	inner := total - 4 // two border cells and one space of padding per side

	body := bannerBlock(inner, g)
	switch s.Step {
	case StepDownloadDir:
		body = append(body, activeSt.Render(g.gear+"  Settings"))
	case StepUpdate:
		body = append(body, activeSt.Render(g.up+"  Update"))
	default:
		body = append(body, rail(s.Step, inner, g))
	}
	body = append(body, "")
	body = append(body, section(s, inner, g)...)
	if s.Error != "" {
		body = append(body, "", errorSt.Render(g.warn+" "+s.Error))
	}
	if s.Hint != "" {
		body = append(body, activeSt.Render(g.hint+" "+s.Hint))
	}

	var b strings.Builder
	b.WriteString(border(g.tl, g.tr, total, g) + "\n")
	for _, line := range body {
		b.WriteString(bodyLine(line, inner, g) + "\n")
	}
	b.WriteString(border(g.bl, g.br, total, g) + "\n\n")
	b.WriteString(helpBlock(s, total, g))

	out := fit(b.String(), s.Width, g.ell)
	if s.Plain {
		out = ansi.Strip(out)
	}
	return out
}

func frameWidth(w int) int {
	switch {
	case w <= 0:
		return defaultWidth
	case w < minWidth:
		return max(w, 12)
	case w > maxWidth:
		return maxWidth
	default:
		return w
	}
}

// section returns the body lines for the active step.
func section(s State, inner int, g glyphs) []string {
	switch s.Step {
	case StepDownloadDir:
		lines := []string{heading("Standard download directory", "", g)}
		for _, r := range strings.Split(s.DownloadReason, "\n") {
			if strings.TrimSpace(r) != "" {
				lines = append(lines, mutedSt.Render(r))
			}
		}
		return append(lines,
			s.Input,
			mutedSt.Render("Paths work like in bash: absolute (/data/dl), relative to the"),
			mutedSt.Render("current directory (../dl), ~, $VARIABLES, quotes and \\ escapes."),
		)
	case StepUpdate:
		return []string{
			heading("Update available", "", g),
			fmt.Sprintf("%s %s is available (installed: %s).", Command, extSt.Render(s.UpdateVersion), s.InstalledVersion),
			"",
			"Install it now?  " + activeSt.Render("[y/N]"),
			mutedSt.Render("Yes closes " + Command + ", installs the update and starts it again."),
			mutedSt.Render("No continues where you were; you'll be asked again next start."),
		}
	case StepName:
		return []string{
			heading("File name", "", g),
			s.Input,
			mutedSt.Render("The name of the file to create; may include a directory."),
		}
	case StepExt:
		lines := []string{heading("File type", "for "+s.Name, g), s.Input, ""}
		return append(lines, suggestionRows(s, inner, g)...)
	case StepSize:
		lines := []string{
			heading("Size", s.FormatName+" "+g.arrow+" "+s.TargetPath, g),
			s.Input,
			mutedSt.Render(fmt.Sprintf("Empty = minimum standard-compliant size: %s (%d bytes).", s.MinSize, s.MinBytes)),
			mutedSt.Render("Units: B " + g.sep + " KB/MB/GB x1000 " + g.sep + " KiB/MiB/GiB (or K/M/G) x1024"),
		}
		if s.SizeNote != "" {
			lines = append(lines, eicarSt.Render("Note: "+s.SizeNote))
		}
		return lines
	case StepLocation:
		lines := []string{heading("Location", "for "+s.TargetPath, g)}
		if s.EICAR {
			lines = append(lines, eicarSt.Render(g.warn+" EICAR anti-malware test file "+g.dash+" 68 bytes, the smallest possible."))
		}
		return append(lines,
			"Create the file in the current directory?",
			"",
			choiceRow(s.LocationYes, "Yes", s.CurrentDir, inner, g),
			choiceRow(!s.LocationYes, "No ", s.DownloadDir+"  (download folder)", inner, g),
		)
	case StepConfirm:
		return []string{
			errorSt.Render(g.warn + " " + s.TargetPath + " already exists."),
			"",
			"Overwrite it?  " + activeSt.Render("[y/N]"),
		}
	case StepGenerating:
		return []string{activeSt.Render(s.Spinner+" ") + "Generating " + s.TargetPath + mutedSt.Render("  "+g.sep+"  "+s.SizeLabel+" "+g.ell)}
	case StepDone:
		lines := []string{
			successSt.Render(g.done+" Created ") + s.TargetPath,
			mutedSt.Render(fmt.Sprintf("  %s "+g.sep+" %s (%d bytes)", s.FormatName, s.SizeLabel, s.Bytes)),
		}
		if s.EICAR {
			lines = append(lines, eicarSt.Render("  EICAR test file "+g.dash+" scanners will flag it; this is expected."))
		}
		return lines
	}
	return nil
}

// heading renders an accent-barred section title with an optional muted note.
func heading(title, note string, g glyphs) string {
	h := barSt.Render(g.bar) + labelSt.Render(title)
	if note != "" {
		h += mutedSt.Render("  " + note)
	}
	return h
}

// rail draws the four-step progress breadcrumb, collapsing to a compact badge
// when the full form would not fit.
func rail(cur Step, inner int, g glyphs) string {
	names := []string{"Name", "Type", "Size", "Location"}
	segs := make([]string, len(names))
	for i, n := range names {
		switch {
		case Step(i) < cur:
			segs[i] = doneSt.Render(g.done + " " + n)
		case Step(i) == cur:
			segs[i] = activeSt.Render(fmt.Sprintf("%s %d %s", g.active, i+1, n))
		default:
			segs[i] = mutedSt.Render(fmt.Sprintf("%s %d %s", g.pending, i+1, n))
		}
	}
	full := strings.Join(segs, frameSt.Render(" "+g.h+" "))
	if ansi.StringWidth(full) <= inner {
		return full
	}
	if int(cur) < len(names) {
		return activeSt.Render(fmt.Sprintf("%s %d %s", g.active, cur+1, names[cur])) +
			mutedSt.Render(fmt.Sprintf("  %s  step %d of 4", g.sep, cur+1))
	}
	return doneSt.Render(g.done + " all steps complete")
}

// suggestionRows renders the filterable file-type list, right-aligning each
// row's category and minimum size and highlighting the selected entry.
func suggestionRows(s State, inner int, g glyphs) []string {
	if len(s.Suggestions) == 0 {
		return []string{mutedSt.Render("  no matching file type " + g.dash + " keep typing")}
	}
	out := make([]string, 0, len(s.Suggestions)+1)
	for _, sg := range s.Suggestions {
		ext := fmt.Sprintf("%-5s", sg.Ext)
		meta := sg.Category + " " + g.sep + " min " + sg.MinSize
		if sg.Selected {
			left := g.sel + " " + ext + "  " + sg.Name
			out = append(out, selectSt.Render(pad(left, meta, inner, g.ell)))
			continue
		}
		left := "  " + extSt.Render(ext) + "  " + sg.Name
		out = append(out, padStyled(left, mutedSt.Render(meta), inner))
	}
	if s.MoreSuggestions > 0 {
		out = append(out, mutedSt.Render(fmt.Sprintf("  %s %d more, keep typing to narrow", g.ell, s.MoreSuggestions)))
	}
	return out
}

// choiceRow renders one option of the location question.
func choiceRow(selected bool, label, dir string, inner int, g glyphs) string {
	if selected {
		return selectSt.Render(cell(g.sel+" "+label+"  "+g.arrow+" "+dir, inner, g.ell))
	}
	return "  " + label + mutedSt.Render("  "+g.arrow+" "+dir)
}

// pad joins a plain left and right segment flush to exactly width w,
// truncating with ell when they do not fit.
func pad(left, right string, w int, ell string) string {
	return cell(padStyled(left, right, w), w, ell)
}

// padStyled pushes right flush to width w, keeping at least two spaces after
// left; the segments may carry ANSI styling.
func padStyled(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return left + "  " + right
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- frame ---

// border draws the card's top or bottom edge between two corner glyphs.
func border(left, right string, total int, g glyphs) string {
	return frameSt.Render(left + strings.Repeat(g.h, total-2) + right)
}

func bodyLine(content string, inner int, g glyphs) string {
	return frameSt.Render(g.v) + " " + cell(content, inner, g.ell) + " " + frameSt.Render(g.v)
}

// --- help ---

func helpBlock(s State, total int, g glyphs) string {
	lines := wrapItems(helpItems(s), total)
	rendered := make([]string, len(lines))
	for i, items := range lines {
		caps := make([]string, len(items))
		for j, it := range items {
			caps[j] = kbd(it)
		}
		rendered[i] = "  " + strings.Join(caps, frameSt.Render(" "+g.sep+" "))
	}
	return strings.Join(rendered, "\n")
}

func helpItems(s State) []string {
	const dl = "ctrl+d download dir"
	up, lr, wait := "↑/↓ choose", "←/→ choose", "please wait…"
	if s.Plain {
		up, lr, wait = "up/down choose", "left/right choose", "please wait..."
	}
	switch s.Step {
	case StepName:
		return []string{"enter next", dl, "esc/ctrl+c quit"}
	case StepExt:
		return []string{"enter select", "tab complete", up, "esc back", dl, "ctrl+c quit"}
	case StepSize:
		return []string{"enter next", "tab apply suggestion", "esc back", dl, "ctrl+c quit"}
	case StepLocation:
		return []string{"y/n answer", lr, "enter confirm", "esc back", dl, "ctrl+c quit"}
	case StepConfirm:
		return []string{"y overwrite", "n/esc back"}
	case StepUpdate:
		return []string{"y install & restart", "n/esc later", "ctrl+c quit"}
	case StepGenerating:
		return []string{wait}
	case StepDownloadDir:
		if s.DownloadCancel {
			return []string{"enter save", "tab complete", "esc cancel", "ctrl+c quit"}
		}
		return []string{"enter save", "tab complete", "esc quit"}
	default:
		return []string{"enter create another", dl, "q quit"}
	}
}

// kbd styles an item's leading key token, keeping the plain text contiguous.
func kbd(item string) string {
	key, rest, found := strings.Cut(item, " ")
	if !found {
		return keySt.Render(item)
	}
	return keySt.Render(key) + mutedSt.Render(" "+rest)
}

// wrapItems greedily packs items (joined by " · ") into lines of width <= total.
func wrapItems(items []string, total int) [][]string {
	const sep = 3 // " X "
	avail := total - 2
	var lines [][]string
	var cur []string
	w := 0
	for _, it := range items {
		iw := ansi.StringWidth(it)
		add := iw
		if len(cur) > 0 {
			add += sep
		}
		if len(cur) > 0 && w+add > avail {
			lines = append(lines, cur)
			cur, w = nil, 0
			add = iw
		}
		cur = append(cur, it)
		w += add
	}
	if len(cur) > 0 {
		lines = append(lines, cur)
	}
	return lines
}

// cell truncates content to w cells and right-pads it to exactly w.
func cell(s string, w int, ell string) string {
	if w < 0 {
		w = 0
	}
	s = ansi.Truncate(s, w, ell)
	if gap := w - ansi.StringWidth(s); gap > 0 {
		s += strings.Repeat(" ", gap)
	}
	return s
}

// fit truncates every line to width cells (0 = unlimited) as a final guard.
func fit(s string, width int, ell string) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > width {
			lines[i] = ansi.Truncate(l, width, ell)
		}
	}
	return strings.Join(lines, "\n")
}
