package object

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"maps"
	"net"
	"reflect"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/dialer"
	"github.com/l7mp/stunner/v2/internal/fifo"
	"github.com/l7mp/stunner/v2/internal/netconn"
	"github.com/l7mp/stunner/v2/internal/netconn/account"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	"github.com/l7mp/stunner/v2/internal/util"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/udp"
)

// handshakeTimeout bounds the TLS or DTLS handshake of a new client.
const handshakeTimeout = 10 * time.Second

// newTLSConfig builds the TLS configuration of a TLS listener. The open-source build ignores the
// PQC mode; a build with the PQC feature replaces it.
var newTLSConfig = func(_ *runtime.Runtime, _ stnrv2.PQCMode, cert tls.Certificate, _ logging.LeveledLogger) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
}

// Listener is a listener object: it accepts client conns, or dials its peer, applies the listener
// filters, and processes the conns via a chain of servers.
type Listener struct {
	tag     api.Tag // the name and the protocol: a change of either restarts
	addr    string
	port    int
	connect *stnrv2.ListenerConnectConfig // nil for an accepting listener
	rt      *runtime.Runtime
	log     logging.LeveledLogger
	scope   string // of the logger
	// sink takes the conns the listener emits: serve, or a test's
	sink func(api.Conn)

	mu          sync.RWMutex
	servers     []string
	filters     []string
	labels      map[string]string
	publicAddr  string
	publicPort  int
	publicAddrs []string
	tls         *stnrv2.TLSConfig // the config's, nil for none
	keyPair     tls.Certificate
	pqcMode     stnrv2.PQCMode

	ln        net.Listener
	closed    chan struct{}
	closeOnce sync.Once
}

var _ runtime.Object = &Listener{}

// NewListener creates the listener object of a config.
func NewListener(conf stnrv2.Config, rt *runtime.Runtime) (*Listener, error) {
	req, err := desiredListener(conf)
	if err != nil {
		return nil, err
	}
	proto, err := stnrv2.NewListenerProtocol(req.Protocol)
	if err != nil {
		return nil, err
	}
	l := &Listener{
		tag:    api.Tag{Name: req.Name, Proto: proto},
		addr:   req.Addr,
		port:   req.Port,
		rt:     rt,
		scope:  fmt.Sprintf("listener-%s", req.Name),
		closed: make(chan struct{}),
	}
	l.log = rt.Logger.NewLogger(l.scope)
	l.sink = l.serve
	if req.Connect != nil {
		l.connect = req.Connect.DeepCopy()
	}
	if err := l.Reconcile(req); err != nil {
		return nil, err
	}
	return l, nil
}

