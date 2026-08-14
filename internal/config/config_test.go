package config

import (
	"path/filepath"
	"reflect"
	"runtime"
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

func TestLoadBundleDir(t *testing.T) {
	home := t.TempDir()
	switch runtime.GOOS {
	case "windows":
		t.Setenv("USERPROFILE", home)
	case "plan9":
		t.Setenv("home", home)
	default:
		t.Setenv("HOME", home)
	}
	t.Setenv("HERDR_NOTES_BUNDLE_DIR", "")

	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "vaults", "CyperX", "openwiki")
	if c.BundleDir != want {
		t.Fatalf("BundleDir = %q, want %q", c.BundleDir, want)
	}

	override := filepath.Join(t.TempDir(), "bundle")
	t.Setenv("HERDR_NOTES_BUNDLE_DIR", override)
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.BundleDir != override {
		t.Fatalf("BundleDir override = %q, want %q", c.BundleDir, override)
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
