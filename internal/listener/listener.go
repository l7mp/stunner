// Package listener implements the client-facing side of the dataplane.
package listener

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/pion/dtls/v3"
	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	"github.com/l7mp/stunner/v2/internal/netconn/account"
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	"github.com/l7mp/stunner/v2/internal/util"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/udp"
)

// Sink takes the client conns a listener emits.
type Sink func(api.Conn)

// handshakeTimeout bounds the TLS or DTLS handshake of a new client.
const handshakeTimeout = 10 * time.Second

// newTLSConfig builds the TLS configuration of a TLS listener. The open-source build ignores the
// PQC mode; a build with the PQC feature replaces it.
var newTLSConfig = func(_ *runtime.Runtime, conf *stnrv2.ListenerConfig, cert tls.Certificate, log logging.LeveledLogger) *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		Certificates: []tls.Certificate{cert},
	}
}

// Listener accepts the client conns of one listener config and hands them, tagged and accounted,
// to its sink: a TLS or DTLS conn once its handshake has completed.
type Listener struct {
	conf stnrv2.ListenerConfig
	tag  api.Tag
	rt   *runtime.Runtime
	log  logging.LeveledLogger
	sink Sink

	listen func() (net.Listener, error)
	// secure wraps an accepted conn in TLS or DTLS, nil for plain listeners.
	secure func(net.Conn) (net.Conn, func(context.Context) error, error)

	ln        net.Listener
	closed    chan struct{}
	closeOnce sync.Once
}

var _ api.Listener = &Listener{}

// New creates the listener of a listener config, with the TLS certificate and key in PEM.
func New(conf *stnrv2.ListenerConfig, rt *runtime.Runtime, sink Sink) (*Listener, error) {
	proto, err := stnrv2.NewListenerProtocol(conf.Protocol)
	if err != nil {
		return nil, err
	}
	l := &Listener{
		conf:   *conf,
		tag:    api.Tag{Name: conf.Name, Proto: proto},
		rt:     rt,
		log:    rt.Logger.NewLogger(fmt.Sprintf("listener-%s", conf.Name)),
		sink:   sink,
		closed: make(chan struct{}),
	}

	addr := net.JoinHostPort(conf.Addr, strconv.Itoa(conf.Port))
	listenUDP := func() (net.Listener, error) {
		lc := udp.DefaultListenConfig
		lc.Net = rt.Net
		return lc.Listen("udp", addr)
	}
	listenTCP := func() (net.Listener, error) {
		return rt.Net.CreateListenConfig(&net.ListenConfig{}).Listen(context.Background(), "tcp", addr)
	}
	switch proto {
	case stnrv2.ProtocolUDP:
		l.listen = listenUDP
	case stnrv2.ProtocolDTLS:
		cert, err := tls.X509KeyPair([]byte(conf.Cert), []byte(conf.Key))
		if err != nil {
			return nil, fmt.Errorf("cannot load cert/key pair for DTLS listener %s: %w",
				conf.Name, err)
		}
		l.listen = listenUDP
		l.secure = func(c net.Conn) (net.Conn, func(context.Context) error, error) {
			dc, err := dtls.ServerWithOptions(packetConn{c}, c.RemoteAddr(), dtls.WithCertificates(cert))
			if err != nil {
				return nil, nil, err
			}
			return dc, dc.HandshakeContext, nil
		}
	case stnrv2.ProtocolTCP:
		l.listen = listenTCP
	case stnrv2.ProtocolTLS:
		cert, err := tls.X509KeyPair([]byte(conf.Cert), []byte(conf.Key))
		if err != nil {
			return nil, fmt.Errorf("cannot load cert/key pair for TLS listener %s: %w",
				conf.Name, err)
		}
		tlsConf := newTLSConfig(rt, conf, cert, l.log)
		l.listen = listenTCP
		l.secure = func(c net.Conn) (net.Conn, func(context.Context) error, error) {
			tc := tls.Server(c, tlsConf)
			return tc, tc.HandshakeContext, nil
		}
	case stnrv2.ProtocolSTDIN:
		l.listen = func() (net.Listener, error) { return newStdioListener(), nil }
	default:
		return nil, fmt.Errorf("unsupported listener protocol %q", proto.String())
	}
	return l, nil
}

func (l *Listener) Name() string { return l.conf.Name }

func (l *Listener) Start() error {
	ln, err := l.listen()
	if err != nil {
		return fmt.Errorf("failed to create %s listener at %s:%d: %w", l.tag.Proto.String(),
			l.conf.Addr, l.conf.Port, err)
	}
	l.ln = ln
	go l.accept()
	l.log.Infof("listener %s: %s listener running at %s", l.conf.Name, l.tag.Proto.String(),
		ln.Addr().String())
	return nil
}

// Close closes the listener. Emitted conns stay up.
func (l *Listener) Close() error {
	l.closeOnce.Do(func() { close(l.closed) })
	if l.ln == nil {
		return nil
	}
	if err := l.ln.Close(); err != nil && !util.IsClosedErr(err) {
		return err
	}
	return nil
}

func (l *Listener) accept() {
	for {
		c, err := l.ln.Accept()
		if err != nil {
			return
		}
		if l.secure == nil {
			l.sink(l.emit(c))
			continue
		}
		sc, handshake, err := l.secure(c)
		if err != nil {
			_ = c.Close()
			continue
		}
		go l.handshake(l.emit(sc), handshake)
	}
}

// emit accounts and tags a client conn.
func (l *Listener) emit(c net.Conn) api.Conn {
	accounted := account.Conn(l.rt.Telemetry, c, l.conf.Name, telemetry.ListenerType)
	return netconn.NewConn(accounted, l.tag, nil)
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

// packetConn is the datagram view of a connected UDP conn that pion/dtls runs over.
type packetConn struct {
	net.Conn
}

func (c packetConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, err := c.Read(p)
	return n, c.RemoteAddr(), err
}

func (c packetConn) WriteTo(p []byte, _ net.Addr) (int, error) { return c.Write(p) }