// socket makes the socket of the listener: a dialing one, or the bound one of its protocol.
func (l *Listener) socket() (net.Listener, error) {
	addr := net.JoinHostPort(l.addr, strconv.Itoa(l.port))
	switch {
	case l.connect != nil:
		return dialer.NewListener(&stnrv2.ListenerConfig{Name: l.Name(), Protocol: l.tag.Proto.String(),
			Addr: l.addr, Port: l.port, Connect: l.connect}, l.rt, l.admits)
	case l.tag.Proto == stnrv2.ProtocolUDP || l.tag.Proto == stnrv2.ProtocolDTLS:
		lc := udp.DefaultListenConfig
		lc.Net, lc.Admit = l.rt.Net, l.admits
		return lc.Listen("udp", addr)
	case l.tag.Proto == stnrv2.ProtocolTCP || l.tag.Proto == stnrv2.ProtocolTLS:
		return l.rt.Net.CreateListenConfig(&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	case l.tag.Proto == stnrv2.ProtocolSTDIN:
		return util.NewStdioListener(), nil
	case l.tag.Proto == stnrv2.ProtocolFIFO:
		return l.rt.Fifos.Listen(context.Background(), fifo.Network, l.addr)
	}
	return nil, fmt.Errorf("unsupported listener protocol %q", l.tag.Proto.String())
}

// secure starts the server side of TLS or DTLS on an accepted conn, with the certificate and the
// PQC mode of the moment, and returns the handshake to complete.
func (l *Listener) secure(c net.Conn) (net.Conn, func(context.Context) error, error) {
	l.mu.RLock()
	mode, kp := l.pqcMode, l.keyPair
	l.mu.RUnlock()
	if l.tag.Proto == stnrv2.ProtocolTLS {
		tc := tls.Server(c, newTLSConfig(l.rt, mode, kp, l.log))
		return tc, tc.HandshakeContext, nil
	}
	dc, err := dtls.ServerWithOptions(netconn.PacketConn{Conn: c}, c.RemoteAddr(),
		dtls.WithCertificates(kp))
	if err != nil {
		return nil, nil, err
	}
	return dc, dc.HandshakeContext, nil
}

// desiredListener returns the validated listener config of a New, an Inspect or a Reconcile.
func desiredListener(conf stnrv2.Config) (*stnrv2.ListenerConfig, error) {
	req, ok := conf.(*stnrv2.ListenerConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	return req, nil
}

func (l *Listener) Name() string             { return l.tag.Name }
func (l *Listener) Type() runtime.ObjectType { return runtime.TypeListener }

// Inspect restarts on a socket change (protocol, address, port, connect); the rest reconciles in
// place.
func (l *Listener) Inspect(old, new stnrv2.Config, full *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, err := desiredListener(new)
	if err != nil {
		return runtime.ActionNone, err
	}
	cur := old.(*stnrv2.ListenerConfig)
	if full != nil {
		for _, name := range req.Filters {
			if _, err := full.GetFilterConfig(name); err != nil {
				l.log.Warnf("listener %s: no filter %q: it admits every client", l.Name(), name)
			}
		}
	}
	switch {
	case cur.DeepEqual(req):
		return runtime.ActionNone, nil
	case cur.Protocol != req.Protocol || cur.Addr != req.Addr || cur.Port != req.Port ||
		!reflect.DeepEqual(cur.Connect, req.Connect):
		return runtime.ActionRestart, nil
	}
	return runtime.ActionReconcile, nil
}

// Reconcile applies everything but the socket to the next conns. The certificate of a running
// listener loads here: one that does not load fails the reconcile and keeps the previous one; a
// stopped listener loads it on Start.
func (l *Listener) Reconcile(conf stnrv2.Config) error {
	req, err := desiredListener(conf)
	if err != nil {
		return err
	}
	if req.Name != l.Name() {
		return fmt.Errorf("cannot rename listener %q to %q", l.Name(), req.Name)
	}
	l.mu.RLock()
	keyPair := l.keyPair
	l.mu.RUnlock()
	if l.ln != nil {
		if keyPair, err = l.loadKeyPair(req.TLS); err != nil {
			return err
		}
	}
	var tlsConf *stnrv2.TLSConfig
	pqcMode := stnrv2.PQCModeDefault
	if req.TLS != nil {
		tlsConf = req.TLS.DeepCopy()
		// a validated config holds a known mode
		pqcMode, _ = stnrv2.NewPQCMode(req.TLS.PQCMode)
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.servers, l.filters = slices.Clone(req.Servers), slices.Clone(req.Filters)
	l.labels = maps.Clone(req.Labels)
	l.publicAddr, l.publicPort = req.PublicAddr, req.PublicPort
	l.publicAddrs = slices.Clone(req.PublicAddrs)
	l.tls, l.pqcMode, l.keyPair = tlsConf, pqcMode, keyPair
	return nil
}

// loadKeyPair loads the base64 PEM certificate and key of an accepting TLS or DTLS listener;
// other listeners have none.
func (l *Listener) loadKeyPair(conf *stnrv2.TLSConfig) (tls.Certificate, error) {
	if l.connect != nil || l.tag.Proto != stnrv2.ProtocolTLS && l.tag.Proto != stnrv2.ProtocolDTLS {
		return tls.Certificate{}, nil
	}
	var certB64, keyB64 string
	if conf != nil {
		certB64, keyB64 = conf.Cert, conf.Key
	}
	cert, err := base64.StdEncoding.DecodeString(certB64)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("invalid TLS certificate: base64-decode error: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("invalid TLS key: base64-decode error: %w", err)
	}
	keyPair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("cannot load cert/key pair for %s listener %s: %w",
			l.tag.Proto.String(), l.Name(), err)
	}
	return keyPair, nil
}

func (l *Listener) GetConfig() stnrv2.Config {
	l.mu.RLock()
	c := &stnrv2.ListenerConfig{
		Name:        l.tag.Name,
		Protocol:    l.tag.Proto.String(),
		Servers:     slices.Clone(l.servers),
		Addr:        l.addr,
		Port:        l.port,
		PublicAddr:  l.publicAddr,
		PublicPort:  l.publicPort,
		PublicAddrs: slices.Clone(l.publicAddrs),
		Filters:     slices.Clone(l.filters),
		Labels:      maps.Clone(l.labels),
	}
	if l.tls != nil {
		c.TLS = l.tls.DeepCopy()
	}
	l.mu.RUnlock()
	if l.connect != nil {
		c.Connect = l.connect.DeepCopy()
	}
	_ = c.Validate()
	return c
}

func (l *Listener) Status() stnrv2.Status {
	status := &stnrv2.ListenerStatus{ListenerConfig: l.GetConfig().(*stnrv2.ListenerConfig)}
	if l.rt.Telemetry != nil {
		status.Stats.Total = l.rt.Telemetry.Stats(l.Name(), telemetry.ListenerType)
	}
	if offloadStatus, ok := l.rt.GetStatus(runtime.TypeOffload, "").(*stnrv2.OffloadStatus); ok {
		status.Stats.Offload = offloadStatus.Listeners[l.Name()]
	}
	return status
}

// Start loads the certificate and binds the socket.
func (l *Listener) Start() error {
	l.mu.RLock()
	tlsConf := l.tls
	l.mu.RUnlock()
	keyPair, err := l.loadKeyPair(tlsConf)
	if err != nil {
		return err
	}
	l.mu.Lock()
	l.keyPair = keyPair
	l.mu.Unlock()
	ln, err := l.socket()
	if err != nil {
		return fmt.Errorf("failed to create %s listener at %s:%d: %w", l.tag.Proto.String(),
			l.addr, l.port, err)
	}
	l.ln = ln
	go l.accept()
	// the socket of a dialing listener reports the peer it dials as its address
	state := "running at"
	if l.connect != nil {
		state = "dialing"
	}
	l.log.Infof("listener %s: %s listener %s %s", l.Name(), l.tag.Proto.String(), state,
		ln.Addr().String())
	return nil
}

// Close closes the socket. Emitted conns stay up, except the conn of a dialing listener.
func (l *Listener) Close(_ bool) error {
	l.closeOnce.Do(func() { close(l.closed) })
	if l.ln == nil {
		return nil
	}
	if err := l.ln.Close(); err != nil && !util.IsClosedErr(err) {
		return err
	}
	return nil
}

// admits lets in the clients the filters of the listener admit, as they stand at the moment; a
// client no rule matches passes.
func (l *Listener) admits(client net.Addr) bool {
	l.mu.RLock()
	filters := l.filters
	l.mu.RUnlock()
	if len(filters) == 0 {
		return true
	}
	allow, matched := l.rt.Match(filters, client)
	ok := allow || !matched
	if !ok && l.rt.Tracing(l.scope) {
		l.log.Tracef("listener %s: filters drop client %s", l.Name(), client.String())
	}
	return ok
}

// serve hands a conn down the server chain, closing it unless a server consumes it.
func (l *Listener) serve(c api.Conn) {
	l.mu.RLock()
	servers := l.servers
	l.mu.RUnlock()
	for _, name := range servers {
		srv, ok := l.rt.Server(name)
		if !ok {
			l.log.Debugf("listener %s: no server %q for client %s", l.Name(), name,
				c.RemoteAddr().String())
			_ = c.Close()
			return
		}
		next, verdict, err := srv.Serve(c)
		if err != nil {
			l.log.Debugf("listener %s: server %q refused client %s: %s", l.Name(), name,
				c.RemoteAddr().String(), err.Error())
			_ = c.Close()
			return
		}
		if verdict == api.Consumed {
			return
		}
		c = next
	}
	if len(servers) > 0 {
		l.log.Warnf("listener %s: the last server %q passed client %s on: closing it", l.Name(),
			servers[len(servers)-1], c.RemoteAddr().String())
	}
	_ = c.Close()
}

func (l *Listener) accept() {
	for {
		c, err := l.ln.Accept()
		if err != nil {
			return
		}
		switch {
		case l.connect == nil && l.tag.Proto != stnrv2.ProtocolUDP &&
			l.tag.Proto != stnrv2.ProtocolDTLS && !l.admits(c.RemoteAddr()):
			// dialed conns are admitted by the dialer before it dials, UDP ones by the intake
			// before it makes a conn
			_ = c.Close()
		case l.connect == nil && (l.tag.Proto == stnrv2.ProtocolTLS || l.tag.Proto == stnrv2.ProtocolDTLS):
			if sc, handshake, err := l.secure(c); err != nil {
				l.log.Debugf("listener %s: cannot secure the conn of %s: %s", l.Name(),
					c.RemoteAddr().String(), err.Error())
				_ = c.Close()
			} else {
				go l.handshake(l.emit(sc), handshake)
			}
		default:
			// a plain conn, or a dialed one: the dialer has secured it
			l.sink(l.emit(c))
		}
	}
}

// emit accounts and tags a client conn; the conns of a dialing listener are accounted by its
// dialer.
func (l *Listener) emit(c net.Conn) api.Conn {
	if l.connect == nil {
		c = account.Conn(l.rt.Telemetry, c, l.Name(), telemetry.ListenerType)
	}
	tag := l.tag
	l.mu.RLock()
	if len(l.labels) > 0 {
		tag.Labels = l.labels
	}
	l.mu.RUnlock()
	return netconn.NewConn(c, tag, nil)
}

// handshake completes the TLS or DTLS handshake of a conn and hands it to the sink.
func (l *Listener) handshake(c api.Conn, handshake func(context.Context) error) {
	ctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
	defer cancel()
	if err := handshake(ctx); err != nil {
		l.log.Debugf("handshake with client %s failed: %s", c.RemoteAddr().String(), err.Error())
		_ = c.Close()
		return
	}
	select {
	case <-l.closed:
		_ = c.Close()
	default:
		l.sink(c)
	}
}
