// Package view renders the TUI. It is a pure function of State: it holds no
// state of its own and never talks to the model.
//
// Everything is drawn inside a single rounded "card" whose width tracks the
// terminal. The brand sits in the top edge, a progress rail and the active
// step's fields fill the body, and keycap-styled shortcuts run underneath.
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
	accent  = lipgloss.AdaptiveColor{Light: "#4f46e5", Dark: "#7aa2f7"}
	accent2 = lipgloss.AdaptiveColor{Light: "#9333ea", Dark: "#bb9af7"}
	muted   = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#737aa2"}
	faint   = lipgloss.AdaptiveColor{Light: "#94a3b8", Dark: "#3b4261"}
	good    = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#9ece6a"}
	bad     = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f7768e"}
	amber   = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#e0af68"}
	paper   = lipgloss.AdaptiveColor{Light: "#ffffff", Dark: "#1a1b26"}

	brandSt   = lipgloss.NewStyle().Bold(true).Foreground(accent)
	tagSt     = lipgloss.NewStyle().Foreground(muted).Italic(true)
	frameSt   = lipgloss.NewStyle().Foreground(faint)
	activeSt  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	doneSt    = lipgloss.NewStyle().Foreground(good)
	mutedSt   = lipgloss.NewStyle().Foreground(muted)
	labelSt   = lipgloss.NewStyle().Bold(true).Foreground(accent2)
	barSt     = lipgloss.NewStyle().Foreground(accent)
	errorSt   = lipgloss.NewStyle().Foreground(bad)
	successSt = lipgloss.NewStyle().Bold(true).Foreground(good)
	extSt     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	selectSt  = lipgloss.NewStyle().Bold(true).Foreground(paper).Background(accent)
	keySt     = lipgloss.NewStyle().Bold(true).Foreground(accent)
	eicarSt   = lipgloss.NewStyle().Foreground(amber)
)

// Render draws the whole screen.
func Render(s State) string {
	total := frameWidth(s.Width)
	inner := total - 4 // two border cells and one space of padding per side

	var body []string
	if s.Step == StepDownloadDir {
		body = append(body, activeSt.Render("⚙  Settings"))
	} else {
		body = append(body, rail(s.Step, inner))
	}
	body = append(body, "")
	body = append(body, section(s, inner)...)
	if s.Error != "" {
		body = append(body, "", errorSt.Render("✗ "+s.Error))
	}
	if s.Hint != "" {
		body = append(body, activeSt.Render("➜ "+s.Hint))
	}

	var b strings.Builder
	b.WriteString(topBorder(total) + "\n")
	for _, line := range body {
		b.WriteString(bodyLine(line, inner) + "\n")
	}
	b.WriteString(bottomBorder(total) + "\n\n")
	b.WriteString(helpBlock(s, total))
	return fit(b.String(), s.Width)
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
func section(s State, inner int) []string {
	switch s.Step {
	case StepDownloadDir:
		lines := []string{heading("Standard download directory", "")}
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
	case StepName:
		return []string{
			heading("File name", ""),
			s.Input,
			mutedSt.Render("The name of the file to create; may include a directory."),
		}
	case StepExt:
		lines := []string{heading("File type", "for "+s.Name), s.Input, ""}
		return append(lines, suggestionRows(s, inner)...)
	case StepSize:
		lines := []string{
			heading("Size", s.FormatName+" → "+s.TargetPath),
			s.Input,
			mutedSt.Render(fmt.Sprintf("Empty = minimum standard-compliant size: %s (%d bytes).", s.MinSize, s.MinBytes)),
			mutedSt.Render("Units: B · KB/MB/GB ×1000 · KiB/MiB/GiB (or K/M/G) ×1024"),
		}
		if s.SizeNote != "" {
			lines = append(lines, eicarSt.Render("Note: "+s.SizeNote))
		}
		return lines
	case StepLocation:
		lines := []string{heading("Location", "for "+s.TargetPath)}
		if s.EICAR {
			lines = append(lines, eicarSt.Render("⚠ EICAR anti-malware test file — 68 bytes, the smallest possible."))
		}
		return append(lines,
			"Create the file in the current directory?",
			"",
			choiceRow(s.LocationYes, "Yes", s.CurrentDir, inner),
			choiceRow(!s.LocationYes, "No ", s.DownloadDir+"  (download folder)", inner),
		)
	case StepConfirm:
		return []string{
			errorSt.Render("⚠ " + s.TargetPath + " already exists."),
			"",
			"Overwrite it?  " + activeSt.Render("[y/N]"),
		}
	case StepGenerating:
		return []string{activeSt.Render(s.Spinner+" ") + "Generating " + s.TargetPath + mutedSt.Render("  ·  "+s.SizeLabel+" …")}
	case StepDone:
		lines := []string{
			successSt.Render("✓ Created ") + s.TargetPath,
			mutedSt.Render(fmt.Sprintf("  %s · %s (%d bytes)", s.FormatName, s.SizeLabel, s.Bytes)),
		}
		if s.EICAR {
			lines = append(lines, eicarSt.Render("  EICAR test file — scanners will flag it; this is expected."))
		}
		return lines
	}
	return nil
}

// heading renders an accent-barred section title with an optional muted note.
func heading(title, note string) string {
	h := barSt.Render("▍") + labelSt.Render(title)
	if note != "" {
		h += mutedSt.Render("  " + note)
	}
	return h
}

// rail draws the four-step progress breadcrumb, collapsing to a compact badge
// when the full form would not fit.
func rail(cur Step, inner int) string {
	names := []string{"Name", "Type", "Size", "Location"}
	segs := make([]string, len(names))
	for i, n := range names {
		switch {
		case Step(i) < cur:
			segs[i] = doneSt.Render("✓ " + n)
		case Step(i) == cur:
			segs[i] = activeSt.Render(fmt.Sprintf("● %d %s", i+1, n))
		default:
			segs[i] = mutedSt.Render(fmt.Sprintf("○ %d %s", i+1, n))
		}
	}
	full := strings.Join(segs, frameSt.Render(" ─ "))
	if ansi.StringWidth(full) <= inner {
		return full
	}
	if int(cur) < len(names) {
		return activeSt.Render(fmt.Sprintf("● %d %s", cur+1, names[cur])) + mutedSt.Render(fmt.Sprintf("  ·  step %d of 4", cur+1))
	}
	return doneSt.Render("✓ all steps complete")
}

// suggestionRows renders the filterable file-type list, right-aligning each
// row's category and minimum size and highlighting the selected entry.
func suggestionRows(s State, inner int) []string {
	if len(s.Suggestions) == 0 {
		return []string{mutedSt.Render("  no matching file type — keep typing")}
	}
	out := make([]string, 0, len(s.Suggestions)+1)
	for _, sg := range s.Suggestions {
		ext := fmt.Sprintf("%-5s", sg.Ext)
		meta := sg.Category + " · min " + sg.MinSize
		if sg.Selected {
			left := "▸ " + ext + "  " + sg.Name
			out = append(out, selectSt.Render(pad(left, meta, inner)))
			continue
		}
		left := "  " + extSt.Render(ext) + "  " + sg.Name
		out = append(out, padStyled(left, mutedSt.Render(meta), inner))
	}
	if s.MoreSuggestions > 0 {
		out = append(out, mutedSt.Render(fmt.Sprintf("  … %d more, keep typing to narrow", s.MoreSuggestions)))
	}
	return out
}

// choiceRow renders one option of the location question.
func choiceRow(selected bool, label, dir string, inner int) string {
	if selected {
		return selectSt.Render(cell("▸ "+label+"  → "+dir, inner))
	}
	return "  " + label + mutedSt.Render("  → "+dir)
}

// pad joins a plain left and right segment flush to width w.
func pad(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return cell(left+"  "+right, w)
	}
	return cell(left+strings.Repeat(" ", gap)+right, w)
}

