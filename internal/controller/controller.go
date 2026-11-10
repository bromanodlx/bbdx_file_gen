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

	"bbdx_file_gen/internal/model"
	"bbdx_file_gen/internal/view"
)

const visibleSuggestions = 8

var spinnerFrames = []string{"⣾", "⣽", "⣻", "⢿", "⡿", "⣟", "⣯", "⣷"}

type (
	generatedMsg struct {
		res model.Result
		err error
	}
	spinnerTick struct{}
)

// Controller holds the wizard state.
type Controller struct {
	svc *model.Service

	step                           view.Step
	nameInput, extInput, sizeInput textinput.Model

	matches []model.Format
	sel     int
	offset  int

	format  model.Format
	target  string
	size    int64
	suggest int64 // nearest valid size offered after a SizeError, -1 if none
	errMsg  string
	result  model.Result
	frame   int
	width   int
	height  int
}

// New creates a controller that generates files through svc.
func New(svc *model.Service) *Controller {
	c := &Controller{
		svc:       svc,
		nameInput: newInput("report"),
		extInput:  newInput("pdf, png, docx, …"),
		sizeInput: newInput(""),
		suggest:   -1,
	}
	c.nameInput.Focus()
	c.refreshMatches()
	return c
}

func newInput(placeholder string) textinput.Model {
	ti := textinput.New()
	ti.Prompt = "> "
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
			c.step = view.StepSize
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
		switch c.step {
		case view.StepName:
			return c.updateName(msg)
		case view.StepExt:
			return c.updateExt(msg)
		case view.StepSize:
			return c.updateSize(msg)
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
		target, err := c.svc.TargetPath(c.nameInput.Value(), f.Ext)
		if err != nil {
			c.errMsg = err.Error()
			return c, nil
		}
		c.format, c.target = f, target
		c.extInput.SetValue(f.Ext)
		c.refreshMatches()
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
		if _, err := os.Stat(c.target); err == nil {
			c.goTo(view.StepConfirm)
			return c, nil
		}
		return c, c.generate(false)
	}
	return c, c.edit(&c.sizeInput, msg)
}

func (c *Controller) updateConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.String() == "y" || msg.String() == "Y":
		return c, c.generate(true)
	case msg.Type == tea.KeyEsc || msg.Type == tea.KeyEnter || msg.String() == "n" || msg.String() == "N":
		c.goTo(view.StepSize)
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
	req := model.Request{Name: c.nameInput.Value(), Ext: c.format.Ext, Size: &size, Overwrite: overwrite}
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
	for _, in := range []*textinput.Model{&c.nameInput, &c.extInput, &c.sizeInput} {
		in.Blur()
	}
	switch step {
	case view.StepName:
		c.nameInput.Focus()
	case view.StepExt:
		c.extInput.Focus()
	case view.StepSize:
		c.sizeInput.Focus()
	}
}

func (c *Controller) reset() {
	c.nameInput.Reset()
	c.extInput.Reset()
	c.sizeInput.Reset()
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
		Spinner: spinnerFrames[c.frame%len(spinnerFrames)],
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
	case view.StepGenerating:
		s.SizeLabel = model.HumanSize(c.size)
	case view.StepDone:
		s.TargetPath, s.Bytes = c.result.Path, c.result.Size
		s.SizeLabel = model.HumanSize(c.result.Size)
	}
	return s
}
