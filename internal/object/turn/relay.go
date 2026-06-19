package turn

import (
	"fmt"
	"net"

	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/internal/netutil"
	objruntime "github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Relay adapts the dataplane relay transport to the pion RelayAddressGenerator interface for one
// listener context. All routing/admission and socket handling live in internal/netutil; this is
// only the pion-facing shim.
type Relay struct {
	listener string
	runtime  *objruntime.Runtime
	// relayIP is the address advertised to the client for relayed transport addresses.
	relayIP net.IP
}

// NewRelay creates a relay address generator for a listener context.
func NewRelay(listener string, rt *objruntime.Runtime) *Relay {
	conf := rt.GetConfig(objruntime.TypeListener, listener).(*stnrv1.ListenerConfig)
	ip := net.ParseIP(conf.Addr)
	if ip == nil && conf.Addr == "localhost" {
		ip = net.ParseIP("127.0.0.1")
	}
	if ip == nil {
		panic(fmt.Sprintf("turn: invalid listener address for %q: %s", listener, conf.Addr))
	}
	return &Relay{listener: listener, runtime: rt, relayIP: ip}
}

// Validate is called on server startup and confirms the RelayAddressGenerator is configured.
func (r *Relay) Validate() error { return nil }

// AllocatePacketConn allocates the UDP relayed transport address of an allocation.
func (r *Relay) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	return netutil.NewRelayPacketConn(r.runtime, r.listener, r.relayIP, conf.Network, conf.RequestedPort)
}

// AllocateConn opens an outgoing connection for an RFC 6062 Connect request, sourced from the
// allocation's relayed transport address.
func (r *Relay) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	return netutil.Dial(r.runtime, r.listener, conf.LocalAddr, conf.RemoteAddr)
}

// AllocateListener binds the relayed transport address of an RFC 6062 TCP allocation, admitting
// incoming connections at accept time. It fails early if the listener routes to no cluster of the
// requested protocol.
func (r *Relay) AllocateListener(conf turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	if !netutil.HasRoutedCluster(r.runtime, r.listener, netutil.ProtocolFromNetwork(conf.Network)) {
		return nil, nil, netutil.ErrPortProhibited
	}
	return netutil.NewRelayListener(r.runtime, r.listener, r.relayIP, conf.Network, conf.RequestedPort)
}
