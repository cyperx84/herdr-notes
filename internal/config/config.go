// Package config resolves Herdr-provided settings without invoking a shell.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Config is the runtime configuration. Editor is an argv, never a shell command.
type Config struct {
	NotesDir  string
	Editor    []string
	Workspace string

	// Scope selects what a note is about: workspace, project, or global.
	// Empty means the caller's default.
	Scope string
	// BundleDir is the OKF bundle root. Load defaults it to the agent-sanctioned
	// openwiki directory in the user's vault; explicitly constructed Config
	// values can still leave it empty to fall back to NotesDir or plugin state.
	BundleDir string
	// DocType is the `type` written into documents this tool creates. The
	// useful vocabulary belongs to the bundle, not to this tool, so a bundle
	// with its own conventions can keep them.
	DocType string
}

// Load reads HERDR_NOTES_* variables. editor_argv is a JSON string array so
// paths and arguments survive intact and cannot become shell syntax.
func Load() (Config, error) {
	bundleDir := strings.TrimSpace(os.Getenv("HERDR_NOTES_BUNDLE_DIR"))
	if bundleDir == "" {
		bundleDir = defaultBundleDir()
	}

	c := Config{
		NotesDir:  strings.TrimSpace(os.Getenv("HERDR_NOTES_NOTES_DIR")),
		Workspace: strings.TrimSpace(os.Getenv("HERDR_WORKSPACE_ID")),
		Scope:     strings.TrimSpace(os.Getenv("HERDR_NOTES_SCOPE")),
		BundleDir: bundleDir,
		DocType:   strings.TrimSpace(os.Getenv("HERDR_NOTES_DOC_TYPE")),
	}
	if raw := strings.TrimSpace(os.Getenv("HERDR_NOTES_EDITOR_ARGV")); raw != "" {
		if err := json.Unmarshal([]byte(raw), &c.Editor); err != nil {
			return Config{}, fmt.Errorf("HERDR_NOTES_EDITOR_ARGV must be a JSON string array: %w", err)
		}
		for _, arg := range c.Editor {
			if strings.IndexByte(arg, 0) >= 0 {
				return Config{}, fmt.Errorf("HERDR_NOTES_EDITOR_ARGV contains a NUL byte")
			}
		}
	}
	if c.NotesDir != "" {
		var err error
		c.NotesDir, err = expandHome(c.NotesDir)
		if err != nil {
			return Config{}, err
		}
	}
	if c.BundleDir != "" {
		var err error
		c.BundleDir, err = expandHome(c.BundleDir)
		if err != nil {
			return Config{}, err
		}
	}
	return c, nil
}

// defaultBundleDir returns the bundle root to use when none is configured.
//
// It points at the agent-sanctioned openwiki directory inside a vault only
// when that vault actually exists on this machine; otherwise it returns ""
// so that ResolveBundleDir falls back to plugin state. This keeps the
// zero-config default working for anyone — no personal path leaks into a
// machine where it does not exist — while still landing in the vault on a
// machine that has one.
func defaultBundleDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	vault := filepath.Join(home, "vaults", "CyperX", "openwiki")
	if info, err := os.Stat(vault); err == nil && info.IsDir() {
		return vault
	}
	return ""
}

// ResolveBundleDir returns the bundle root, preferring explicit configuration
// and falling back to herdr's plugin state directory.
//
// The fallback is what keeps this zero-config for someone who just installs
// the plugin, while a single setting points it at an existing vault.
func (c Config) ResolveBundleDir(stateDir string) (string, error) {
	for _, candidate := range []string{c.BundleDir, c.NotesDir, stateDir} {
		if strings.TrimSpace(candidate) != "" {
			return candidate, nil
		}
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot resolve a bundle directory: %w", err)
	}
	return filepath.Join(base, "herdr", "herdr-notes"), nil
}

// ResolveDocType returns the frontmatter `type` for created documents.
func (c Config) ResolveDocType() string {
	if c.DocType != "" {
		return c.DocType
	}
	return "Note"
}

func expandHome(path string) (string, error) {
	if path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`) {
		return filepath.Clean(path), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve notes_dir: %w", err)
	}
	if path == "~" {
		return home, nil
	}
	return filepath.Join(home, path[2:]), nil
}

// EditorArgv returns the configured editor, then VISUAL/EDITOR when each is a
// single executable path, then nvim. Environment values containing whitespace
// are deliberately rejected: use editor_argv for arguments.
func (c Config) EditorArgv() []string {
	if len(c.Editor) > 0 {
		return append([]string(nil), c.Editor...)
	}
	for _, name := range []string{"VISUAL", "EDITOR"} {
		if value := strings.TrimSpace(os.Getenv(name)); value != "" && !strings.ContainsAny(value, " \t\r\n") {
			return []string{value}
		}
	}
	return []string{"nvim"}
}
