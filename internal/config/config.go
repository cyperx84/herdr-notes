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
}

// Load reads HERDR_NOTES_* variables. editor_argv is a JSON string array so
// paths and arguments survive intact and cannot become shell syntax.
func Load() (Config, error) {
	c := Config{
		NotesDir:  strings.TrimSpace(os.Getenv("HERDR_NOTES_NOTES_DIR")),
		Workspace: strings.TrimSpace(os.Getenv("HERDR_WORKSPACE_ID")),
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
	return c, nil
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
