package launcher

import (
	"fmt"
	"testing"
	"time"
)

const page = "projects/herdr-loop/scratch.md"

// pane renders a Notes pane with the given id, workspace, page key, and
// heartbeat seconds.
func pane(id, workspace, pageKey string, heartbeat int64) string {
	return fmt.Sprintf(`{"pane_id":%q,"label":"Notes","workspace_id":%q,"tokens":{"herdr-notes":%q,"herdr-notes-page":%q}}`,
		id, workspace, fmt.Sprint(heartbeat), pageKey)
}

func list(panes ...string) []byte {
	return []byte(`{"result":{"panes":[` + join(panes) + `]}}`)
}

func join(panes []string) string {
	out := ""
	for i, p := range panes {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

func TestDecideLifecycle(t *testing.T) {
	now := time.Unix(100, 0)
	focused := `{"pane_id":"w1:p1","focused":true,"tab_id":"w1:t1","workspace_id":"w1"}`
	live := pane("w1:p2", "w1", page, 95)

	if got := Decide(list(focused), page, now); got.Action != "OPEN" {
		t.Fatalf("open: %+v", got)
	}
	if got := Decide(list(focused, live), page, now); got.Action != "FOCUS" || got.PaneID != "w1:p2" {
		t.Fatalf("focus: %+v", got)
	}

	focusedLive := `{"pane_id":"w1:p2","label":"Notes","focused":true,"tab_id":"w1:t1","workspace_id":"w1",` +
		`"tokens":{"herdr-notes":"95","herdr-notes-page":"` + page + `"}}`
	if got := Decide(list(focusedLive), page, now); got.Action != "CLOSE" {
		t.Fatalf("close: %+v", got)
	}
}

func TestDecideReplacesCorpsesAndStaleHeartbeat(t *testing.T) {
	now := time.Unix(100, 0)
	focused := `{"pane_id":"w1:p1","focused":true,"workspace_id":"w1"}`
	for _, notes := range []string{
		// label-only: no page key, so unknown identity.
		`{"pane_id":"w1:p2","label":"Notes","workspace_id":"w1"}`,
		// stale heartbeat: key present but old.
		pane("w1:p2", "w1", page, 40),
	} {
		if got := Decide(list(focused, notes), page, now); got.Action != "REPLACE" {
			t.Fatalf("replace: %+v", got)
		}
	}
}

func TestDecideScopesByPage(t *testing.T) {
	now := time.Unix(100, 0)
	other := pane("w1:p2", "w1", "workspaces/w2.md", 95)
	focused := `{"pane_id":"w1:p1","focused":true,"workspace_id":"w1"}`
	if got := Decide(list(focused, other), page, now); got.Action != "OPEN" {
		t.Fatalf("other page: %+v", got)
	}
}

// Two panes on different workspaces but the same project page must resolve to
// one live pane, never a duplicate.
func TestDecideSamePageAcrossWorkspacesIsOne(t *testing.T) {
	now := time.Unix(100, 0)
	p1 := pane("w1:p2", "w1", page, 95)
	p2 := pane("w2:p2", "w2", page, 95)
	focused := `{"pane_id":"w2:p1","focused":true,"tab_id":"w2:t1","workspace_id":"w2"}`
	got := Decide(list(focused, p1, p2), page, now)
	if got.Action != "FOCUS" || got.PaneID != "w1:p2" {
		t.Fatalf("same page across workspaces = %+v, want FOCUS w1:p2", got)
	}
}

func TestPanesWithoutPageKeyAreUnknown(t *testing.T) {
	now := time.Unix(100, 0)
	old := `{"pane_id":"w1:p2","label":"Notes","workspace_id":"w1","tokens":{"herdr-notes":"95"}}`
	if liveNotePresent(list(old), page, now) {
		t.Fatal("a pane without a page key must not match any page")
	}
}

func TestLiveNotePresentRequiresFreshMatchingHeartbeat(t *testing.T) {
	now := time.Unix(100, 0)
	if !liveNotePresent(list(pane("w1:p2", "w1", page, 95)), page, now) {
		t.Fatal("fresh matching pane not found")
	}
	if liveNotePresent(list(pane("w1:p2", "w1", page, 95)), "other/page.md", now) {
		t.Fatal("other page matched")
	}
	if liveNotePresent(list(pane("w1:p2", "w1", page, 40)), page, now) {
		t.Fatal("stale heartbeat treated as live")
	}
}
