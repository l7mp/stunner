package runtime

import (
	"net"
	"strings"
	"sync/atomic"

	"k8s.io/utils/lru"

	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

const (
	listenerRouteCacheSize = 64
	peerRouteCacheSize     = 4096
)

// router is the default Router implementation, backed by listener-route and peer-route LRUs.
// Relays are resolved from the registry (one relay node per cluster and protocol).
type router struct {
	rt         *Runtime
	routeCache *lru.Cache
	peerCache  *lru.Cache
	epoch      atomic.Uint64
	log        logging.LeveledLogger
}

type routeCacheEntry struct {
	epoch    uint64
	routeKey string
	relays   map[stnrv1.ClusterProtocol][]Relay
	relayMap map[stnrv1.ClusterProtocol]map[string]Relay
}

type peerCacheValue struct {
	epoch   uint64
	proto   stnrv1.ClusterProtocol
	cluster string
}

// NewRouter returns the default Router backed by listener-route and peer-route LRUs.
func NewRouter(rt *Runtime) Router {
	return &router{
		rt:         rt,
		routeCache: lru.New(listenerRouteCacheSize),
		peerCache:  lru.New(peerRouteCacheSize),
		log:        rt.Logger.NewLogger("router"),
	}
}

func (r *router) Route(listener string, routes []string, peer net.IP, port int) (Relay, bool) {
	entry := r.getRouteEntry(listener, routes)

	if relay, ok := r.getPeerRelay(listener, stnrv1.ClusterProtocolUDP, peer, entry); ok {
		return relay, relay.Match(peer, port)
	}

	for _, relay := range entry.relays[stnrv1.ClusterProtocolUDP] {
		if relay.Match(peer, 0) {
			r.setPeer(listener, stnrv1.ClusterProtocolUDP, peer, relay.ClusterName(), entry.epoch)
			return relay, relay.Match(peer, port)
		}
	}

	return nil, false
}

func (r *router) InvalidateCache() {
	r.epoch.Add(1)
	r.routeCache.Clear()
	r.peerCache.Clear()
}

func (r *router) getRouteEntry(listener string, routes []string) *routeCacheEntry {
	curEpoch := r.epoch.Load()
	cacheKey := listener
	routeKey := strings.Join(routes, "\x00")

	if value, ok := r.routeCache.Get(cacheKey); ok {
		entry := value.(*routeCacheEntry)
		if entry.epoch == curEpoch && entry.routeKey == routeKey {
			return entry
		}
	}

	entry := &routeCacheEntry{
		epoch:    curEpoch,
		routeKey: routeKey,
		relays: map[stnrv1.ClusterProtocol][]Relay{
			stnrv1.ClusterProtocolUDP: {},
			stnrv1.ClusterProtocolTCP: {},
		},
		relayMap: map[stnrv1.ClusterProtocol]map[string]Relay{
			stnrv1.ClusterProtocolUDP: {},
			stnrv1.ClusterProtocolTCP: {},
		},
	}

	for _, name := range routes {
		if relay, ok := r.rt.GetRelay(name, stnrv1.ClusterProtocolUDP); ok {
			entry.relays[stnrv1.ClusterProtocolUDP] = append(entry.relays[stnrv1.ClusterProtocolUDP], relay)
			entry.relayMap[stnrv1.ClusterProtocolUDP][name] = relay
		}
		if relay, ok := r.rt.GetRelay(name, stnrv1.ClusterProtocolTCP); ok {
			entry.relays[stnrv1.ClusterProtocolTCP] = append(entry.relays[stnrv1.ClusterProtocolTCP], relay)
			entry.relayMap[stnrv1.ClusterProtocolTCP][name] = relay
		}
	}

	r.routeCache.Add(cacheKey, entry)
	return entry
}

func (r *router) getPeerRelay(listener string, proto stnrv1.ClusterProtocol, peer net.IP, entry *routeCacheEntry) (Relay, bool) {
	key := peerCacheKey(listener, proto, peer)
	v, ok := r.peerCache.Get(key)
	if !ok {
		return nil, false
	}

	p := v.(*peerCacheValue)
	if p.epoch != entry.epoch || p.proto != proto {
		r.peerCache.Remove(key)
		return nil, false
	}

	relay, found := entry.relayMap[proto][p.cluster]
	if !found {
		r.peerCache.Remove(key)
		return nil, false
	}

	if !relay.Match(peer, 0) {
		r.peerCache.Remove(key)
		return nil, false
	}

	return relay, true
}

func (r *router) setPeer(listener string, proto stnrv1.ClusterProtocol, peer net.IP, cluster string, epoch uint64) {
	r.peerCache.Add(peerCacheKey(listener, proto, peer), &peerCacheValue{
		epoch:   epoch,
		proto:   proto,
		cluster: cluster,
	})
}

func peerCacheKey(listener string, proto stnrv1.ClusterProtocol, peer net.IP) string {
	return listener + "|" + proto.String() + "|" + peer.String()
}
