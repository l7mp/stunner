//go:build linux

package netutil

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// ReuseAddrControl sets SO_REUSEADDR and SO_REUSEPORT on the socket. TCP relay sockets
// need this to share the relayed transport address: per RFC 6062 the allocation's
// listener and every outgoing dial bind the same address:port (dialed connections get
// unique 4-tuples, so the kernel allows the share).
func ReuseAddrControl(_, _ string, conn syscall.RawConn) error {
	var serr error
	err := conn.Control(func(fd uintptr) {
		if err := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); err != nil {
			serr = err
			return
		}
		serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
	})
	if err != nil {
		return err
	}
	return serr
}
