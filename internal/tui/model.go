// Package tui provides the Bubble Tea model for browsing an OKF bundle inside
// a herdr pane.
//
// The model is a pure client of notes.App: it never computes a path, parses
// frontmatter, or writes a file directly. Editing is deliberately delegated
// to the external editor rather than done in-pane, because the CLI already
// owns every write path and duplicating it here would be the third place the
// same logic lived.
package tui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/cyperx84/herdr-notes/internal/herdripc"
	"github.com/cyperx84/herdr-notes/internal/notes"
	"github.com/cyperx84/herdr-notes/internal/render"
)

type mode uint8

const (
	modePage mode = iota
	modeList
)

// listSource controls what the list view shows.
type listSource uint8

const (
	srcAll listSource = iota
	srcBacklinks
)

type heartbeatTick struct{}
type editDone struct{ err error }

type pageRow struct {
	Path  string
	Title string
}

// Model is the pane.
type Model struct {
	app        *notes.App
	editorArgv []string
	paneID     string

	mode   mode
	source listSource

	current string // bundle-relative path of the page being viewed

	listPages   []pageRow
	listCursor  int
	listFilter  string
	listChanged bool

	view   viewport.Model
	render *render.Renderer
	width  int
	height int
	status string
}

// New builds a Model showing the scope's current page.
func New(app *notes.App, editorArgv []string, paneID string) *Model {
	v := viewport.New(80, 20)
	m := &Model{
		app:        app,
		editorArgv: editorArgv,
		paneID:     paneID,
		mode:       modePage,
		current:    app.Store.Resolve(app.Current()),
		view:       v,
		render:     render.New(80),
	}
	m.loadCurrent()
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(heartbeatCmd(m.paneID, m.current), heartbeatAfter(), tea.EnterAltScreen)
}

func heartbeatCmd(paneID, pageKey string) tea.Cmd {
	return func() tea.Msg {
		if paneID != "" {
			_ = herdripc.Stamp(paneID, pageKey, time.Now())
		}
		return nil
	}
}

func heartbeatAfter() tea.Cmd {
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg { return heartbeatTick{} })
}

// loadCurrent reads the current page and renders it through the memo.
func (m *Model) loadCurrent() {
	page, err := m.app.Read(m.current)
	if err != nil {
		m.body("")
		m.status = "error: " + err.Error()
		return
	}
	m.body(page.Doc.Body)
	m.status = page.Path
}

func (m *Model) body(md string) {
	m.view.SetContent(m.render.Render(md, m.width))
	m.view.GotoTop()
}

