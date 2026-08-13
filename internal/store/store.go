// Package store owns canonical Markdown persistence and legacy migration.
package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// Store represents exactly one workspace note.
type Store struct {
	Path       string
	workspace  string
	base       string
	configBase string
	stateBase  string
	mu         sync.Mutex
}

// OpenPath returns a Store for an exact file path.
//
// Open derives a path from a workspace id; OpenPath is for callers that have
// already resolved one — the bundle and scope packages now own that decision,
// and two components computing the same path independently is how they drift.
func OpenPath(path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("store: path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	return &Store{
		Path:      abs,
		workspace: strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs)),
		base:      filepath.Dir(abs),
	}, nil
}

// Open resolves a workspace's canonical .md path. A workspace ID is required:
// silently sharing a fallback would violate the one-note-per-workspace boundary.
func Open(notesDir, workspace string) (*Store, error) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return nil, errors.New("HERDR_WORKSPACE_ID is required (or pass --workspace)")
	}
	configBase, _ := os.UserConfigDir()
	stateBase := strings.TrimSpace(os.Getenv("HERDR_PLUGIN_STATE_DIR"))
	base := notesDir
	if base == "" {
		if stateBase != "" {
			base = stateBase
		} else if configBase != "" {
			base = filepath.Join(configBase, "herdr", "herdr-notes")
		} else {
			return nil, errors.New("cannot resolve notes directory")
		}
	}
	return &Store{
		Path:       filepath.Join(base, FileKey(workspace)+".md"),
		workspace:  workspace,
		base:       base,
		configBase: configBase,
		stateBase:  stateBase,
	}, nil
}

// FileKey preserves normal Herdr IDs and hashes anything unsafe for a filename.
// This keeps every non-empty stable ID distinct instead of coarsening IDs into
// a shared fallback note.
func FileKey(id string) string {
	safe := id != "" && id != "." && id != ".."
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			safe = false
			break
		}
	}
	if safe {
		if runtime.GOOS == "windows" {
			return strings.ToLower(id)
		}
		return id
	}
	sum := sha256.Sum256([]byte(id))
	return "workspace-" + hex.EncodeToString(sum[:12])
}

// Load returns canonical Markdown. If it is absent, legacy plain/JSON fallback
// locations are imported with an atomic canonical write, then renamed with a
// .migrated suffix. Canonical content always wins, including an empty file.
func (s *Store) Load() (string, error) {
	data, err := os.ReadFile(s.Path)
	if err == nil {
		return string(data), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("read note: %w", err)
	}
	for _, src := range s.legacyPaths() {
		if filepath.Clean(src) == filepath.Clean(s.Path) {
			continue
		}
		data, err = os.ReadFile(src)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", fmt.Errorf("read legacy note %s: %w", src, err)
		}
		text, ok := decodeLegacy(src, data)
		if !ok {
			continue
		}
		if err := s.Save(text); err != nil {
			return "", fmt.Errorf("migrate %s: %w", src, err)
		}
		_ = os.Rename(src, src+".migrated")
		return text, nil
	}
	return "", nil
}

func decodeLegacy(path string, data []byte) (string, bool) {
	if !strings.EqualFold(filepath.Ext(path), ".json") {
		return string(data), true
	}
	var old struct {
		Text *string `json:"text"`
	}
	if json.Unmarshal(data, &old) != nil || old.Text == nil {
		return "", false
	}
	return *old.Text, true
}

func (s *Store) legacyPaths() []string {
	key := FileKey(s.workspace)
	var paths []string
	// Old plugin-state JSON and plain fallback, including upstream's note.json.
	for _, base := range unique(s.stateBase, s.base) {
		paths = append(paths,
			filepath.Join(base, key+".json"),
			filepath.Join(base, s.workspace+".json"),
			filepath.Join(base, "note.json"),
		)
	}
	if s.configBase != "" {
		herdr := filepath.Join(s.configBase, "herdr")
		paths = append(paths,
			filepath.Join(herdr, "notes", key+".md"),
			filepath.Join(herdr, "notes", key+".json"),
			filepath.Join(herdr, "notes", s.workspace+".json"),
			filepath.Join(herdr, "notes.json"),
		)
	}
	return paths
}

func unique(values ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value != "" && !seen[filepath.Clean(value)] {
			seen[filepath.Clean(value)] = true
			out = append(out, value)
		}
	}
	return out
}

// Save atomically replaces the canonical Markdown file after syncing its data.
func (s *Store) Save(text string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return fmt.Errorf("create notes directory: %w", err)
	}
	f, err := os.CreateTemp(filepath.Dir(s.Path), ".herdr-notes-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary note: %w", err)
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if err = f.Chmod(0o600); err == nil {
		_, err = f.WriteString(text)
	}
	if err == nil {
		err = f.Sync()
	}
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write temporary note: %w", err)
	}
	if err := replaceFile(tmp, s.Path); err != nil {
		return fmt.Errorf("replace note: %w", err)
	}
	return syncParent(filepath.Dir(s.Path))
}
