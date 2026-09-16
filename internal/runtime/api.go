package runtime

import (
	"net"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// This file declares the interfaces of the runtime services that the Runtime stores and hands
// out. The concrete implementations live in their own packages (internal/router, internal/quota,
// ...) and satisfy these interfaces structurally. Keeping the interface here, rather than
// importing the implementation package into runtime, lets those packages import runtime (and so
// reach every other runtime service, e.g. the license manager) without an import cycle.

// Router finds the cluster that serves a request on a listener. It is deliberately dumb: it looks
// up the listener's routes and walks them in order, handing each cluster object to a matcher;
// what a cluster admits is the cluster's business. RoutePeer is the packet-path variant that
// caches its verdicts; the cache is invalidated explicitly at every point routing state changes
// (listener and cluster reconcile, DNS re-resolution). Implemented by internal/router.
type Router interface {
	// Route returns the first cluster on the listener's routes satisfying the matcher.
	Route(listener string, match func(Cluster) bool) (Cluster, bool)
	// RoutePeer returns the name of the first cluster on the listener's routes with the given
	// protocol that admits (peer, port), caching the verdict. port==0 ignores the port.
	RoutePeer(listener string, proto stnrv1.ClusterProtocol, peer net.IP, port int) (string, bool)
	// InvalidateCache drops all cached routing state; call whenever routing state changes.
	InvalidateCache()
}

// Cluster is the routing surface a cluster object exposes to the packet path: its name, its
// protocol, the upstream TURN server it names (nil for direct clusters), and its own admission
// verdict. Implemented by internal/object.Cluster; reach it by asserting a Registry object.
type Cluster interface {
	// Name returns the cluster name.
	Name() string
	// Protocol returns the cluster's protocol.
	Protocol() stnrv1.ClusterProtocol
	// TURNServer returns the upstream TURN server of a TURN-* protocol cluster, nil otherwise.
	TURNServer() *stnrv1.TURNServer
	// Admits reports whether the cluster admits (peer, port) on its own terms: a direct
	// cluster admits the peers its endpoints name, a TURN-* cluster admits every peer since
	// admission is then the upstream TURN server's job. port==0 ignores the port.
	Admits(peer net.IP, port int) bool
}

// QuotaHandler tracks per-user TURN allocation quotas. CheckAndIncrement reports whether a new
// allocation is admissible for the (username, realm) pair given the quota and accounts for it;
// Decrement releases one previously admitted allocation. Implemented by internal/quota.
type QuotaHandler interface {
	CheckAndIncrement(username, realm string, quota int) bool
	Decrement(username, realm string)
}
