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
	"github.com/cyperx84/herdr-notes/internal/notes"
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

// Decide returns OPEN, FOCUS, CLOSE, or REPLACE.
//
// A pane is identified by the notes page it is showing — recorded in the
// pane's metadata tokens as the bundle-relative page path — not by the
// workspace it lives in. That identity is what makes a toggle idempotent
// under project scope, where one page is shared by many workspaces: two panes
// on two workspaces showing the same project page must resolve to one, not
// two independent notes.
func Decide(input []byte, pageKey string, now time.Time) Decision {
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
	// Two distinct outcomes to find among Notes panes:
	//  - a live pane showing this exact page (fresh heartbeat, matching key);
	//  - a corpse to reclaim (no page key at all, or a stale heartbeat).
	// A live pane showing a *different* page is neither: it is a legitimate
	// second Notes pane and must be left alone.
	var live, corpse *Pane
	for i := range list.Result.Panes {
		p := &list.Result.Panes[i]
		if !isNotes(*p) {
			continue
		}
		key := paneKey(*p)
		switch {
		case key == pageKey && !heartbeatStale(p.Tokens, now):
			if live == nil || (p.TabID == focused.TabID && live.TabID != focused.TabID) {
				live = p
			}
		case key == "" || heartbeatStale(p.Tokens, now):
			if corpse == nil {
				corpse = p
			}
		}
	}
	if live != nil {
		if !safeID(live.PaneID) {
			return Decision{Action: "OPEN"}
		}
		if live.PaneID == focused.PaneID {
			return Decision{Action: "CLOSE", PaneID: live.PaneID}
		}
		return Decision{Action: "FOCUS", PaneID: live.PaneID}
	}
	if corpse != nil && safeID(corpse.PaneID) {
		return Decision{Action: "REPLACE", PaneID: corpse.PaneID}
	}
	return Decision{Action: "OPEN"}
}

// paneKey returns the notes page a pane is showing, or "".
//
// The page path is carried in the pane's metadata tokens under the source key
// alongside its heartbeat, so that the heartbeat and the identity cannot
// disagree. Panes that predate this field carry no key and are treated as not
// matching any page, which means a stale pre-scope pane is replaced rather
// than mis-focused.
func paneKey(p Pane) string {
	if !isNotes(p) {
		return ""
	}
	var key string
	if msg := p.Tokens[herdripc.PageKey]; json.Unmarshal(msg, &key) == nil {
		return key
	}
	return ""
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

// Toggle serializes action invocations per page, preventing startup races.
//
// It takes the application core rather than a raw path because the page
// identity — which page is open, and how to match a live pane to it — is
// exactly what notes.App resolves. Passing a file path would push that
// decision into a second place and the two would drift.
func Toggle(app *notes.App) error {
	pageKey := app.Store.Resolve(app.Current())
	lockDir := filepath.Join(app.Store.Root(), filepath.Dir(filepath.FromSlash(pageKey)))
	unlock, err := acquire(lockDir, filepath.Base(pageKey))
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
	decision := Decide(output, pageKey, time.Now())
	switch decision.Action {
	case "FOCUS":
		return focus(bin, decision.PaneID)
	case "CLOSE":
		return closePane(bin, decision.PaneID)
	case "REPLACE":
		_ = closePane(bin, decision.PaneID)
		return open(bin, pageKey)
	default:
		return open(bin, pageKey)
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

func open(bin, pageKey string) error {
	args := []string{"plugin", "pane", "open", "--plugin", "herdr-notes", "--entrypoint", "notes", "--placement", "split", "--direction", "right", "--focus"}
	cmd := exec.Command(bin, args...)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil {
		return err
	}
	// Keep the per-page action lock until the new TUI's startup heartbeat is
	// observable. Without this handshake, a second rapid toggle can see the
	// fresh pane as label-only and replace it as a restart corpse.
	deadline := time.Now().Add(startupDeadline)
	for time.Now().Before(deadline) {
		output, err := exec.Command(bin, "pane", "list").Output()
		if err == nil && liveNotePresent(output, pageKey, time.Now()) {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("notes pane opened but did not publish its heartbeat within %s", startupDeadline)
}

func liveNotePresent(input []byte, pageKey string, now time.Time) bool {
	var list paneList
	if json.Unmarshal(input, &list) != nil {
		return false
	}
	for _, pane := range list.Result.Panes {
		if paneKey(pane) == pageKey && isNotes(pane) && !heartbeatStale(pane.Tokens, now) {
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
