// Package controller wires user input to the model and hands view state to
// the view. It implements tea.Model so Bubble Tea can drive it.
package controller

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/cursor"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"bbdx_file_gen/internal/model"
	"bbdx_file_gen/internal/view"
)

const visibleSuggestions = 8

var (
	spinnerFrames      = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}
	spinnerFramesASCII = []string{"|", "/", "-", "\\"}
)

type (
	generatedMsg struct {
		res model.Result
		err error
	}
	spinnerTick struct{}
)

// Settings persists the user configuration (model.ConfigStore in production).
type Settings interface {
	Load() (model.Config, error)
	Save(model.Config) error
}

// Setup is the environment the controller works in.
type Setup struct {
	Current   string // the working directory ("Yes" in the location question)
	Home      string // expands ~ in paths
	Suggested string // proposal shown when no download directory is configured
	Store     Settings
	Plain     bool // ASCII-only glyphs and no color
}

// Controller holds the wizard state.
type Controller struct {
	svc   *model.Service
	setup Setup

	plain   bool
	spinner []string

	step                           view.Step
	nameInput, extInput, sizeInput textinput.Model
	dlInput                        textinput.Model

	downloads string    // the configured, verified standard download directory
	dlReturn  view.Step // step to return to after the download directory screen
	dlCancel  bool      // the download directory screen can be left with esc
	dlReason  string

	matches []model.Format
	sel     int
	offset  int

	format  model.Format
	target  string // final path once the location is known, else the bare file name
	dir     string // chosen base directory
	eicar   bool   // the current file is an EICAR test file (fixed 68 bytes)
	locYes  bool   // "Yes" highlighted in the location question
	size    int64
	suggest int64 // nearest valid size offered after a SizeError, -1 if none
	errMsg  string
	result  model.Result
	frame   int
	width   int
	height  int
}

// New creates a controller that generates files through svc. If no reachable
// standard download directory is configured, it starts by asking for one.
func New(svc *model.Service, setup Setup) *Controller {
	placeholder := "pdf, png, docx, …"
	spinner := spinnerFrames
	if setup.Plain {
		placeholder = "pdf, png, docx, ..."
		spinner = spinnerFramesASCII
	}
	c := &Controller{
		svc:       svc,
		setup:     setup,
		plain:     setup.Plain,
		spinner:   spinner,
		nameInput: newInput("report", setup.Plain),
		extInput:  newInput(placeholder, setup.Plain),
		sizeInput: newInput("", setup.Plain),
		dlInput:   newInput("~/Downloads", setup.Plain),
		suggest:   -1,
	}
	c.refreshMatches()
	c.goTo(view.StepName)
	cfg, err := setup.Store.Load()
	switch {
	case err != nil:
		c.askDownloadDir(fmt.Sprintf("The configuration could not be read:\n  %v", err), setup.Suggested)
	case cfg.DownloadDir == "":
		c.askDownloadDir("No standard download directory is configured yet.", setup.Suggested)
	default:
		if err := model.CheckDir(cfg.DownloadDir); err != nil {
			c.askDownloadDir(fmt.Sprintf("The configured download directory is not reachable:\n  %v", err), cfg.DownloadDir)
		} else {
			c.downloads = cfg.DownloadDir
		}
	}
	return c
}

// askDownloadDir shows the mandatory download directory screen.
func (c *Controller) askDownloadDir(reason, prefill string) {
	c.dlReason, c.dlCancel, c.dlReturn = reason, false, view.StepName
	c.dlInput.SetValue(prefill)
	c.dlInput.CursorEnd()
	c.goTo(view.StepDownloadDir)
}

// changeDownloadDir opens the download directory screen from the current step.
func (c *Controller) changeDownloadDir() {
	c.dlReason, c.dlCancel, c.dlReturn = "", true, c.step
	c.dlInput.SetValue(c.downloads)
	c.dlInput.CursorEnd()
	c.goTo(view.StepDownloadDir)
}

func (c *Controller) updateDownloadDir(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		if !c.dlCancel {
			return c, tea.Quit
		}
		c.goTo(c.dlReturn)
		return c, nil
	case tea.KeyTab:
		c.dlInput.SetValue(model.CompleteDir(c.dlInput.Value(), c.setup.Current, c.setup.Home))
		c.dlInput.CursorEnd()
		c.clearMessages()
		return c, nil
	case tea.KeyEnter:
		dir, err := model.SetDownloadDir(c.setup.Store, c.dlInput.Value(), c.setup.Current, c.setup.Home)
		if err != nil {
			c.errMsg = err.Error()
			return c, nil
		}
		c.downloads = dir
		c.goTo(c.dlReturn)
		return c, nil
	}
	return c, c.edit(&c.dlInput, msg)
}

