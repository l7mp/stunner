// Package l4 implements the flow engine of the plain (non-TURN) listeners. A plain listener
// relays every client flow verbatim to the single static peer configured on the listener: raw
// flows carry no in-band peer address, so the peer is pinned in the listener config.
//
// Flows are strictly client-initiated: peer traffic is delivered only into existing flows, the
// L4 analog of TURN's allocation-scoped peer traffic. Relaying is outbound-only for now; a
// peer-initiated (reverse tunnel) mode is future work.
package l4

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v4/udp"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/netutil"
	objectturn "github.com/l7mp/stunner/v2/internal/object/turn"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	"github.com/l7mp/stunner/v2/internal/util"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// FlowTimeout is the idle timeout of the plain-listener flows. Matches the default TURN allocation
// lifetime (RFC 5766).
var FlowTimeout = 10 * time.Minute

// Server drives the plain-listener flow engine for one listener context.
type Server struct {
	listener string
	proto    stnrv1.ListenerProtocol
	idle     time.Duration
	runtime  *objruntime.Runtime
	relay    *objectturn.Relay
	quota    *objectturn.Quota
	gate     turn.QuotaHandler
	events   EventHandler
	offload  *offloadHandler
	ln       net.Listener
	log      logging.LeveledLogger

	mu     sync.Mutex
	flows  map[*flow]struct{}
	closed bool
}

// NewServer starts the flow engine for a plain listener context.
func NewServer(listener string, rt *objruntime.Runtime) (*Server, error) {
	conf := rt.GetConfig(objruntime.TypeListener, listener).(*stnrv1.ListenerConfig)
	proto, err := stnrv1.NewListenerProtocol(conf.Protocol)
	if err != nil {
		return nil, fmt.Errorf("invalid listener protocol for %q: %w", listener, err)
	}
	log := rt.Logger.NewLogger(fmt.Sprintf("listener-%s", listener))

	// The quota machinery is shared with the TURN engine: the same gate admits sessions
	// and the same allocation handler administers the counters from the lifecycle events.
	q := objectturn.NewQuotaHandler(rt)
	s := &Server{
		listener: listener,
		proto:    proto,
		idle:     FlowTimeout,
		runtime:  rt,
		relay:    objectturn.NewRelay(listener, rt),
		quota:    q,
		gate:     q.QuotaHandler(),
		events:   NewEventHandler(listener, rt, log, q),
		offload:  newOffloadHandler(listener, rt, log),
		log:      log,
		flows:    make(map[*flow]struct{}),
	}
	log.Debugf("flow engine %s (re)starting", listener)

	// Empty host is the unspecified address: on dual-stack hosts ":port" binds a single
	// socket reachable via both IPv4 and IPv6, on single-family hosts the available family.
	addr := net.JoinHostPort("", strconv.Itoa(conf.Port))

	var ln net.Listener
	switch proto {
	case stnrv1.ProtocolUDP:
		laddr, err := net.ResolveUDPAddr("udp", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse UDP listener address %s: %w", addr, err)
		}
		ln, err = (&udp.ListenConfig{}).Listen("udp", laddr)
		if err != nil {
			return nil, fmt.Errorf("failed to create UDP listener at %s: %w", addr, err)
		}
	case stnrv1.ProtocolTCP:
		var err error
		ln, err = net.Listen("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to create TCP listener at %s: %w", addr, err)
		}
	case stnrv1.ProtocolSTDIN:
		ln = newStdioListener()
	default:
		return nil, fmt.Errorf("unsupported plain listener protocol %q", proto.String())
	}
	s.ln = netutil.NewListener(ln, listener, telemetry.ListenerType, rt.Telemetry, nil, log)

	go s.acceptLoop()
	log.Infof("listener %s: %s flow engine running, peer %s", listener, proto.String(),
		conf.PeerAddr)
	return s, nil
}

func (s *Server) acceptLoop() {
	for {
		conn, err := s.ln.Accept()
		if err != nil {
			return
		}
		go s.serve(conn)
	}
}

func (s *Server) serve(client net.Conn) {
	f, err := s.newFlow(client)
	if err != nil {
		s.log.Infof("rejecting flow from client %s: %s",
			client.RemoteAddr().String(), err.Error())
		_ = client.Close()
		return
	}
	f.run()
}

