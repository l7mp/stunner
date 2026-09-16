// Package offload implements a kernel-offload engine to speed up transporting ChannelData
// messages. The open-source build ships only the null engine; New dispatches through the
// engineConstructor seam.
package offload

import (
	"fmt"
	"net"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/telemetry"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	licensecfg "github.com/l7mp/stunner/v2/pkg/config/license"
)

// Deps are the process-wide dependencies of the offload engine, set once at construction.
type Deps struct {
	Telemetry *telemetry.Telemetry
	License   licensecfg.ConfigManager
	Log       logging.LeveledLogger
}

var engineConstructor = func(Deps) Engine { return NewNullEngine() }

// New builds the offload engine. The engine is a process-wide singleton with the lifetime of the
// server: it is created once at startup, Start pins the eBPF maps, and Close unpins them on
// shutdown. Config changes (engine mode, interfaces) arrive through Reconcile and must not bounce
// the pinned maps.
func New(deps Deps) Engine { return engineConstructor(deps) }

// Engine provides a general interface for offloading techniques (e.g., XDP). The engine instance
// is a process-wide singleton (rt.OffloadEngine) with the server's lifetime; Start pins the eBPF
// maps and attaches to interfaces, Close unpins them. An offload-config change is applied by the
// Offload object as a Close()+Start() (re-pin), since the engine has no lighter in-place path.
type Engine interface {
	// Name returns the offload engine type.
	Name() string
	// Start pins the eBPF maps and attaches the program to the given interfaces, selecting the
	// mechanism from mode ("none"/"tc"/"xdp"/"auto"). Re-callable: a config change closes then
	// restarts the engine.
	Start(mode string, interfaces []string) error
	// Close detaches and unpins the eBPF maps.
	Close() error
	// Upsert establishes a new offloaded connection on the engine or modifies an existing one.
	Upsert(client, peer Connection, listenerName, clusterName string) error
	// Remove removes an offloaded connection. Removing a connection that is not offloaded
	// is not an error.
	Remove(client, peer Connection) error
	// Stats returns the last cached offload statistics, keyed by object name-hash and direction.
	Stats() (StatMap, error)
}

// stats flags.
const (
	// FlagListener marks a stat as belonging to a listener, otherwise a cluster.
	FlagListener uint8 = 1 << 0
	// FlagDirIn marks a stat as inbound (DIR_IN), otherwise outbound (DIR_OUT).
	FlagDirIn uint8 = 2 << 0
)

// IsListener reports whether the stat flags mark a listener.
func IsListener(flag uint8) bool { return flag&FlagListener == FlagListener }

// IsDirIn reports whether the stat flags mark inbound traffic.
func IsDirIn(flag uint8) bool { return flag&FlagDirIn == FlagDirIn }

// NameHash hashes an object name into the 16-bit identifier used by the offload datapath.
func NameHash(s string) uint16 {
	var hash uint16
	for i := 0; i < len(s); i++ {
		hash = (hash << 5) + hash + uint16(s[i]) // hash * 33 + char
	}
	return hash
}

// StatKey identifies an offload statistic by object name-hash and flags.
type StatKey struct {
	NameHash uint16
	Flags    uint8
}

// StatInfo holds a single offload statistics sample.
type StatInfo struct {
	Pkts          uint64
	Bytes         uint64
	TimestampLast uint64
}

// StatMap maps stat keys to their last cached samples.
type StatMap = map[StatKey]StatInfo

// Protocol values of a Connection: the transport as the TURN five-tuple names it, which is what
// the engines match on.
const (
	ProtocolUDP = "UDP"
	ProtocolTCP = "TCP"
)

// Offloadable reports whether the leg between a listener and a cluster of the given protocols is
// one the engines can accelerate. They implement exactly one shape: plaintext ChannelData on one
// side of the leg, raw datagrams on the other, decapsulated one way and encapsulated back. Only
// two protocol pairs have that shape, and they are mirror images: a turn-udp listener relaying to
// a udp cluster, where the ChannelData arrives from the client; and a udp listener tunnelling to
// a turn-udp cluster, where it leaves towards the upstream server.
//
// Everything else stays in user space. A leg that is raw on both sides has no channel to strip. A
// leg carrying ChannelData on both sides (TURN relay chaining) would need the datapath to rewrite
// one channel number into another, which the maps cannot express. turn-tls and turn-dtls encrypt
// the ChannelData, so there is nothing parseable on the wire. And a stream or stdin leg carries no
// datagrams to match a 4-tuple against.
//
// Both arguments are the same underlying type, so keep them in order: the listener first.
func Offloadable(listener stnrv1.ListenerProtocol, cluster stnrv1.ClusterProtocol) bool {
	switch listener {
	case stnrv1.ListenerProtocolTURNUDP:
		return cluster == stnrv1.ClusterProtocolUDP
	case stnrv1.ListenerProtocolUDP:
		return cluster == stnrv1.ClusterProtocolTURNUDP
	default:
		return false
	}
}

// Connection combines the offload engine identifiers required for uniquely identifying an
// allocation channel binding. RemoteAddr and LocalAddr are the source and the destination of a
// packet arriving on this side of the offloaded pair. Depending on the engine, some values are
// unused (e.g., SocketFd has no role for an XDP offload).
type Connection struct {
	RemoteAddr net.Addr
	LocalAddr  net.Addr
	// Protocol is the transport, one of the Protocol constants above. The engines compare it
	// verbatim, so the spelling is part of the contract.
	Protocol  string
	SocketFd  uintptr
	ChannelID uint32
}

func (c *Connection) String() string {
	return fmt.Sprintf("%s:local:%s-remote:%s-chan:%d",
		c.RemoteAddr.Network(), c.LocalAddr.String(), c.RemoteAddr.String(),
		c.ChannelID)
}
