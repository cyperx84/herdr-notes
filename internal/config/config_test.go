package config

import (
	"os"
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

	// No vault present: the default resolves empty, letting ResolveBundleDir
	// fall back to plugin state rather than inventing a personal path.
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if c.BundleDir != "" {
		t.Fatalf("BundleDir with no vault = %q, want empty fallback", c.BundleDir)
	}

	// A vault does exist: the default points at its openwiki directory.
	if err := os.MkdirAll(filepath.Join(home, "vaults", "CyperX", "openwiki"), 0o700); err != nil {
		t.Fatal(err)
	}
	c, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "vaults", "CyperX", "openwiki")
	if c.BundleDir != want {
		t.Fatalf("BundleDir with vault = %q, want %q", c.BundleDir, want)
	}

	// An explicit override always wins.
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