var (
	promptStyle      = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#4f46e5", Dark: "#7aa2f7"})
	placeholderStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#94a3b8", Dark: "#565f89"})
	cursorStyle      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9333ea", Dark: "#bb9af7"})
)

func newInput(placeholder string, plain bool) textinput.Model {
	ti := textinput.New()
	ti.Prompt = "❯ "
	ti.PromptStyle = promptStyle
	ti.PlaceholderStyle = placeholderStyle
	ti.Cursor.Style = cursorStyle
	if plain {
		ti.Prompt = "> "
		ti.PromptStyle = lipgloss.NewStyle()
		ti.PlaceholderStyle = lipgloss.NewStyle()
		ti.Cursor.Style = lipgloss.NewStyle()
	}
	ti.Placeholder = placeholder
	ti.CharLimit = 4096
	ti.Cursor.SetMode(cursor.CursorStatic)
	return ti
}

func (c *Controller) Init() tea.Cmd { return nil }

func (c *Controller) View() string { return view.Render(c.State()) }

func (c *Controller) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width, c.height = msg.Width, msg.Height
		return c, nil
	case spinnerTick:
		if c.step != view.StepGenerating {
			return c, nil
		}
		c.frame++
		return c, tick()
	case generatedMsg:
		if msg.err != nil {
			c.goTo(c.backFromGenerate())
			c.errMsg = msg.err.Error()
			return c, nil
		}
		c.result = msg.res
		c.step = view.StepDone
		return c, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC {
			return c, tea.Quit
		}
		if msg.Type == tea.KeyCtrlD {
			switch c.step {
			case view.StepName, view.StepExt, view.StepSize, view.StepLocation, view.StepDone:
				c.changeDownloadDir()
			}
			return c, nil
		}
		switch c.step {
		case view.StepDownloadDir:
			return c.updateDownloadDir(msg)
		case view.StepName:
			return c.updateName(msg)
		case view.StepExt:
			return c.updateExt(msg)
		case view.StepSize:
			return c.updateSize(msg)
		case view.StepLocation:
			return c.updateLocation(msg)
		case view.StepConfirm:
			return c.updateConfirm(msg)
		case view.StepDone:
			return c.updateDone(msg)
		}
	}
	return c, nil
}

func (c *Controller) updateName(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		return c, tea.Quit
	case tea.KeyEnter:
		name := strings.TrimSpace(c.nameInput.Value())
		if err := model.ValidateName(name); err != nil {
			c.errMsg = err.Error()
			return c, nil
		}
		if c.extInput.Value() == "" {
			if f, ok := c.svc.Registry.Lookup(filepath.Ext(name)); ok && filepath.Ext(name) != "" {
				c.extInput.SetValue(f.Ext)
				c.refreshMatches()
			}
		}
		c.goTo(view.StepExt)
		return c, nil
	}
	return c, c.edit(&c.nameInput, msg)
}

func (c *Controller) updateExt(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		c.goTo(view.StepName)
		return c, nil
	case tea.KeyUp:
		c.moveSelection(-1)
		return c, nil
	case tea.KeyDown:
		c.moveSelection(1)
		return c, nil
	case tea.KeyTab:
		if len(c.matches) > 0 {
			c.extInput.SetValue(c.matches[c.sel].Ext)
			c.extInput.CursorEnd()
			c.refreshMatches()
		}
		return c, nil
	case tea.KeyEnter:
		f, ok := c.svc.Registry.Lookup(c.extInput.Value())
		if !ok && len(c.matches) > 0 {
			f, ok = c.matches[c.sel], true
		}
		if !ok {
			c.errMsg = fmt.Sprintf("unknown file type %q (%d types supported)", model.NormalizeExt(c.extInput.Value()), len(c.svc.Registry.All()))
			return c, nil
		}
		target, err := c.svc.TargetPath("", c.nameInput.Value(), f.Ext)
		if err != nil {
			c.errMsg = err.Error()
			return c, nil
		}
		c.format, c.target = f, target
		c.extInput.SetValue(f.Ext)
		c.refreshMatches()
		// An "eicar" name has a fixed 68-byte content, so there is no size to ask.
		if c.eicar = model.IsEICARName(c.nameInput.Value()); c.eicar {
			c.size = int64(len(model.EICAR))
			return c, c.proceedToLocation()
		}
		c.sizeInput.Placeholder = fmt.Sprintf("%d (minimum)", f.Rule.Min)
		c.goTo(view.StepSize)
		return c, nil
	}
	cmd := c.edit(&c.extInput, msg)
	c.refreshMatches()
	return c, cmd
}

