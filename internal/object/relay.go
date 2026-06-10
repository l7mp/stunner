package object

import (
	"fmt"
	"sync"

	"net"

	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// RelayConfig identifies one relay node of a cluster. It exists for set-membership diffing in
// the reconciler: the real relay state is derived from the parent cluster.
type RelayConfig struct {
	Cluster  string                 `json:"cluster"`
	Protocol stnrv1.ClusterProtocol `json:"protocol"`
}

func (c *RelayConfig) Validate() error { return nil }
func (c *RelayConfig) ConfigName() string {
	return runtime.RelayName(c.Cluster, c.Protocol)
}
func (c *RelayConfig) DeepEqual(other stnrv1.Config) bool {
	o, ok := other.(*RelayConfig)
	return ok && *c == *o
}
func (c *RelayConfig) DeepCopyInto(dst stnrv1.Config) {
	if d, ok := dst.(*RelayConfig); ok {
		*d = *c
	}
}
func (c *RelayConfig) String() string {
	return fmt.Sprintf("RelayConfig{cluster=%q,protocol=%s}", c.Cluster, c.Protocol.String())
}

// RelayNode is the lifecycle-only child node that owns the protocol-specific relay
// implementation of one Cluster. Start builds the implementation from the cluster's current
// state (and registers strict-DNS domains); Close tears it down. Peer matching delegates to
// the parent cluster so that endpoint updates are visible immediately after a cluster
// reconcile, independent of the relay lifecycle.
type RelayNode struct {
	name    string
	proto   stnrv1.ClusterProtocol
	cluster *Cluster

	mu   sync.Mutex
	impl runtime.ManagedRelay
}

// NewRelayNode creates a relay node for a cluster and protocol.
func NewRelayNode(cluster *Cluster, proto stnrv1.ClusterProtocol) *RelayNode {
	return &RelayNode{
		name:    runtime.RelayName(cluster.Name(), proto),
		proto:   proto,
		cluster: cluster,
	}
}

func (n *RelayNode) Name() string             { return n.name }
func (n *RelayNode) Type() runtime.ObjectType { return runtime.TypeRelay }

// Start builds the relay implementation from the cluster's current state and starts it
// (strict-DNS relays register their domains with the resolver).
func (n *RelayNode) Start() error {
	impl, err := n.getImpl()
	if err != nil {
		return err
	}
	return impl.Start()
}

// Close stops the relay implementation (strict-DNS relays unregister their domains).
func (n *RelayNode) Close(_ bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.impl == nil {
		return nil
	}
	err := n.impl.Close()
	n.impl = nil
	return err
}

// getImpl returns the relay implementation, building it lazily so allocations keep working
// across the close-reconcile-start window of a cluster restart.
func (n *RelayNode) getImpl() (runtime.ManagedRelay, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.impl != nil {
		return n.impl, nil
	}
	impl, err := n.cluster.newRelayImpl(n.proto)
	if err != nil {
		return nil, err
	}
	n.impl = impl
	return impl, nil
}

// RelayNode implements runtime.Relay so the router can hand it straight to the TURN
// allocation path.

func (n *RelayNode) ClusterName() string              { return n.cluster.Name() }
func (n *RelayNode) Protocol() stnrv1.ClusterProtocol { return n.proto }
func (n *RelayNode) Validate() error                  { return nil }

// Match delegates to the parent cluster: matching reflects the reconciled endpoint set
// immediately, regardless of the relay lifecycle state.
func (n *RelayNode) Match(peer net.IP, port int) bool {
	return n.cluster.Match(peer, port)
}

func (n *RelayNode) AllocatePacketConn(conf turn.AllocateListenerConfig) (net.PacketConn, net.Addr, error) {
	impl, err := n.getImpl()
	if err != nil {
		return nil, nil, err
	}
	return impl.AllocatePacketConn(conf)
}

func (n *RelayNode) AllocateListener(conf turn.AllocateListenerConfig) (net.Listener, net.Addr, error) {
	impl, err := n.getImpl()
	if err != nil {
		return nil, nil, err
	}
	return impl.AllocateListener(conf)
}

func (n *RelayNode) AllocateConn(conf turn.AllocateConnConfig) (net.Conn, error) {
	impl, err := n.getImpl()
	if err != nil {
		return nil, err
	}
	return impl.AllocateConn(conf)
}
