package mediactl

import (
	"context"
	"errors"
	"net"
	"time"

	"github.com/pion/logging"
)

// Dialer makes the conns of a sender: net.Dialer for UDP and TCP, and the dialers of the
// subpackages for SIP calls, SRT streams and TURN relays.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Echo sends back everything it reads on a conn until the conn fails or, for a datagram conn,
// stays silent for FlowIdle; then it closes the conn and returns what it received. It counts the
// packets and the bytes only: the sender measures the loss and the round trip.
func Echo(conn net.Conn, datagram bool) *Stats {
	s := NewStats()
	buf := make([]byte, 65536)
	for {
		if datagram {
			_ = conn.SetReadDeadline(time.Now().Add(FlowIdle))
		}
		n, err := conn.Read(buf)
		if err != nil {
			break
		}
		s.Received(buf[:n], false)
		if _, err := conn.Write(buf[:n]); err != nil {
			break
		}
	}
	_ = conn.Close()
	return s
}

// Serve echoes every conn a listener accepts until the listener fails: UDP clients of pkg/udp,
// TCP conns, SIP calls of a sip.Phone, SRT callers. Every conn is reported to the logger (unless
// nil) when its echo ends.
func Serve(l net.Listener, datagram bool, log logging.LeveledLogger) error {
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		if log != nil {
			log.Infof("echoing %s", conn.RemoteAddr())
		}
		go func() {
			s := Echo(conn, datagram)
			if log != nil {
				log.Infof("flow %s: %s", conn.RemoteAddr(), s)
			}
		}()
	}
}

// IsClosed tells whether an error is that of a closed conn or listener, the error a server
// returns when its run ends.
func IsClosed(err error) bool { return errors.Is(err, net.ErrClosed) }