func (c *Controller) updateSize(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		c.goTo(view.StepExt)
		return c, nil
	case tea.KeyTab:
		if c.suggest >= 0 {
			c.sizeInput.SetValue(strconv.FormatInt(c.suggest, 10))
			c.sizeInput.CursorEnd()
			c.clearMessages()
		}
		return c, nil
	case tea.KeyEnter:
		size := c.format.Rule.Min
		if v := strings.TrimSpace(c.sizeInput.Value()); v != "" {
			n, err := model.ParseSize(v)
			if err != nil {
				c.errMsg = err.Error()
				return c, nil
			}
			size = n
		}
		if err := c.format.Rule.Check(size); err != nil {
			var se *model.SizeError
			errors.As(err, &se)
			se.Ext = c.format.Ext
			c.errMsg = se.Error()
			c.suggest = se.Above
			if c.suggest < 0 {
				c.suggest = se.Below
			}
			return c, nil
		}
		c.size = size
		return c, c.proceedToLocation()
	}
	return c, c.edit(&c.sizeInput, msg)
}

// proceedToLocation moves to the location question once the size is known, or
// generates straight away when the name is an absolute path.
func (c *Controller) proceedToLocation() tea.Cmd {
	if c.absoluteName() {
		c.dir = ""
		return c.confirmOrGenerate()
	}
	c.target = model.FileName(c.nameInput.Value(), c.format.Ext)
	c.locYes = true
	c.goTo(view.StepLocation)
	return nil
}

func (c *Controller) updateLocation(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEsc:
		c.goTo(c.backFromLocation())
	case msg.Type == tea.KeyLeft || msg.Type == tea.KeyUp:
		c.locYes = true
	case msg.Type == tea.KeyRight || msg.Type == tea.KeyDown || msg.Type == tea.KeyTab:
		c.locYes = !c.locYes
	case msg.String() == "y" || msg.String() == "Y":
		return c, c.chooseLocation(true)
	case msg.String() == "n" || msg.String() == "N":
		return c, c.chooseLocation(false)
	case msg.Type == tea.KeyEnter:
		return c, c.chooseLocation(c.locYes)
	}
	return c, nil
}

// chooseLocation answers "create in the current directory?".
func (c *Controller) chooseLocation(yes bool) tea.Cmd {
	c.locYes = yes
	c.dir = c.setup.Current
	if !yes {
		c.dir = c.downloads
	}
	return c.confirmOrGenerate()
}

func (c *Controller) confirmOrGenerate() tea.Cmd {
	target, err := c.svc.TargetPath(c.dir, c.nameInput.Value(), c.format.Ext)
	if err != nil {
		c.errMsg = err.Error()
		return nil
	}
	c.target = target
	if _, err := os.Stat(target); err == nil {
		c.goTo(view.StepConfirm)
		return nil
	}
	return c.generate(false)
}

func (c *Controller) absoluteName() bool {
	return filepath.IsAbs(strings.TrimSpace(c.nameInput.Value()))
}

// backFromLocation is the step the location question returns to; an EICAR file
// has no size step in between.
func (c *Controller) backFromLocation() view.Step {
	if c.eicar {
		return view.StepExt
	}
	return view.StepSize
}

// backFromGenerate is the step to return to when generation is declined or fails.
func (c *Controller) backFromGenerate() view.Step {
	if c.absoluteName() {
		return c.backFromLocation()
	}
	return view.StepLocation
}

func (c *Controller) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "y" || msg.String() == "Y":
		return c, c.generate(true)
	case msg.Type == tea.KeyEsc || msg.Type == tea.KeyEnter || msg.String() == "n" || msg.String() == "N":
		c.goTo(c.backFromGenerate())
	}
	return c, nil
}

