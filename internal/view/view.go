// Package view renders the TUI. It is a pure function of State: it holds no
// state of its own and never talks to the model.
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

	SizeLabel string // human readable size of the file being/just generated
	Bytes     int64
	Spinner   string

	Error string
	Hint  string
}

var (
	accent    = lipgloss.AdaptiveColor{Light: "#1d4ed8", Dark: "#7aa2f7"}
	muted     = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#737aa2"}
	good      = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#9ece6a"}
	bad       = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#f7768e"}
	titleSt   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Background(accent).Padding(0, 1)
	activeSt  = lipgloss.NewStyle().Bold(true).Foreground(accent)
	doneSt    = lipgloss.NewStyle().Foreground(good)
	mutedSt   = lipgloss.NewStyle().Foreground(muted)
	labelSt   = lipgloss.NewStyle().Bold(true)
	errorSt   = lipgloss.NewStyle().Foreground(bad)
	successSt = lipgloss.NewStyle().Bold(true).Foreground(good)
	selectSt  = lipgloss.NewStyle().Bold(true).Foreground(accent)
)

// Render draws the whole screen.
func Render(s State) string {
	var b strings.Builder
	b.WriteString(titleSt.Render("bbdx_file_gen") + mutedSt.Render("  standard-compliant test files") + "\n")
	b.WriteString(steps(s.Step) + "\n\n")

	switch s.Step {
	case StepName:
		b.WriteString(labelSt.Render("File name") + "\n")
		b.WriteString(s.Input + "\n")
		b.WriteString(mutedSt.Render("Name of the file to create; may include a directory.") + "\n")
	case StepExt:
		b.WriteString(labelSt.Render("File type") + mutedSt.Render("  for "+s.Name) + "\n")
		b.WriteString(s.Input + "\n\n")
		b.WriteString(suggestions(s))
	case StepSize:
		b.WriteString(labelSt.Render("Size") + mutedSt.Render("  "+s.FormatName+" → "+s.TargetPath) + "\n")
		b.WriteString(s.Input + "\n")
		b.WriteString(mutedSt.Render(fmt.Sprintf("Leave empty for the minimum standard-compliant size: %s (%d bytes)", s.MinSize, s.MinBytes)) + "\n")
		b.WriteString(mutedSt.Render("Units: B, KB, MB, GB (×1000) · KiB, MiB, GiB or K, M, G (×1024)") + "\n")
		if s.SizeNote != "" {
			b.WriteString(mutedSt.Render("Note: "+s.SizeNote) + "\n")
		}
	case StepLocation:
		b.WriteString(labelSt.Render("Location") + mutedSt.Render("  for "+s.TargetPath) + "\n")
		b.WriteString("Create the file in the current directory?\n")
		b.WriteString(choice(s.LocationYes, "Yes", s.CurrentDir) + "\n")
		b.WriteString(choice(!s.LocationYes, "No ", s.DownloadDir+"  (download folder)") + "\n")
	case StepConfirm:
		b.WriteString(errorSt.Render(s.TargetPath+" already exists.") + " Overwrite? " + labelSt.Render("[y/N]") + "\n")
	case StepGenerating:
		b.WriteString(activeSt.Render(s.Spinner) + " Generating " + s.TargetPath + " (" + s.SizeLabel + ") …\n")
	case StepDone:
		b.WriteString(successSt.Render("✓ Created "+s.TargetPath) + "\n")
		b.WriteString(fmt.Sprintf("  %s · %s (%d bytes)\n", s.FormatName, s.SizeLabel, s.Bytes))
	}

	if s.Error != "" {
		b.WriteString("\n" + errorSt.Render("✗ "+s.Error) + "\n")
	}
	if s.Hint != "" {
		b.WriteString(activeSt.Render(s.Hint) + "\n")
	}
	b.WriteString("\n" + mutedSt.Render(help(s.Step)))
	return fit(b.String(), s.Width)
}

func steps(cur Step) string {
	names := []string{"Name", "Type", "Size", "Location"}
	parts := make([]string, len(names))
	for i, n := range names {
		label := fmt.Sprintf("%d %s", i+1, n)
		switch {
		case Step(i) < cur:
			parts[i] = doneSt.Render("✓ " + n)
		case Step(i) == cur:
			parts[i] = activeSt.Render("● " + label)
		default:
			parts[i] = mutedSt.Render("○ " + label)
		}
	}
	return strings.Join(parts, mutedSt.Render("  ›  "))
}

func suggestions(s State) string {
	if len(s.Suggestions) == 0 {
		return mutedSt.Render("  no matching file type") + "\n"
	}
	nameWidth := 0
	for _, sg := range s.Suggestions {
		nameWidth = max(nameWidth, len(sg.Name))
	}
	var b strings.Builder
	for _, sg := range s.Suggestions {
		line := fmt.Sprintf("%-6s %-*s  %-20s min %s", sg.Ext, nameWidth, sg.Name, sg.Category, sg.MinSize)
		if sg.Selected {
			b.WriteString(selectSt.Render("▸ "+line) + "\n")
		} else {
			b.WriteString("  " + line + "\n")
		}
	}
	if s.MoreSuggestions > 0 {
		b.WriteString(mutedSt.Render(fmt.Sprintf("  … %d more, keep typing or scroll", s.MoreSuggestions)) + "\n")
	}
	return b.String()
}

func choice(selected bool, label, dir string) string {
	if selected {
		return selectSt.Render("▸ "+label) + "  → " + dir
	}
	return "  " + label + mutedSt.Render("  → "+dir)
}

func help(step Step) string {
	switch step {
	case StepName:
		return "enter next · esc/ctrl+c quit"
	case StepExt:
		return "enter select · tab complete · ↑/↓ choose · esc back · ctrl+c quit"
	case StepSize:
		return "enter next · tab apply suggestion · esc back · ctrl+c quit"
	case StepLocation:
		return "y/n answer · ←/→ choose · enter confirm · esc back · ctrl+c quit"
	case StepConfirm:
		return "y overwrite · n/esc back"
	case StepGenerating:
		return "please wait…"
	default:
		return "enter create another · q quit"
	}
}

// fit truncates every line to width cells (0 = unlimited).
func fit(s string, width int) string {
	if width <= 0 {
		return s
	}
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, width, "…")
	}
	return strings.Join(lines, "\n")
}
