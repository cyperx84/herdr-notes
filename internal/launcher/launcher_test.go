package launcher

import (
	"fmt"
	"testing"
	"time"
)

func list(panes string) []byte {
	return []byte(fmt.Sprintf(`{"result":{"panes":[%s]}}`, panes))
}

func TestDecideLifecycle(t *testing.T) {
	now := time.Unix(100, 0)
	focused := `{"pane_id":"w1:p1","focused":true,"tab_id":"w1:t1","workspace_id":"w1"}`
	live := `{"pane_id":"w1:p2","label":"Notes","tab_id":"w1:t1","workspace_id":"w1","tokens":{"herdr-notes":"95"}}`
	if got := Decide(list(focused), now); got.Action != "OPEN" {
		t.Fatalf("open: %+v", got)
	}
	if got := Decide(list(focused+","+live), now); got.Action != "FOCUS" || got.PaneID != "w1:p2" {
		t.Fatalf("focus: %+v", got)
	}
	focusedLive := `{"pane_id":"w1:p2","label":"Notes","focused":true,"tab_id":"w1:t1","workspace_id":"w1","tokens":{"herdr-notes":"95"}}`
	if got := Decide(list(focusedLive), now); got.Action != "CLOSE" {
		t.Fatalf("close: %+v", got)
	}
}

func TestDecideReplacesCorpsesAndStaleHeartbeat(t *testing.T) {
	now := time.Unix(100, 0)
	focused := `{"pane_id":"w1:p1","focused":true,"workspace_id":"w1"}`
	for _, notes := range []string{
		`{"pane_id":"w1:p2","label":"Notes","workspace_id":"w1"}`,
		`{"pane_id":"w1:p2","workspace_id":"w1","tokens":{"herdr-notes":"40"}}`,
	} {
		if got := Decide(list(focused+","+notes), now); got.Action != "REPLACE" {
			t.Fatalf("replace: %+v", got)
		}
	}
}

func TestDecideScopesByWorkspaceFile(t *testing.T) {
	now := time.Unix(100, 0)
	json := list(`{"pane_id":"w1:p1","focused":true,"workspace_id":"w1"},{"pane_id":"w2:p2","label":"Notes","workspace_id":"w2","tokens":{"herdr-notes":"95"}}`)
	if got := Decide(json, now); got.Action != "OPEN" {
		t.Fatalf("other workspace: %+v", got)
	}
}

func TestLiveNotePresentRequiresFreshMatchingHeartbeat(t *testing.T) {
	now := time.Unix(100, 0)
	live := list(`{"pane_id":"w1:p2","label":"Notes","workspace_id":"w1","tokens":{"herdr-notes":"95"}}`)
	if !liveNotePresent(live, "w1", now) {
		t.Fatal("fresh matching pane not found")
	}
	if liveNotePresent(live, "w2", now) {
		t.Fatal("other workspace matched")
	}
	corpse := list(`{"pane_id":"w1:p2","label":"Notes","workspace_id":"w1"}`)
	if liveNotePresent(corpse, "w1", now) {
		t.Fatal("label-only corpse treated as live")
	}
}
