package turn

import (
	"net"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/server"
)

// PermissionHandler exposes the permission handler to the external tests.
func (s *Server) PermissionHandler(src net.Addr, peer net.IP) bool {
	return s.permissionHandler(src, peer)
}

// Conn exposes the index of served conns to the external tests.
func (s *Server) Conn(remote, local net.Addr) (api.Conn, bool) {
	s.mu.Lock()
	ct := s.conntrack
	s.mu.Unlock()
	e, ok := ct.Get(server.Key(remote, local))
	if !ok {
		return nil, false
	}
	return e.Client, true
}
