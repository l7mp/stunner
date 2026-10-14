package dialer

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// The redial backoff of a dialing listener follows the kubelet restart backoff: the first dial is
// immediate, every failure or short-lived conn doubles the delay from dialBackoffInitial up to
// dialBackoffMax, and a conn that stayed up for dialBackoffReset resets it.
var (
	dialBackoffInitial = 10 * time.Second
	dialBackoffMax     = 5 * time.Minute
	dialBackoffReset   = 10 * time.Minute
)

// NewListener returns the socket of a dialing listener: Accept dials the peer of conf.Connect,
// from the listener's address and port if set, through a dialer built from the listener config:
// plain, TLS or DTLS by the listener's protocol, admitting the peers admit admits, accounted as
// the listener. STDIN and FIFO listeners do not dial.
func NewListener(conf *stnrv2.ListenerConfig, rt *runtime.Runtime, admit func(net.Addr) bool) (net.Listener, error) {
	proto, err := stnrv2.NewListenerProtocol(conf.Protocol)
	if err != nil {
		return nil, err
	}
	cluster := &stnrv2.ClusterConfig{Name: conf.Name, Protocol: stnrv2.ProtocolTCP.String()}
	switch proto {
	case stnrv2.ProtocolUDP:
		cluster.Protocol = stnrv2.ProtocolUDP.String()
	case stnrv2.ProtocolDTLS:
		cluster.Protocol, cluster.TLS = stnrv2.ProtocolUDP.String(), &stnrv2.TLSConfig{}
	case stnrv2.ProtocolTCP:
	case stnrv2.ProtocolTLS:
		cluster.TLS = &stnrv2.TLSConfig{}
	default:
		return nil, fmt.Errorf("listener %q: connect mode is not supported for %s listeners",
			conf.Name, proto.String())
	}
	if cluster.TLS != nil && conf.Connect.TLS != nil {
		cluster.TLS = conf.Connect.TLS
	}
	d, err := newDialer(cluster, rt, admit, telemetry.ListenerType)
	if err != nil {
		return nil, err
	}

	var local net.Addr
	if conf.Addr != "" || conf.Port != 0 {
		ip := net.ParseIP(conf.Addr)
		if conf.Addr != "" && ip == nil {
			return nil, fmt.Errorf("listener %q: invalid local address %q", conf.Name, conf.Addr)
		}
		local = &net.TCPAddr{IP: ip, Port: conf.Port}
		if cluster.Protocol == stnrv2.ProtocolUDP.String() {
			local = &net.UDPAddr{IP: ip, Port: conf.Port}
		}
	}
	log, addr := rt.Logger.NewLogger(fmt.Sprintf("listener-%s", conf.Name)), conf.Connect.Address
	return &dialListener{
		dial: func() (net.Conn, error) {
			c, err := d.Dial(context.Background(), local, addr)
			if err != nil {
				log.Warnf("listener %s: cannot dial %s: %s", conf.Name, addr, err.Error())
				return nil, err
			}
			log.Infof("listener %s: connected to %s from %s", conf.Name, c.RemoteAddr().String(),
				c.LocalAddr().String())
			return c, nil
		},
		addr:   peerAddr{network: strings.ToLower(cluster.Protocol), address: addr},
		closed: make(chan struct{}),
	}, nil
}

// dialListener is the socket of a dialing listener: Accept dials the peer, and the next Accept
// waits until that conn closes and redials with backoff. Close stops dialing and closes the
// current conn.
type dialListener struct {
	dial   func() (net.Conn, error)
	addr   net.Addr
	delay  time.Duration
	closed chan struct{}
	once   sync.Once

	mu  sync.Mutex
	cur *dialedConn
}

func (d *dialListener) Accept() (net.Conn, error) {
	d.mu.Lock()
	cur := d.cur
	d.mu.Unlock()
	if cur != nil {
		select {
		case <-cur.done:
		case <-d.closed:
			return nil, net.ErrClosed
		}
		if time.Since(cur.start) >= dialBackoffReset {
			d.delay = 0
		} else {
			d.delay = nextDialDelay(d.delay)
		}
	}
	for {
		select {
		case <-time.After(d.delay):
		case <-d.closed:
			return nil, net.ErrClosed
		}
		c, err := d.dial()
		if err != nil {
			d.delay = nextDialDelay(d.delay)
			continue
		}
		dc := &dialedConn{Conn: c, start: time.Now(), done: make(chan struct{})}
		d.mu.Lock()
		defer d.mu.Unlock()
		select {
		case <-d.closed:
			_ = c.Close()
			return nil, net.ErrClosed
		default:
		}
		d.cur = dc
		return dc, nil
	}
}

func (d *dialListener) Close() error {
	d.once.Do(func() { close(d.closed) })
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.cur != nil {
		_ = d.cur.Close()
	}
	return nil
}

func (d *dialListener) Addr() net.Addr { return d.addr }

func nextDialDelay(d time.Duration) time.Duration {
	if d == 0 {
		return dialBackoffInitial
	}
	return min(2*d, dialBackoffMax)
}

// dialedConn is a conn of a dialing listener: closing it, by whoever, wakes the listener.
type dialedConn struct {
	net.Conn
	start time.Time
	done  chan struct{}
	once  sync.Once
}

func (c *dialedConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return c.Conn.Close()
}

// peerAddr is the address a dialing listener dials, as given.
type peerAddr struct{ network, address string }

func (a peerAddr) Network() string { return a.network }
func (a peerAddr) String() string  { return a.address }
