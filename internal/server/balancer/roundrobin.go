package balancer

import (
	"net/netip"
	"sync/atomic"

	"github.com/l7mp/stunner/v2/internal/runtime"
)

// roundRobin chooses the targets in turn.
type roundRobin struct {
	next atomic.Uint64
}

func (b *roundRobin) Next(rt *runtime.Runtime, clusters []string) (string, netip.AddrPort, bool) {
	ts := targets(rt, clusters)
	if len(ts) == 0 {
		return "", netip.AddrPort{}, false
	}
	t := ts[int((b.next.Add(1)-1)%uint64(len(ts)))]
	return t.cluster, t.dst, true
}
