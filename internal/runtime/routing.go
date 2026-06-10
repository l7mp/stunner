package runtime

import (
	"net"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// SelectConnRelay selects a relay for a downstream connection-oriented allocation.
func SelectConnRelay(rt *Runtime, listener string, remote net.Addr) (Relay, bool) {
	lconf, ok := rt.GetConfig(TypeListener, listener).(*stnrv1.ListenerConfig)
	if !ok {
		return nil, false
	}
	proto, peer, port, ok := protocolFromRemoteAddr(remote)
	if !ok {
		return nil, false
	}

	for _, cluster := range lconf.Routes {
		relay, ok := rt.GetRelay(cluster, proto)
		if !ok {
			continue
		}
		if relay.Match(peer, 0) {
			return relay, relay.Match(peer, port)
		}
	}

	return nil, false
}

// SelectListenerRelay selects a relay for a downstream listener-side allocation.
func SelectListenerRelay(rt *Runtime, listener string, proto stnrv1.ClusterProtocol) (Relay, bool) {
	lconf, ok := rt.GetConfig(TypeListener, listener).(*stnrv1.ListenerConfig)
	if !ok {
		return nil, false
	}
	for _, cluster := range lconf.Routes {
		relay, ok := rt.GetRelay(cluster, proto)
		if ok {
			return relay, true
		}
	}

	return nil, false
}

func protocolFromRemoteAddr(remote net.Addr) (stnrv1.ClusterProtocol, net.IP, int, bool) {
	switch r := remote.(type) {
	case *net.UDPAddr:
		return stnrv1.ClusterProtocolUDP, r.IP, r.Port, true
	case *net.TCPAddr:
		return stnrv1.ClusterProtocolTCP, r.IP, r.Port, true
	default:
		return stnrv1.ClusterProtocolUnknown, nil, 0, false
	}
}
