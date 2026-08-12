package config

import (
	"reflect"
	"testing"
)

func TestLoadEditorArgv(t *testing.T) {
	t.Setenv("HERDR_WORKSPACE_ID", "w3")
	t.Setenv("HERDR_NOTES_EDITOR_ARGV", `["nvim","-f"]`)
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.Workspace != "w3" || !reflect.DeepEqual(c.Editor, []string{"nvim", "-f"}) {
		t.Fatalf("config: %#v", c)
	}
}

func TestMalformedEditorArgv(t *testing.T) {
	t.Setenv("HERDR_NOTES_EDITOR_ARGV", "nvim -f")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}

func TestEditorDefaultsNeovimFirst(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if got := (Config{}).EditorArgv(); !reflect.DeepEqual(got, []string{"nvim"}) {
		t.Fatalf("got %v", got)
	}
}
