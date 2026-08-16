package tui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cyperx84/herdr-notes/internal/bundle"
	"github.com/cyperx84/herdr-notes/internal/notes"
	"github.com/cyperx84/herdr-notes/internal/scope"
)

func testModel(t *testing.T) *Model {
	t.Helper()
	store, err := bundle.Open(t.TempDir(), "Note")
	if err != nil {
		t.Fatal(err)
	}
	app := notes.New(store, scope.Resolve(scope.Workspace, scope.Context{WorkspaceID: "w1"}), []string{"nvim"})
	app.Now = func() time.Time { return time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC) }
	if err := app.Append("", "hello from the current page"); err != nil {
		t.Fatal(err)
	}
	m := New(app, []string{"nvim"}, "")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestNewShowsCurrentPage(t *testing.T) {
	m := testModel(t)
	if m.current != "workspaces/w1.md" {
		t.Errorf("current = %q, want the workspace page", m.current)
	}
	if !strings.Contains(m.View(), "hello from the current page") {
		t.Errorf("view did not render the page body: %q", m.View())
	}
}

func TestListNavigatesToPage(t *testing.T) {
	m := testModel(t)
	if err := m.app.Append("other-page", "other content"); err != nil {
		t.Fatal(err)
	}

	m.Update(key("l"))
	if m.mode != modeList {
		t.Fatal("l did not open the list")
	}
	if len(m.listPages) < 2 {
		t.Fatalf("list has %d pages, want at least 2", len(m.listPages))
	}
	m.Update(key("enter"))
	if m.mode != modePage {
		t.Fatal("enter did not open a page")
	}
}

func TestListFiltersByTyping(t *testing.T) {
	m := testModel(t)
	m.app.Append("zzz", "noise") // ignore error; page creation is not under test
	m.app.Append("target", "goal")

	m.Update(key("l"))
	m.app.List() // ensure pages loaded
	m.refreshList()
	// Type a filter narrowing to one page.
	for _, r := range "target" {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if len(m.listPages) != 1 {
		t.Fatalf("filter left %d pages, want 1: %#v", len(m.listPages), m.listPages)
	}
	if m.listPages[0].Path != "target.md" {
		t.Errorf("filtered page = %q", m.listPages[0].Path)
	}
}

func TestBacklinksSwitchesToList(t *testing.T) {
	m := testModel(t)
	// The current page is workspaces/w1.md; make another page link TO it.
	m.app.Append("hub", "see [the current page](workspaces/w1.md)")

	m.Update(key("backspace"))
	if m.mode != modeList || m.source != srcBacklinks {
		t.Fatalf("backspace did not open backlink list: mode=%d source=%d", m.mode, m.source)
	}
	if len(m.listPages) == 0 {
		t.Fatal("expected backlinks")
	}
	found := false
	for _, p := range m.listPages {
		if p.Path == "hub.md" {
			found = true
		}
	}
	if !found {
		t.Fatalf("hub.md should be a backlink, got %#v", m.listPages)
	}
}

func TestEnterFollowsFirstLink(t *testing.T) {
	m := testModel(t)
	m.app.Append("hub", "go to [dest](dest.md)")
	m.current = "hub.md"
	m.loadCurrent()

	m.Update(key("enter"))
	if m.current != "dest.md" {
		t.Errorf("enter followed to %q, want dest.md", m.current)
	}
}

func TestQuitFromPage(t *testing.T) {
	m := testModel(t)
	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("q should return a quit command")
	}
}

// The memoizing renderer must actually avoid re-rendering identical input
// when the model re-renders the same page body (e.g. on resize).
func TestRepeatRenderIsMemoized(t *testing.T) {
	m := testModel(t)
	md := m.currentMarkdown()
	m.body(md) // initial real render
	hits1, misses1 := m.render.Stats()
	for i := 0; i < 5; i++ {
		m.body(md)
	}
	hits2, _ := m.render.Stats()
	if hits2 <= hits1 {
		t.Errorf("identical bodies should hit the memo: hits %d -> %d", hits1, hits2)
	}
	if misses1 == 0 {
		t.Error("the first render should have missed")
	}
}

// Live-refresh: an external append bumps the file's mod time and the next
// heartbeat reload picks it up, without re-rendering when nothing changed.
func TestRefreshIfChangedReloadsOnExternalAppend(t *testing.T) {
	m := testModel(t)
	before := m.lastMod
	if before.IsZero() {
		t.Fatal("lastMod should be set after initial load")
	}
	if !strings.Contains(m.currentMarkdown(), "hello from the current page") {
		t.Fatal("initial page content missing")
	}

	// External append via the app (as an agent on another pane would).
	time.Sleep(time.Millisecond * 10) // ensure a distinct mod time
	if err := m.app.Append("", "added by an agent"); err != nil {
		t.Fatal(err)
	}

	m.refreshIfChanged()
	if m.lastMod.Equal(before) {
		t.Fatal("refresh did not detect the external change")
	}
	if !strings.Contains(m.currentMarkdown(), "added by an agent") {
		t.Error("reloaded page does not include the appended line")
	}
}

// No-op when nothing changed: a heartbeat should not re-render identical
// content.
func TestRefreshIfChangedIsNoopWithoutChange(t *testing.T) {
	m := testModel(t)
	before := m.lastMod
	m.refreshIfChanged()
	if !m.lastMod.Equal(before) {
		t.Error("mod time changed when the file did not")
	}
}
