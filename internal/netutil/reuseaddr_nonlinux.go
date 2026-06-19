//go:build !linux

package netutil

import "syscall"

// ReuseAddrControl is a no-op on platforms without the unix reuse socket options:
// outgoing TCP relay dials cannot share the relayed transport address with the
// allocation's listener there and may fail with EADDRINUSE.
func ReuseAddrControl(_, _ string, _ syscall.RawConn) error { return nil }
