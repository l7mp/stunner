// Package router resolves which cluster serves a peer and whether a cluster admits a peer. It is
// the single authority on routing and admission for the relay path: cluster endpoint matching
// lives here, not on the cluster objects. The router is not TURN-specific — clusters are plain
// UDP or TCP — so it can be reused by future pure UDP/TCP listeners.
package router

import (
	"net"
	"strings"
	"sync/atomic"

	"k8s.io/utils/lru"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/util"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

const (
	matcherCacheSize       = 256
	listenerRouteCacheSize = 64
	peerRouteCacheSize     = 4096
)

var _ runtime.Router = (*router)(nil)

// router is the default runtime.Router. The hot path is served entirely from caches; the slow
// path resolves cluster endpoint state from the registry via rt.GetConfig and parses it once.
type router struct {
	rt           *runtime.Runtime
	matcherCache *lru.Cache // cluster name -> *clusterMatcher
	routeCache   *lru.Cache // listener -> *routeCacheEntry
	peerCache    *lru.Cache // listener|proto|peer -> *peerCacheValue
	epoch        atomic.Uint64
	log          logging.LeveledLogger
}

// clusterMatcher is the parsed, ready-to-match endpoint snapshot of one cluster, built from its
// config and cached until the next epoch.
type clusterMatcher struct {
	epoch     uint64
	typ       stnrv1.ClusterType
	proto     stnrv1.ClusterProtocol
	endpoints []*util.Endpoint
	domains   []string
}

type routeCacheEntry struct {
	epoch    uint64
	routeKey string
	// clusters lists, per protocol, the names of the listener's routed clusters of that protocol,
	// in route order.
	clusters map[stnrv1.ClusterProtocol][]string
}

type peerCacheValue struct {
	epoch   uint64
	proto   stnrv1.ClusterProtocol
	cluster string
}

// RouteAny resolves the cluster serving a peer across all cluster protocols (UDP preferred). Used
// for IP-level permission decisions where the flow protocol is not yet known: protocol and port
// specificity are enforced at flow establishment.
func RouteAny(rt *runtime.Runtime, listener string, routes []string, peer net.IP) (string, bool) {
	for _, proto := range []stnrv1.ClusterProtocol{stnrv1.ClusterProtocolUDP, stnrv1.ClusterProtocolTCP} {
		if cluster, ok := rt.Router.Route(listener, routes, proto, peer, 0); ok {
			return cluster, true
		}
	}
	return "", false
}

// NewRouter returns the default Router.
func NewRouter(rt *runtime.Runtime) runtime.Router {
	return &router{
		rt:           rt,
		matcherCache: lru.New(matcherCacheSize),
		routeCache:   lru.New(listenerRouteCacheSize),
		peerCache:    lru.New(peerRouteCacheSize),
		log:          rt.Logger.NewLogger("router"),
	}
}

func (r *router) InvalidateCache() {
	r.epoch.Add(1)
	r.matcherCache.Clear()
	r.routeCache.Clear()
	r.peerCache.Clear()
}

func (r *router) Match(cluster string, peer net.IP, port int) bool {
	return r.match(r.getMatcher(cluster), peer, port)
}

func (r *router) Route(listener string, routes []string, proto stnrv1.ClusterProtocol, peer net.IP, port int) (string, bool) {
	entry := r.getRouteEntry(listener, routes)

	if cluster, ok := r.getPeerCluster(listener, proto, peer, entry); ok {
		if r.Match(cluster, peer, port) {
			return cluster, true
		}
		return "", false
	}

	for _, cluster := range entry.clusters[proto] {
		if r.Match(cluster, peer, 0) {
			r.setPeer(listener, proto, peer, cluster, entry.epoch)
			return cluster, r.Match(cluster, peer, port)
		}
	}

	return "", false
}

// match tests a parsed matcher against a peer endpoint.
func (r *router) match(m *clusterMatcher, peer net.IP, port int) bool {
	switch m.typ {
	case stnrv1.ClusterTypeStatic:
		for _, e := range m.endpoints {
			if e.Match(peer, port) {
				return true
			}
		}
	case stnrv1.ClusterTypeStrictDNS:
		if r.rt.Resolver == nil {
			return false
		}
		for _, d := range m.domains {
			hosts, err := r.rt.Resolver.Lookup(d)
			if err != nil {
				continue
			}
			for _, h := range hosts {
				if h.Equal(peer) {
					return true
				}
			}
		}
	}
	return false
}

// getMatcher returns the parsed matcher for a cluster, building it from the cluster's config on a
// cache miss. A missing cluster yields an empty matcher that admits nothing.
func (r *router) getMatcher(cluster string) *clusterMatcher {
	curEpoch := r.epoch.Load()
	if v, ok := r.matcherCache.Get(cluster); ok {
		if m := v.(*clusterMatcher); m.epoch == curEpoch {
			return m
		}
	}

	m := &clusterMatcher{epoch: curEpoch}
	if conf, ok := r.rt.GetConfig(runtime.TypeCluster, cluster).(*stnrv1.ClusterConfig); ok && conf != nil {
		m.typ, _ = stnrv1.NewClusterType(conf.Type)
		m.proto, _ = stnrv1.NewClusterProtocol(conf.Protocol)
		switch m.typ {
		case stnrv1.ClusterTypeStatic:
			for _, e := range conf.Endpoints {
				ep, err := util.ParseEndpoint(e)
				if err != nil {
					continue
				}
				m.endpoints = append(m.endpoints, ep)
			}
		case stnrv1.ClusterTypeStrictDNS:
			m.domains = append([]string(nil), conf.Endpoints...)
		}
	}

	r.matcherCache.Add(cluster, m)
	return m
}

func (r *router) getRouteEntry(listener string, routes []string) *routeCacheEntry {
	curEpoch := r.epoch.Load()
	routeKey := strings.Join(routes, "\x00")

	if value, ok := r.routeCache.Get(listener); ok {
		entry := value.(*routeCacheEntry)
		if entry.epoch == curEpoch && entry.routeKey == routeKey {
			return entry
		}
	}

	entry := &routeCacheEntry{
		epoch:    curEpoch,
		routeKey: routeKey,
		clusters: map[stnrv1.ClusterProtocol][]string{
			stnrv1.ClusterProtocolUDP: {},
			stnrv1.ClusterProtocolTCP: {},
		},
	}
	for _, name := range routes {
		m := r.getMatcher(name)
		switch m.proto {
		case stnrv1.ClusterProtocolUDP, stnrv1.ClusterProtocolTCP:
			entry.clusters[m.proto] = append(entry.clusters[m.proto], name)
		}
	}

	r.routeCache.Add(listener, entry)
	return entry
}

func (r *router) getPeerCluster(listener string, proto stnrv1.ClusterProtocol, peer net.IP, entry *routeCacheEntry) (string, bool) {
	key := peerCacheKey(listener, proto, peer)
	v, ok := r.peerCache.Get(key)
	if !ok {
		return "", false
	}

	p := v.(*peerCacheValue)
	if p.epoch != entry.epoch || p.proto != proto {
		r.peerCache.Remove(key)
		return "", false
	}

	return p.cluster, true
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
