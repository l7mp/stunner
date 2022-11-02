package util

import (
	"errors"
	"io"
	"net"
	"syscall"
)

// IsClosedErr is a helper function to check for "already closed" errors, which are usually
// harmless but still show up in logs, code adapted from
// https://stackoverflow.com/questions/44974984/how-to-check-a-net-conn-is-closed
func IsClosedErr(err error) bool {
	switch {
	case
		errors.Is(err, net.ErrClosed),
		errors.Is(err, io.EOF),
		errors.Is(err, syscall.EPIPE),
		// the PacketConn forged by pion/turn fails to check for net.ErrClosed
		// !strings.Contains(err.Error(), "use of closed network connection"),
		// vnet returns its own error (unexported) types
		err.Error() == "already closed":
		return true
	default:
		return false
	}
}
