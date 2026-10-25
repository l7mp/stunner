package netconn

import (
	"net"
	"net/netip"
	"sync"
)

// FilterPacketConn drops the datagrams to and from the peers admit refuses. A UDP peer admitted
// once stays admitted for the life of the conn, so a filter change applies to new peers only; a
// refused peer is checked again on its next datagram.
func FilterPacketConn(c net.PacketConn, admit func(net.Addr) bool) net.PacketConn {
	return &filteredPacketConn{PacketConn: c, admit: admit, admitted: map[netip.AddrPort]bool{}}
}

type filteredPacketConn struct {
	net.PacketConn
	admit    func(net.Addr) bool
	mu       sync.RWMutex
	admitted map[netip.AddrPort]bool
}

func (c *filteredPacketConn) admits(a net.Addr) bool {
	u, ok := a.(*net.UDPAddr)
	if !ok {
		return c.admit(a)
	}
	k := u.AddrPort()
	c.mu.RLock()
	ok = c.admitted[k]
	c.mu.RUnlock()
	if ok {
		return true
	}
	if !c.admit(a) {
		return false
	}
	c.mu.Lock()
	c.admitted[k] = true
	c.mu.Unlock()
	return true
}

// WriteTo drops a datagram to a refused peer silently, as the network would.
func (c *filteredPacketConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	if !c.admits(addr) {
		return len(p), nil
	}
	return c.PacketConn.WriteTo(p, addr)
}

func (c *filteredPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		n, from, err := c.PacketConn.ReadFrom(p)
		if err != nil || c.admits(from) {
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
