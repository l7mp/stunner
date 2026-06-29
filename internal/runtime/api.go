package runtime

import (
	"net"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// This file declares the interfaces of the runtime services that the Runtime stores and hands
// out. The concrete implementations live in their own packages (internal/router, internal/quota,
// ...) and satisfy these interfaces structurally. Keeping the interface here — rather than
// importing the implementation package into runtime — lets those packages import runtime (and so
// reach every other runtime service, e.g. the license manager) without an import cycle.

// Router resolves which cluster serves a peer and whether a cluster admits a peer. It is the
// single authority on routing and admission: matching lives here, not on the cluster objects.
// Implemented by internal/router.
type Router interface {
	// Route returns the name of the cluster on the listener's routes that admits (peer, port)
	// for the given protocol, or ("", false) if none does. port==0 ignores the port.
	Route(listener string, routes []string, proto stnrv1.ClusterProtocol, peer net.IP, port int) (string, bool)
	// Match reports whether the named cluster admits (peer, port). port==0 ignores the port.
	Match(cluster string, peer net.IP, port int) bool
	// InvalidateCache drops all cached routing state; call after a config change.
	InvalidateCache()
}

// QuotaHandler tracks per-user TURN allocation quotas. CheckAndIncrement reports whether a new
// allocation is admissible for the (username, realm) pair given the quota and accounts for it;
// Decrement releases one previously admitted allocation. Implemented by internal/quota.
type QuotaHandler interface {
	CheckAndIncrement(username, realm string, quota int) bool
	Decrement(username, realm string)
}
