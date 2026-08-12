// Package launcher implements a serialized, heartbeat-aware Notes pane toggle.
package launcher

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/cyperx84/herdr-notes/internal/herdripc"
	"github.com/cyperx84/herdr-notes/internal/store"
)

const (
	staleAfter      = 20 * time.Second
	startupDeadline = 15 * time.Second
)

type paneList struct {
	Result struct {
		Panes []Pane `json:"panes"`
	} `json:"result"`
}

// Pane is the portion of pane-list JSON used by the decision function.
type Pane struct {
	PaneID      string                     `json:"pane_id"`
	Label       string                     `json:"label"`
	Focused     bool                       `json:"focused"`
	TabID       string                     `json:"tab_id"`
	WorkspaceID string                     `json:"workspace_id"`
	Tokens      map[string]json.RawMessage `json:"tokens"`
}

type Decision struct {
	Action string
	PaneID string
}

// Decide returns OPEN, FOCUS, CLOSE, or REPLACE. Matching uses canonical file
// identity, so two processes can never intentionally edit one note.
func Decide(input []byte, now time.Time) Decision {
	var list paneList
	input = []byte(strings.TrimPrefix(string(input), "\ufeff"))
	if json.Unmarshal(input, &list) != nil {
		return Decision{Action: "OPEN"}
	}
	var focused *Pane
	for i := range list.Result.Panes {
		if list.Result.Panes[i].Focused {
			focused = &list.Result.Panes[i]
			break
		}
	}
	if focused == nil {
		return Decision{Action: "OPEN"}
	}
	key := store.FileKey(focused.WorkspaceID)
	var candidate *Pane
	for i := range list.Result.Panes {
		p := &list.Result.Panes[i]
		if store.FileKey(p.WorkspaceID) != key || !isNotes(*p) {
			continue
		}
		if candidate == nil || (p.TabID == focused.TabID && candidate.TabID != focused.TabID) {
			candidate = p
		}
	}
	if candidate == nil || !safeID(candidate.PaneID) {
		return Decision{Action: "OPEN"}
	}
	if heartbeatStale(candidate.Tokens, now) {
		return Decision{Action: "REPLACE", PaneID: candidate.PaneID}
	}
	if candidate.PaneID == focused.PaneID {
		return Decision{Action: "CLOSE", PaneID: candidate.PaneID}
	}
	return Decision{Action: "FOCUS", PaneID: candidate.PaneID}
}

func isNotes(p Pane) bool {
	_, token := p.Tokens[herdripc.Source]
	return token || p.Label == herdripc.Title
}

func heartbeatStale(tokens map[string]json.RawMessage, now time.Time) bool {
	raw, ok := tokens[herdripc.Source]
	if !ok {
		return true
	}
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return true
	}
	var seconds int64
	switch v := value.(type) {
	case string:
		seconds, _ = strconv.ParseInt(v, 10, 64)
	case float64:
		seconds = int64(v)
	}
	return seconds <= 0 || now.Sub(time.Unix(seconds, 0)) > staleAfter
}

func safeID(id string) bool {
	if id == "" || strings.HasPrefix(id, "-") {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune(":._-", r)) {
			return false
		}
	}
	return true
}

// Toggle serializes action invocations per note, preventing startup races.
func Toggle(note *store.Store) error {
	unlock, err := acquire(filepath.Dir(note.Path), filepath.Base(note.Path))
	if err != nil {
		return err
	}
	defer unlock()

	bin := os.Getenv("HERDR_BIN_PATH")
	if bin == "" {
		bin = "herdr"
	}
	output, err := exec.Command(bin, "pane", "list").Output()
	if err != nil {
		return fmt.Errorf("list panes: %w", err)
	}
	decision := Decide(output, time.Now())
	switch decision.Action {
	case "FOCUS":
		return focus(bin, decision.PaneID)
	case "CLOSE":
		return closePane(bin, decision.PaneID)
	case "REPLACE":
		_ = closePane(bin, decision.PaneID)
		return open(bin, strings.TrimSuffix(filepath.Base(note.Path), filepath.Ext(note.Path)))
	default:
		return open(bin, strings.TrimSuffix(filepath.Base(note.Path), filepath.Ext(note.Path)))
	}
}

func focus(bin, paneID string) error {
	// pane.zoom only changes geometry; it does not transfer input focus.
	// Plugin panes have a dedicated focus API and paneID has already passed
	// the strict safeID check in Decide.
	return exec.Command(bin, "plugin", "pane", "focus", paneID).Run()
}

func closePane(bin, paneID string) error {
	_ = exec.Command(bin, "pane", "send-keys", paneID, "Escape", "q").Run()
	time.Sleep(250 * time.Millisecond)
	// The TUI normally exits its pane; close is fallback cleanup.
	_ = exec.Command(bin, "pane", "close", paneID).Run()
	return nil
}

func open(bin, noteKey string) error {
	args := []string{"plugin", "pane", "open", "--plugin", "herdr-notes", "--entrypoint", "notes", "--placement", "split", "--direction", "right", "--focus"}
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	// Keep the per-note action lock until the new TUI's startup heartbeat is
	// observable. Without this handshake, a second rapid toggle can see the
	// fresh pane as label-only and replace it as a restart corpse.
	deadline := time.Now().Add(startupDeadline)
	for time.Now().Before(deadline) {
		output, err := exec.Command(bin, "pane", "list").Output()
		if err == nil && liveNotePresent(output, noteKey, time.Now()) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("notes pane opened but did not publish its heartbeat within %s", startupDeadline)
}

func liveNotePresent(input []byte, noteKey string, now time.Time) bool {
	var list paneList
	if json.Unmarshal(input, &list) != nil {
		return false
	}
	for _, pane := range list.Result.Panes {
		if store.FileKey(pane.WorkspaceID) == noteKey && isNotes(pane) && !heartbeatStale(pane.Tokens, now) {
			return true
		}
	}
	return false
}

func acquire(dir, name string) (func(), error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	lock := filepath.Join(dir, "."+name+".toggle.lock")
	deadline := time.Now().Add(5 * time.Second)
	for {
		err := os.Mkdir(lock, 0o700)
		if err == nil {
			return func() { _ = os.Remove(lock) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(lock); statErr == nil && time.Since(info.ModTime()) > 15*time.Second {
			_ = os.Remove(lock)
			continue
		}
		if time.Now().After(deadline) {
			return nil, errors.New("another notes toggle is still in progress")
		}
		time.Sleep(40 * time.Millisecond)
	}
}
