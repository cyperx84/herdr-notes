// Package herdripc implements the one-line JSON socket call needed for pane identity.
package herdripc

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	Source = "herdr-notes"
	Title  = "Notes"
	// PageKey is the metadata token carrying the bundle-relative page path a
	// pane is showing. It lets the launcher identify a pane by page rather
	// than by workspace, which is what makes toggles idempotent under project
	// scope where one page is shared across workspaces.
	//
	// Token keys must match herdr's schema (`^[A-Za-z0-9_-]{1,32}$`), so the
	// separator is a dash, not a dot.
	PageKey = "herdr-notes-page"
)

// Stamp marks a pane as a live Notes process showing a given page. Failures
// are intentionally non-fatal to the TUI, which also works outside Herdr.
func Stamp(paneID, pageKey string, now time.Time) error {
	if paneID == "" {
		return errors.New("empty pane id")
	}
	return Call("pane.report_metadata", map[string]any{
		"pane_id": paneID,
		"source":  Source,
		"title":   Title,
		"tokens": map[string]string{
			Source:  fmt.Sprint(now.Unix()),
			PageKey: pageKey,
		},
	})
}

// Call makes one request/response round trip.
func Call(method string, params any) error {
	path := os.Getenv("HERDR_SOCKET_PATH")
	if path == "" {
		return errors.New("HERDR_SOCKET_PATH is unset")
	}
	conn, err := dial(path)
	if err != nil {
		return err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
	req := map[string]any{"id": "herdr-notes:" + method, "method": method, "params": params}
	if err := json.NewEncoder(conn).Encode(req); err != nil {
		return err
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	var response struct {
		Error any `json:"error"`
	}
	if json.Unmarshal(line, &response) == nil && response.Error != nil {
		return fmt.Errorf("herdr API: %v", response.Error)
	}
	return nil
}
