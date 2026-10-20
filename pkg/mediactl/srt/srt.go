// Package srt holds the SRT dialer and listener of mediactl: a dialer calls for a stream in a
// mode, named in the access control syntax of the stream ID, a listener takes the callers of a
// passphrase, passes every published stream to the readers of the same stream itself and returns
// the other callers, for mediactl.Serve to echo.
package srt

import (
	"context"
	"net"
	"strings"
	"sync"

	gosrt "github.com/datarhei/gosrt"
	"github.com/pion/logging"

	srtsrv "github.com/l7mp/stunner/v2/internal/server/srt"
	"github.com/l7mp/stunner/v2/pkg/mediactl"
)

// Mode is what a caller does with its stream: the m= of the stream ID.
type Mode string

const (
	// Publish sends the stream.
	Publish Mode = "publish"
	// Request reads the stream.
	Request Mode = "request"
	// Echo sends packets and takes their echo: a stream ID with no mode.
	Echo Mode = ""
)

// StreamID returns the stream ID naming a stream in a mode: #!::r=<stream>[,m=<mode>].
func StreamID(stream string, m Mode) string {
	if m == Echo {
		return "#!::r=" + stream
	}
	return "#!::r=" + stream + ",m=" + string(m)
}

// Dialer calls SRT listeners for a stream in a mode, encrypted with the passphrase unless it is
// empty.
type Dialer struct {
	Stream     string
	Mode       Mode
	Passphrase string
}

// DialContext calls the SRT listener at address; the network is SRT whatever it says.
func (d Dialer) DialContext(_ context.Context, _, address string) (net.Conn, error) {
	cfg := gosrt.DefaultConfig()
	cfg.StreamId, cfg.Passphrase = StreamID(d.Stream, d.Mode), d.Passphrase
	return gosrt.Dial("srt", address, cfg)
}

// Config returns the stream config of a caller in a mode: a publisher sends, a reader only reads,
// an echo caller sends and takes the echo.
func Config(m Mode, c mediactl.Config) mediactl.Config {
	if m == Request {
		c.Rate = 0
	}
	c.Echo, c.ByteStream = m == Echo, false
	return c
}

// Listener takes the SRT callers at an address. A publisher's stream goes to the readers of the same
// stream inside the listener; Accept returns the other callers, as gosrt.Conns.
type Listener struct {
	ln         gosrt.Listener
	passphrase string
	log        logging.LeveledLogger

	mu      sync.Mutex
	streams map[string]gosrt.PubSub
}

// Listen takes SRT callers at an address; a caller with an encrypted stream must know the
// passphrase. The callers the listener refuses or serves itself are reported to the logger, unless
// nil.
func Listen(address, passphrase string, log logging.LeveledLogger) (*Listener, error) {
	ln, err := gosrt.Listen("srt", address, gosrt.DefaultConfig())
	if err != nil {
		return nil, err
	}
	return &Listener{ln: ln, passphrase: passphrase, log: log, streams: map[string]gosrt.PubSub{}}, nil
}

// Accept returns the next caller that neither publishes nor reads.
func (l *Listener) Accept() (net.Conn, error) {
	for {
		req, err := l.ln.Accept2()
		if err != nil {
			return nil, net.ErrClosed
		}
		if req.IsEncrypted() {
			if err := req.SetPassphrase(l.passphrase); err != nil {
				if l.log != nil {
					l.log.Infof("rejecting %s: %s", req.RemoteAddr(), err.Error())
				}
				req.Reject(gosrt.REJ_BADSECRET)
				continue
			}
		}
		conn, err := req.Accept()
		if err != nil {
			continue
		}
		sid := conn.StreamId()
		var m Mode
		if rest, ok := strings.CutPrefix(sid, "#!::"); ok {
			for kv := range strings.SplitSeq(rest, ",") {
				if v, ok := strings.CutPrefix(kv, "m="); ok {
					m = Mode(v)
				}
			}
		}
		if m != Publish && m != Request {
			return conn, nil
		}
		res := srtsrv.Resource(sid)
		l.mu.Lock()
		ps, ok := l.streams[res]
		if !ok {
			ps = gosrt.NewPubSub(gosrt.PubSubConfig{Logger: gosrt.NewLogger(nil)})
			l.streams[res] = ps
		}
		l.mu.Unlock()
		if l.log != nil {
			l.log.Infof("%s %s on stream %q", conn.RemoteAddr(), m, res)
		}
		go func() {
			defer conn.Close() //nolint:errcheck
			if m == Publish {
				_ = ps.Publish(conn)
			} else {
				_ = ps.Subscribe(conn)
			}
			if l.log != nil {
				l.log.Infof("%s on stream %q closed", conn.RemoteAddr(), res)
			}
		}()
	}
}

// Close stops taking callers.
func (l *Listener) Close() error {
	l.ln.Close()
	return nil
}

// Addr returns the address the listener takes callers at.
func (l *Listener) Addr() net.Addr { return l.ln.Addr() }