func (c *Controller) updateDone(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.Type == tea.KeyEnter:
		c.reset()
	case msg.Type == tea.KeyEsc || msg.String() == "q":
		return c, tea.Quit
	}
	return c, nil
}

func (c *Controller) generate(overwrite bool) tea.Cmd {
	c.goTo(view.StepGenerating)
	size := c.size
	req := model.Request{
		Dir:  c.dir,
		Name: c.nameInput.Value(), Ext: c.format.Ext, Size: &size, Overwrite: overwrite,
	}
	svc := c.svc
	return tea.Batch(func() tea.Msg {
		res, err := svc.Create(req)
		return generatedMsg{res, err}
	}, tick())
}

func tick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return spinnerTick{} })
}

// edit forwards a key to an input and clears stale messages.
func (c *Controller) edit(in *textinput.Model, msg tea.KeyMsg) tea.Cmd {
	before := in.Value()
	var cmd tea.Cmd
	*in, cmd = in.Update(msg)
	if in.Value() != before {
		c.clearMessages()
	}
	return cmd
}

func (c *Controller) clearMessages() {
	c.errMsg = ""
	c.suggest = -1
}

func (c *Controller) goTo(step view.Step) {
	c.step = step
	c.clearMessages()
	for _, in := range []*textinput.Model{&c.nameInput, &c.extInput, &c.sizeInput, &c.dlInput} {
		in.Blur()
	}
	switch step {
	case view.StepName:
		c.nameInput.Focus()
	case view.StepExt:
		c.extInput.Focus()
	case view.StepSize:
		c.sizeInput.Focus()
	case view.StepDownloadDir:
		c.dlInput.Focus()
	}
}

func (c *Controller) reset() {
	c.nameInput.Reset()
	c.extInput.Reset()
	c.sizeInput.Reset()
	c.eicar = false
	c.refreshMatches()
	c.goTo(view.StepName)
}

func (c *Controller) refreshMatches() {
	c.matches = c.svc.Registry.Search(c.extInput.Value())
	c.sel, c.offset = 0, 0
}

func (c *Controller) moveSelection(delta int) {
	c.sel = max(0, min(len(c.matches)-1, c.sel+delta))
	if c.sel < c.offset {
		c.offset = c.sel
	}
	if c.sel >= c.offset+visibleSuggestions {
		c.offset = c.sel - visibleSuggestions + 1
	}
}

// State builds the data the view renders.
func (c *Controller) State() view.State {
	s := view.State{
		Step: c.step, Width: c.width, Height: c.height,
		Name: strings.TrimSpace(c.nameInput.Value()), Error: c.errMsg,
		Ext: c.format.Ext, FormatName: c.format.Name, TargetPath: c.target,
		Spinner:    c.spinner[c.frame%len(c.spinner)],
		CurrentDir: c.setup.Current, DownloadDir: c.downloads, LocationYes: c.locYes,
		DownloadReason: c.dlReason, DownloadCancel: c.dlCancel, EICAR: c.eicar, Plain: c.plain,
	}
	if c.format.Ext != "" {
		s.MinSize, s.MinBytes, s.SizeNote = model.HumanSize(c.format.Rule.Min), c.format.Rule.Min, c.format.Rule.Note
	}
	if c.suggest >= 0 {
		s.Hint = fmt.Sprintf("tab: use %d bytes", c.suggest)
	}
	switch c.step {
	case view.StepName:
		s.Input = c.nameInput.View()
	case view.StepExt:
		s.Input = c.extInput.View()
		end := min(len(c.matches), c.offset+visibleSuggestions)
		for i := c.offset; i < end; i++ {
			f := c.matches[i]
			s.Suggestions = append(s.Suggestions, view.Suggestion{
				Ext: f.Ext, Name: f.Name, Category: string(f.Category),
				MinSize: model.HumanSize(f.Rule.Min), Selected: i == c.sel,
			})
		}
		s.MoreSuggestions = len(c.matches) - (end - c.offset)
	case view.StepSize:
		s.Input = c.sizeInput.View()
	case view.StepDownloadDir:
		s.Input = c.dlInput.View()
	case view.StepGenerating:
		s.SizeLabel = model.HumanSize(c.size)
	case view.StepDone:
		s.TargetPath, s.Bytes = c.result.Path, c.result.Size
		s.SizeLabel = model.HumanSize(c.result.Size)
	}
	return s
}
