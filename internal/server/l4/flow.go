package l4

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/l7mp/stunner/v2/internal/netconn/classify"
)

// bufferSize is the pump chunk size: one read makes one datagram on a datagram leg, so a
// stream-to-datagram flow frames the stream into MTU-sized datagrams (and the reverse
// direction preserves datagram boundaries only as chunk boundaries in the stream).
const bufferSize = 1500

// errQuotaExceeded rejects a flow the quota gate refused.
var errQuotaExceeded = errors.New("flow quota exceeded")

// Flow is one relayed client Flow: the client-side conn, the relay leg towards the pinned
// peer (a packet conn for datagram legs, a conn for stream legs), and the idle machinery.
type Flow struct {
	s           *Server
	client      net.Conn
	RelayPacket classify.PacketConn
	RelayStream classify.Conn
	Peer        net.Addr
	Event       FlowEvent
	closed      atomic.Bool // teardown flag, checked by the offload housekeeping

	last      atomic.Int64 // UnixNano of the last activity in either direction
	timer     *time.Timer
	closeOnce sync.Once
}

// run starts the peer-side pump and runs the client-side pump; it returns on flow teardown.
func (f *Flow) run() {
	go f.pumpPeerToClient()
	f.pumpClientToPeer()
}

func (f *Flow) pumpClientToPeer() {
	buf := make([]byte, bufferSize)
	for {
		n, err := f.client.Read(buf)
		if err != nil {
			// io.EOF included: a TCP FIN or stdin EOF ends the flow
			f.close("client side closed")
			return
		}
		f.touch()
		if f.RelayPacket != nil {
			_, err = f.RelayPacket.WriteTo(buf[:n], f.Peer)
		} else {
			_, err = f.RelayStream.Write(buf[:n])
		}
		if err != nil {
			f.s.events.OnFlowError(f.client.RemoteAddr(), f.Event.Protocol,
				"peer side write error: "+err.Error())
			f.close("peer side write error")
			return
		}
	}
}

func (f *Flow) pumpPeerToClient() {
	buf := make([]byte, bufferSize)
	for {
		var n int
		var err error
		if f.RelayPacket != nil {
			var from net.Addr
			n, from, err = f.RelayPacket.ReadFrom(buf)
			// the flow is pinned to its peer: drop datagrams from anyone else (the
			// classified leg has already dropped unclassified sources)
			if err == nil && from.String() != f.Peer.String() {
				continue
			}
		} else {
			n, err = f.RelayStream.Read(buf)
		}
		if err != nil {
			f.close("peer side closed")
			return
		}
		f.touch()
		if _, err := f.client.Write(buf[:n]); err != nil {
			f.s.events.OnFlowError(f.client.RemoteAddr(), f.Event.Protocol,
				"client side write error: "+err.Error())
			f.close("client side write error")
			return
		}
	}
}

func (f *Flow) touch() { f.last.Store(time.Now().UnixNano()) }

// checkIdle fires on the coarse per-flow timer: it keeps the flow alive ONLY if either:
// - the flow has sent a user space packet in any direction for the idle timeout OR
// - the kernel reports the flow as being offloaded AND currently active.
// otherwise the flow is torn down.
func (f *Flow) checkIdle() {
	elapsed := time.Duration(time.Now().UnixNano() - f.last.Load())
	if elapsed >= f.s.idle {
		if moved, known := f.s.offload.Active(f); known && moved {
			f.touch()
			f.timer.Reset(f.s.idle)
			return
		}
		f.close("idle timeout")
		return
	}
	f.timer.Reset(f.s.idle - elapsed)
}

func (f *Flow) close(reason string) {
	f.closeOnce.Do(func() {
		f.timer.Stop()
		f.closed.Store(true)
		f.s.removeFlow(f)
		f.s.log.Debugf("closing flow from client %s: %s",
			f.client.RemoteAddr().String(), reason)
		f.s.offload.Remove(f)
		f.s.events.OnFlowDeleted(f.Event)
		_ = f.client.Close()
		f.closeLeg()
	})
}

func (f *Flow) closeLeg() {
	if f.RelayPacket != nil {
		_ = f.RelayPacket.Close()
	} else {
		_ = f.RelayStream.Close()
	}
}