// padStyled is pad for already-styled (ANSI-bearing) segments.
func padStyled(left, right string, w int) string {
	gap := w - ansi.StringWidth(left) - ansi.StringWidth(right)
	if gap < 2 {
		return left + "  " + right
	}
	return left + strings.Repeat(" ", gap) + right
}

// --- frame ---

func topBorder(total int) string {
	label := brandSt.Render("bbdx_file_gen")
	tag := "standard-compliant test files"
	if 5+ansi.StringWidth(label)+3+len(tag)+1 <= total {
		label += "  " + tagSt.Render(tag)
	}
	if lw := ansi.StringWidth(label); lw > total-6 {
		label = ansi.Truncate(label, total-6, "…")
	}
	dashes := total - 5 - ansi.StringWidth(label)
	if dashes < 1 {
		dashes = 1
	}
	return frameSt.Render("╭─ ") + label + frameSt.Render(" "+strings.Repeat("─", dashes)+"╮")
}

func bottomBorder(total int) string {
	return frameSt.Render("╰" + strings.Repeat("─", total-2) + "╯")
}

func bodyLine(content string, inner int) string {
	return frameSt.Render("│") + " " + cell(content, inner) + " " + frameSt.Render("│")
}

// --- help ---

func helpBlock(s State, total int) string {
	lines := wrapItems(helpItems(s), total)
	rendered := make([]string, len(lines))
	for i, items := range lines {
		caps := make([]string, len(items))
		for j, it := range items {
			caps[j] = kbd(it)
		}
		rendered[i] = "  " + strings.Join(caps, frameSt.Render(" · "))
	}
	return strings.Join(rendered, "\n")
}

func helpItems(s State) []string {
	const dl = "ctrl+d download dir"
	switch s.Step {
	case StepName:
		return []string{"enter next", dl, "esc/ctrl+c quit"}
	case StepExt:
		return []string{"enter select", "tab complete", "↑/↓ choose", "esc back", dl, "ctrl+c quit"}
	case StepSize:
		return []string{"enter next", "tab apply suggestion", "esc back", dl, "ctrl+c quit"}
	case StepLocation:
		return []string{"y/n answer", "←/→ choose", "enter confirm", "esc back", dl, "ctrl+c quit"}
	case StepConfirm:
		return []string{"y overwrite", "n/esc back"}
	case StepGenerating:
		return []string{"please wait…"}
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
	const sep = 3 // " · "
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
func cell(s string, w int) string {
	if w < 0 {
		w = 0
	}
	s = ansi.Truncate(s, w, "…")
	if g := w - ansi.StringWidth(s); g > 0 {
		s += strings.Repeat(" ", g)
	}
	return s
}

// fit truncates every line to width cells (0 = unlimited) as a final guard.
func fit(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if ansi.StringWidth(l) > width {
			lines[i] = ansi.Truncate(l, width, "…")
		}
	}
	return strings.Join(lines, "\n")
}
