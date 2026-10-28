// Package offload implements the kernel offload of relayed UDP flows. The open-source build ships
// only the null engine; New dispatches through the engineConstructor seam.
//
// An offloaded flow is a pair of sides, each a 4-tuple and a channel: a packet arriving on one side
// leaves on the other, its addresses and ports rewritten and the ChannelData framing changed from
// the channel of its side to the channel of the other, 0 meaning raw UDP. So one shape covers the
// TURN channels (ChannelData towards the client, raw towards the peer), the L4 flows (raw both
// ways, or ChannelData towards an upstream TURN server), the SRT streams and the RTP media (raw
// both ways). A side's punt rules, and a STUN header on any side, send a packet to user space
// instead, so the server keeps seeing what it must (a handshake, a shutdown, a BYE). TLS and DTLS
// encrypt the payload and streams carry no datagrams: those flows report no wire and stay in user
// space.
package offload

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/telemetry"
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
// shutdown.
func New(deps Deps) Engine { return engineConstructor(deps) }

// ErrOffloadLimit is the error of an Upsert beyond the offload limit of the license: the flow
// stays in user space.
var ErrOffloadLimit = errors.New("offload limit reached")

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
	// Upsert offloads the flow between two sides, both directions, or rewrites it: a packet
	// arriving on a is sent out on b and the other way round. The listener and the cluster name
	// the stats of the two sides. Since eBPF maps are LRU, an offload can lose part of itself;
	// upsert repairs it but resets the Packets counter. An error leaves the flow in user space.
	Upsert(a, b Connection, listenerName, clusterName string) error
	// Remove removes an offloaded flow. Removing a flow that is not offloaded is not an error.
	Remove(a, b Connection) error
	// Stats returns the last cached offload statistics, keyed by object name-hash and direction.
	Stats() (StatMap, error)
	// Packets returns the packet counter of a flow, both directions, and whether the flow is
	// offloaded. The counter is monotone between upserts; Upsert resets it.
	Packets(a, b Connection) (uint64, bool)
	// Flows returns how many flows the engine holds and the offload limit it admits them up to:
	// 0 is unlimited, a negative value unknown.
	Flows() (live, limit int)
}

// stats flags.
const (
	// FlagListener marks a stat as belonging to a listener, otherwise a cluster.
	FlagListener uint8 = 1 << 0
	// FlagDirIn marks a stat as inbound (DIR_IN), otherwise outbound (DIR_OUT).
	FlagDirIn uint8 = 1 << 1
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

// Connection is one side of an offloaded flow: a packet arriving on it comes from RemoteAddr to
// LocalAddr, framed as ChannelData of ChannelID, 0 meaning raw UDP; a packet leaving on it goes from
// LocalAddr to RemoteAddr, framed the same way. A packet arriving that matches one of the side's
// punt rules goes to user space instead.
type Connection struct {
	RemoteAddr net.Addr
	LocalAddr  net.Addr
	// Protocol is the transport, one of the Protocol constants above. The engines compare it
	// verbatim, so the spelling is part of the contract.
	Protocol  string
	ChannelID uint32
	Punt      []PuntRule
}

// PuntRule matches a packet by a 32-bit big-endian word of its UDP payload at Offset: the packet
// matches when the word masked with Mask equals Value.
type PuntRule struct {
	Offset      uint16
	Mask, Value uint32
}

// Match reports whether the rule matches a UDP payload; a payload too short for the word never
// matches. The engines implement the same match in the kernel.
func (r PuntRule) Match(payload []byte) bool {
	end := int(r.Offset) + 4
	return end <= len(payload) && binary.BigEndian.Uint32(payload[r.Offset:end])&r.Mask == r.Value
}

// MaxPuntRules is the number of punt rules a side carries at most.
const MaxPuntRules = 2

func (c *Connection) String() string {
	return fmt.Sprintf("%s:local:%s-remote:%s-chan:%d",
		c.RemoteAddr.Network(), c.LocalAddr.String(), c.RemoteAddr.String(),
		c.ChannelID)
}