// newFlow admits and wires a client flow: it resolves the configured peer, routes it through
// the listener's clusters (failing early when nothing admits it), creates the relay leg, and
// registers the flow. The peer is read from the live config, so a reconciled peer address
// applies to new flows while existing flows stay pinned to the peer they were created with.
func (s *Server) newFlow(client net.Conn) (*flow, error) {
	conf, ok := s.runtime.GetConfig(objruntime.TypeListener, s.listener).(*stnrv1.ListenerConfig)
	if !ok || conf == nil {
		return nil, net.ErrClosed
	}
	peerProto, peerAddr := conf.PeerEndpoint()

	host, portStr, err := net.SplitHostPort(peerAddr)
	if err != nil {
		return nil, fmt.Errorf("invalid peer address %q: %w", peerAddr, err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		return nil, fmt.Errorf("invalid port in peer address %q: %w", peerAddr, err)
	}
	ipAddr, err := net.ResolveIPAddr("ip", host)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve peer %q: %w", host, err)
	}
	ip := ipAddr.IP

	// Admission pre-flight, with exactly the TURN permission-handler rule: grant when any
	// routed cluster admits the peer (a TURN-* cluster admits every peer, admission is then
	// the upstream server's job).
	rt := s.runtime
	admitting, ok := rt.Router.Route(s.listener, func(c objruntime.Cluster) bool {
		return c.Admits(ip, port)
	})
	if !ok {
		return nil, netutil.ErrPortProhibited
	}

	// Quota gate: mint the flow's quota identity and fail before anything is dialed. The
	// gate reserves the quota atomically, so every later failure path must release the
	// reservation (success releases through the flow-deleted event).
	user, realm := flowIdentity(rt, client.RemoteAddr())
	if !s.gate(user, realm, client.RemoteAddr()) {
		return nil, errQuotaExceeded
	}
	quotaRelease := func() {
		s.quota.AllocationHandler(client.RemoteAddr(), client.LocalAddr(),
			strings.ToLower(s.proto.String()), user, realm, objectturn.AllocationDeleted)
	}

	// The peer's own transport picks the relay leg; whether the leg relays directly or
	// through an upstream TURN server is the allocators' internal business.
	f := &flow{s: s, client: client}
	f.ev = FlowEvent{
		SrcAddr:      client.RemoteAddr(),
		DstAddr:      client.LocalAddr(),
		Protocol:     strings.ToLower(s.proto.String()),
		PeerProtocol: strings.ToLower(peerProto.String()),
		Cluster:      admitting.Name(),
		Username:     user,
		Realm:        realm,
	}
	switch peerProto {
	case stnrv1.ProtocolTCP:
		peer := &net.TCPAddr{IP: ip, Port: port}
		f.peer = peer
		network := "tcp4"
		if ip.To4() == nil {
			network = "tcp6"
		}
		conn, err := s.relay.AllocateConn(turn.AllocateConnConfig{Network: network, RemoteAddr: peer})
		if err != nil {
			quotaRelease()
			return nil, err
		}
		f.relayStream = conn
	default:
		f.peer = &net.UDPAddr{IP: ip, Port: port}
		network := "udp4"
		if ip.To4() == nil {
			network = "udp6"
		}
		pc, _, err := s.relay.AllocatePacketConn(turn.AllocateListenerConfig{Network: network})
		if err != nil {
			quotaRelease()
			return nil, err
		}
		f.relayPacket = pc
	}
	f.ev.Peer = f.peer

	// The transport reports its own wire addresses: a direct leg leaves from its relay socket
	// with no fixed remote, an upstream TURN leg from the session's transport socket towards
	// the server.
	if f.relayPacket != nil {
		f.ev.RelayAddr, f.ev.ServerAddr = f.relayPacket.TransportAddrs()
	} else {
		f.ev.RelayAddr, f.ev.ServerAddr = f.relayStream.TransportAddrs()
	}

	if !s.addFlow(f) {
		quotaRelease()
		f.closeLeg()
		return nil, net.ErrClosed
	}
	f.touch()
	f.timer = time.AfterFunc(s.idle, f.checkIdle)
	s.events.OnFlowCreated(f.ev)
	s.offload.upsert(f)
	return f, nil
}

func (s *Server) addFlow(f *flow) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.flows[f] = struct{}{}
	return true
}

func (s *Server) removeFlow(f *flow) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.flows, f)
}

// AllocationCount returns the number of live flows: flows are the L4 analog of TURN
// allocations and count as such for draining, telemetry, and status.
func (s *Server) AllocationCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.flows)
}

// Close shuts down the listener and tears down every live flow.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	flows := make([]*flow, 0, len(s.flows))
	for f := range s.flows {
		flows = append(flows, f)
	}
	s.mu.Unlock()

	err := s.ln.Close()
	for _, f := range flows {
		f.close("listener shutting down")
	}
	if err != nil && !util.IsClosedErr(err) {
		return err
	}
	return nil
}

// stdioListener is the one-shot listener of a STDIN listener: it accepts a single conn wrapping
// the process stdin/stdout pair, then blocks until closed. EOF on stdin ends the flow, which is
// the tunnel's teardown signal.
type stdioListener struct {
	conn      chan net.Conn
	done      chan struct{}
	closeOnce sync.Once
}

func newStdioListener() *stdioListener {
	l := &stdioListener{conn: make(chan net.Conn, 1), done: make(chan struct{})}
	l.conn <- stdioConn{}
	return l
}

func (l *stdioListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conn:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *stdioListener) Close() error {
	l.closeOnce.Do(func() { close(l.done) })
	return nil
}

func (l *stdioListener) Addr() net.Addr { return &util.FileConnAddr{File: os.Stdin} }

// stdioConn is the duplex conn of the stdin/stdout pair.
type stdioConn struct{}

func (stdioConn) Read(b []byte) (int, error)  { return os.Stdin.Read(b) }
func (stdioConn) Write(b []byte) (int, error) { return os.Stdout.Write(b) }
func (stdioConn) Close() error                { return os.Stdin.Close() }
func (stdioConn) LocalAddr() net.Addr         { return &util.FileConnAddr{File: os.Stdout} }
func (stdioConn) RemoteAddr() net.Addr        { return &util.FileConnAddr{File: os.Stdin} }

func (stdioConn) SetDeadline(time.Time) error      { return nil }
func (stdioConn) SetReadDeadline(time.Time) error  { return nil }
func (stdioConn) SetWriteDeadline(time.Time) error { return nil }
