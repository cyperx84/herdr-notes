// Package tui provides the Bubble Tea preview/editor for one scratch note.
package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"

	"github.com/cyperx84/herdr-notes/internal/herdripc"
	"github.com/cyperx84/herdr-notes/internal/store"
)

const debounce = 1200 * time.Millisecond

type mode uint8

const (
	preview mode = iota
	edit
)

type saveTick struct{ generation uint64 }
type heartbeatTick struct{}
type externalDone struct{ err error }

// Model is the complete application state.
type Model struct {
	store      *store.Store
	editorArgv []string
	text       string
	area       textarea.Model
	view       viewport.Model
	mode       mode
	confirm    bool
	dirty      bool
	generation uint64
	width      int
	height     int
	status     string
	paneID     string
	err        error
}

// New loads a note and creates a preview-first model.
func New(s *store.Store, editorArgv []string, paneID string) (*Model, error) {
	text, err := s.Load()
	if err != nil {
		return nil, err
	}
	a := textarea.New()
	a.SetValue(text)
	a.Prompt = ""
	a.ShowLineNumbers = false
	a.CharLimit = 0
	v := viewport.New(80, 20)
	m := &Model{store: s, editorArgv: editorArgv, text: text, area: a, view: v, mode: preview, paneID: paneID}
	m.renderPreview()
	return m, nil
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(heartbeatCmd(m.paneID), heartbeatAfter(), tea.EnterAltScreen)
}

func heartbeatCmd(paneID string) tea.Cmd {
	return func() tea.Msg {
		if paneID != "" {
			_ = herdripc.Stamp(paneID, time.Now())
		}
		return nil
	}
}

func heartbeatAfter() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return heartbeatTick{} })
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		body := max(1, msg.Height-2)
		m.view.Width, m.view.Height = msg.Width, body
		m.area.SetWidth(msg.Width)
		m.area.SetHeight(body)
		m.renderPreview()
		return m, nil
	case heartbeatTick:
		return m, tea.Batch(heartbeatCmd(m.paneID), heartbeatAfter())
	case saveTick:
		if m.dirty && msg.generation == m.generation {
			m.commitEdit()
			m.save()
		}
		return m, nil
	case externalDone:
		if msg.err != nil {
			m.err = msg.err
			m.status = "external editor failed"
			return m, nil
		}
		text, err := m.store.Load()
		if err != nil {
			m.err, m.status = err, "reload failed"
		} else {
			m.text = text
			m.area.SetValue(text)
			m.renderPreview()
			m.status = "reloaded"
		}
		return m, nil
	case tea.KeyMsg:
		if m.confirm {
			if msg.String() == "y" || msg.String() == "Y" {
				m.text = ""
				m.area.SetValue("")
				m.view.GotoTop()
				m.save()
			}
			m.confirm = false
			return m, nil
		}
		if m.mode == preview {
			return m.previewKey(msg)
		}
		return m.editKey(msg)
	}
	if m.mode == edit {
		before := m.area.Value()
		var cmd tea.Cmd
		m.area, cmd = m.area.Update(msg)
		if m.area.Value() != before {
			return m, tea.Batch(cmd, m.touch())
		}
		return m, cmd
	}
	var cmd tea.Cmd
	m.view, cmd = m.view.Update(msg)
	return m, cmd
}

func (m *Model) previewKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "ctrl+c":
		m.save()
		return m, tea.Quit
	case "e", "enter":
		m.mode = edit
		m.area.SetValue(m.text)
		m.area.Focus()
		return m, textarea.Blink
	case "E":
		if len(m.editorArgv) == 0 {
			m.status = "no editor configured"
			return m, nil
		}
		m.save()
		cmd := exec.Command(m.editorArgv[0], append(m.editorArgv[1:], m.store.Path)...)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return externalDone{err: err} })
	case "x":
		m.confirm = true
	case "g", "home":
		m.view.GotoTop()
	case "G", "end":
		m.view.GotoBottom()
	default:
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(key)
		return m, cmd
	}
	return m, nil
}

func (m *Model) editKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.commitEdit()
		m.mode = preview
		m.area.Blur()
		m.save()
		m.renderPreview()
		return m, nil
	case "ctrl+s":
		m.commitEdit()
		m.save()
		return m, nil
	case "ctrl+c":
		m.commitEdit()
		m.save()
		return m, tea.Quit
	}
	before := m.area.Value()
	var cmd tea.Cmd
	m.area, cmd = m.area.Update(key)
	if m.area.Value() != before {
		return m, tea.Batch(cmd, m.touch())
	}
	return m, cmd
}

func (m *Model) touch() tea.Cmd {
	m.dirty = true
	m.generation++
	generation := m.generation
	return tea.Tick(debounce, func(time.Time) tea.Msg { return saveTick{generation: generation} })
}

func (m *Model) commitEdit() { m.text = m.area.Value() }

func (m *Model) save() {
	if err := m.store.Save(m.text); err != nil {
		m.err, m.status = err, "save failed"
		return
	}
	m.dirty = false
	m.err = nil
	m.status = "saved"
}

func (m *Model) renderPreview() {
	width := max(20, m.width-2)
	content := m.text
	if strings.TrimSpace(content) == "" {
		content = "*(empty note)*\n\n`e`/`Enter` edit · `E` Neovim/external editor · `x` clear · `q` quit"
	}
	renderer, err := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(width))
	if err != nil {
		m.view.SetContent(content)
		return
	}
	rendered, err := renderer.Render(content)
	if err != nil {
		rendered = content
	}
	m.view.SetContent(strings.TrimSpace(rendered))
}

func (m *Model) View() string {
	if m.width == 0 {
		return ""
	}
	label := "preview"
	body := m.view.View()
	hint := "e/Enter edit  E external  ↑/↓ PgUp/PgDn g/G scroll  x clear  q quit"
	if m.mode == edit {
		label, body, hint = "edit", m.area.View(), "Esc preview+save  Ctrl+S save"
	}
	status := m.status
	if m.dirty {
		status = "unsaved"
	}
	header := lipgloss.NewStyle().Bold(true).Render("Notes") + "  " + label
	if status != "" {
		header += "  " + lipgloss.NewStyle().Faint(true).Render(status)
	}
	footer := lipgloss.NewStyle().Faint(true).Render(hint)
	if m.confirm {
		footer = lipgloss.NewStyle().Bold(true).Render("Clear this workspace note? y/N")
	}
	if m.err != nil {
		footer = lipgloss.NewStyle().Render(fmt.Sprintf("%s: %v", footer, m.err))
	}
	return header + "\n" + body + "\n" + footer
}

// Finalize synchronously commits and saves before the terminal is restored.
func (m *Model) Finalize() error {
	if m.mode == edit {
		m.commitEdit()
	}
	return m.store.Save(m.text)
}
