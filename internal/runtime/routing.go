package runtime

import (
	"net"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// RouteAny resolves the cluster serving a peer across all cluster protocols (UDP preferred). Used
// for IP-level permission decisions where the flow protocol is not yet known: protocol and port
// specificity are enforced at flow establishment.
func RouteAny(rt *Runtime, listener string, routes []string, peer net.IP) (string, bool) {
	for _, proto := range []stnrv1.ClusterProtocol{stnrv1.ClusterProtocolUDP, stnrv1.ClusterProtocolTCP} {
		if cluster, ok := rt.Router.Route(listener, routes, proto, peer, 0); ok {
			return cluster, true
		}
	}
	return "", false
}
