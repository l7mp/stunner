// Package balancer chooses the peers of a server among the targets of its clusters, by a
// load-balancing policy.
package balancer

import (
	"fmt"
	"net/netip"

	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Balancer chooses a target among the targets of every cluster of the names, in order, and
// returns it with the name of its cluster; ok is false when the clusters have no target. The
// clusters are resolved by name at each call, so a reconcile applies to the next choice. A
// Balancer is safe for concurrent use.
type Balancer interface {
	Next(rt *runtime.Runtime, clusters []string) (cluster string, dst netip.AddrPort, ok bool)
}

// target is a target of a cluster.
type target struct {
	cluster string
	dst     netip.AddrPort
}

// New creates the balancer of a load-balancing policy.
func New(policy stnrv2.RoutingPolicy) (Balancer, error) {
	switch policy {
	case stnrv2.RoutingPolicyRoundRobin:
		return &roundRobin{}, nil
	}
	return nil, fmt.Errorf("unsupported load-balancing policy %q", policy.String())
}

// targets lists the targets of every cluster of the names, in order.
func targets(rt *runtime.Runtime, clusters []string) []target {
	ret := []target{}
	for _, name := range clusters {
		r, found := rt.Router(name)
		if !found {
			continue
		}
		for _, dst := range r.Targets() {
			ret = append(ret, target{cluster: name, dst: dst})
		}
	}
	return ret
}
