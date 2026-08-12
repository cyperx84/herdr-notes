package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/cyperx84/herdr-notes/internal/store"
)

func testModel(t *testing.T, text string) *Model {
	t.Helper()
	s, err := store.Open(t.TempDir(), "w1")
	if err != nil || s.Save(text) != nil {
		t.Fatal(err)
	}
	m, err := New(s, []string{"nvim"}, "")
	if err != nil {
		t.Fatal(err)
	}
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return m
}

func key(s string) tea.KeyMsg {
	if s == "enter" {
		return tea.KeyMsg{Type: tea.KeyEnter}
	}
	if s == "esc" {
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func TestPreviewEditEscSave(t *testing.T) {
	m := testModel(t, "old")
	m.Update(key("e"))
	if m.mode != edit {
		t.Fatal("did not enter edit")
	}
	m.area.SetValue("new")
	m.Update(key("esc"))
	if m.mode != preview {
		t.Fatal("did not leave edit")
	}
	got, err := m.store.Load()
	if err != nil || got != "new" {
		t.Fatalf("saved %q, %v", got, err)
	}
}

func TestClearRequiresYes(t *testing.T) {
	m := testModel(t, "keep")
	m.Update(key("x"))
	m.Update(key("n"))
	if m.text != "keep" {
		t.Fatal("decline cleared note")
	}
	m.Update(key("x"))
	m.Update(key("y"))
	if m.text != "" {
		t.Fatal("confirmation did not clear")
	}
}

func TestGenerationMakesDebounceStaleSafe(t *testing.T) {
	m := testModel(t, "")
	m.mode = edit
	m.area.Focus()
	m.area.SetValue("one")
	m.touch()
	first := m.generation
	m.area.SetValue("two")
	m.touch()
	m.Update(saveTick{generation: first})
	got, _ := m.store.Load()
	if got != "" {
		t.Fatalf("stale timer saved %q", got)
	}
	m.Update(saveTick{generation: m.generation})
	got, _ = m.store.Load()
	if got != "two" {
		t.Fatalf("latest timer saved %q", got)
	}
}
