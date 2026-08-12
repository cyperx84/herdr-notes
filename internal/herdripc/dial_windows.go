//go:build windows

package herdripc

import (
	"net"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
)

func dial(path string) (net.Conn, error) {
	if !strings.HasPrefix(path, `\\.\pipe\`) {
		path = `\\.\pipe\` + path
	}
	timeout := 2 * time.Second
	return winio.DialPipe(path, &timeout)
}
