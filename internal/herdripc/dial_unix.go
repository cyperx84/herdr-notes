//go:build !windows

package herdripc

import (
	"net"
)

func dial(path string) (net.Conn, error) { return net.DialTimeout("unix", path, 2e9) }