func (m *Model) currentMarkdown() string {
	page, err := m.app.Read(m.current)
	if err != nil {
		return ""
	}
	return page.Doc.Body
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.view.Width = msg.Width
		m.view.Height = max(1, msg.Height-2)
		if m.mode == modePage {
			m.body(m.currentMarkdown())
		}
		return m, nil

	case heartbeatTick:
		return m, tea.Batch(heartbeatCmd(m.paneID, m.current), heartbeatAfter())

	case editDone:
		m.loadCurrent()
		if msg.err != nil {
			m.status = "editor failed: " + msg.err.Error()
		} else {
			m.status = "reloaded after edit"
		}
		return m, nil

	case tea.KeyMsg:
		if m.mode == modeList {
			return m.updateList(msg)
		}
		return m.updatePage(msg)
	}

	if m.mode == modePage {
		var cmd tea.Cmd
		m.view, cmd = m.view.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *Model) updatePage(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit

	case "e":
		if len(m.editorArgv) == 0 {
			m.status = "no editor configured"
			return m, nil
		}
		cmd := exec.Command(m.editorArgv[0], append(append([]string(nil), m.editorArgv[1:]...), m.absCurrent())...)
		return m, tea.ExecProcess(cmd, func(err error) tea.Msg { return editDone{err: err} })

	case "l":
		m.source = srcAll
		m.openList()

	case "backspace":
		back, err := m.app.Backlinks(m.current)
		if err != nil {
			m.status = "error: " + err.Error()
			return m, nil
		}
		if len(back) == 0 {
			m.status = "no backlinks"
			return m, nil
		}
		m.source = srcBacklinks
		m.openList()
		m.listPages = pagesFromPaths(back, m.app)

	case "enter":
		links, err := m.app.Links(m.current)
		if err != nil {
			m.status = "error: " + err.Error()
			return m, nil
		}
		if len(links) == 0 {
			m.status = "no links on this page"
			return m, nil
		}
		m.current = m.app.Store.Resolve(links[0])
		m.loadCurrent()
		return m, nil

	case "r":
		m.loadCurrent()
		m.status = "reloaded"

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

func (m *Model) openList() {
	m.mode = modeList
	m.listCursor = 0
	m.listFilter = ""
	m.refreshList()
}

func pagesFromPaths(paths []string, app *notes.App) []pageRow {
	var rows []pageRow
	for _, p := range paths {
		title := strings.TrimSuffix(p, ".md")
		if page, err := app.Read(p); err == nil {
			title = page.Title()
		}
		rows = append(rows, pageRow{Path: p, Title: title})
	}
	return rows
}

func (m *Model) refreshList() {
	if m.source == srcBacklinks {
		// Backlinks were already populated as a fixed set; only re-filter.
		m.applyFilter()
		return
	}
	pages, err := m.app.List()
	if err != nil {
		m.status = "error: " + err.Error()
		return
	}
	m.listPages = m.listPages[:0]
	for _, p := range pages {
		m.listPages = append(m.listPages, pageRow{Path: p.Path, Title: p.Title()})
	}
	m.applyFilter()
	if m.listCursor >= len(m.listPages) {
		m.listCursor = max(0, len(m.listPages)-1)
	}
}

func (m *Model) applyFilter() {
	filter := strings.ToLower(m.listFilter)
	if filter == "" {
		return
	}
	kept := m.listPages[:0]
	for _, p := range m.listPages {
		if strings.Contains(strings.ToLower(p.Path), filter) || strings.Contains(strings.ToLower(p.Title), filter) {
			kept = append(kept, p)
		}
	}
	m.listPages = kept
	if m.listCursor >= len(m.listPages) {
		m.listCursor = max(0, len(m.listPages)-1)
	}
}

func (m *Model) updateList(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "esc":
		m.mode = modePage
		return m, nil
	case "up", "k":
		if m.listCursor > 0 {
			m.listCursor--
		}
	case "down", "j":
		if m.listCursor < len(m.listPages)-1 {
			m.listCursor++
		}
	case "enter", "right", "l":
		if len(m.listPages) > 0 {
			m.current = m.app.Store.Resolve(m.listPages[m.listCursor].Path)
			m.mode = modePage
			m.loadCurrent()
		}
	case "backspace":
		if len(m.listFilter) > 0 {
			m.listFilter = m.listFilter[:len(m.listFilter)-1]
			m.listChanged = true
		}
	default:
		if len(key.Runes) == 1 && key.Runes[0] >= ' ' && key.Runes[0] != 127 {
			m.listFilter += string(key.Runes[0])
			m.listChanged = true
		}
	}
	if m.listChanged {
		m.refreshList()
		m.listChanged = false
	}
	return m, nil
}

func (m *Model) absCurrent() string {
	return m.app.Store.Root() + "/" + m.current
}

func (m *Model) View() string {
	if m.mode == modeList {
		return m.viewList()
	}
	return m.viewPage()
}

func (m *Model) viewPage() string {
	header := lipgloss.NewStyle().Bold(true).Render("Notes") + "  " +
		lipgloss.NewStyle().Faint(true).Render(m.current)
	if m.status != "" {
		header += "  " + lipgloss.NewStyle().Faint(true).Render(m.status)
	}
	hint := "e edit  l list  enter follow  backspace backlinks  r reload  g/G scroll  q quit"
	return header + "\n" + m.view.View() + "\n" + lipgloss.NewStyle().Faint(true).Render(hint)
}

func (m *Model) viewList() string {
	title := "Pages"
	if m.source == srcBacklinks {
		title = "Backlinks"
	}
	header := lipgloss.NewStyle().Bold(true).Render(title) + "  " +
		lipgloss.NewStyle().Faint(true).Render("j/k move · enter open · esc back · type to filter")
	if m.listFilter != "" {
		header += "  " + lipgloss.NewStyle().Faint(true).Render("filter: "+m.listFilter)
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n")
	for i, p := range m.listPages {
		cursor := "  "
		if i == m.listCursor {
			cursor = "> "
		}
		fmt.Fprintf(&b, "%s%-40s %s\n", cursor, p.Path, p.Title)
	}
	if len(m.listPages) == 0 {
		b.WriteString("(no pages)\n")
	}
	return b.String()
}
