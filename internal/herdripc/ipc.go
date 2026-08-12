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
)

// Stamp marks a pane as a live Notes process. Failures are intentionally
// non-fatal to the TUI, which also works outside Herdr.
func Stamp(paneID string, now time.Time) error {
	if paneID == "" {
		return errors.New("empty pane id")
	}
	return Call("pane.report_metadata", map[string]any{
		"pane_id": paneID,
		"source":  Source,
		"title":   Title,
		"tokens":  map[string]string{Source: fmt.Sprint(now.Unix())},
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
