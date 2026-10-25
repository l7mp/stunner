package netconn

import "net"

// FilterPacketConn drops the datagrams to and from the peers admit refuses.
func FilterPacketConn(c net.PacketConn, admit func(net.Addr) bool) net.PacketConn {
	return &filteredPacketConn{PacketConn: c, admit: admit}
}

type filteredPacketConn struct {
	net.PacketConn
	admit func(net.Addr) bool
}

// WriteTo drops a datagram to a refused peer silently, as the network would.
func (c *filteredPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if !c.admit(addr) {
		return len(p), nil
	}
	return c.PacketConn.WriteTo(p, addr)
}

func (c *filteredPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, from, err := c.PacketConn.ReadFrom(p)
		if err != nil || c.admit(from) {
			return n, from, err
		}
	}
}

// FilterListener closes the conns of the peers admit refuses.
func FilterListener(l net.Listener, admit func(net.Addr) bool) net.Listener {
	return &filteredListener{Listener: l, admit: admit}
}

type filteredListener struct {
	net.Listener
	admit func(net.Addr) bool
}

func (l *filteredListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil || l.admit(c.RemoteAddr()) {
			return c, err
		}
		_ = c.Close()
	}
}
