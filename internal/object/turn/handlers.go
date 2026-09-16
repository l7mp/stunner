package turn

import (
	"fmt"
	"net"

	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/offload"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
	a12n "github.com/l7mp/stunner/v2/pkg/authentication"
)

// NewAuthHandler returns an authentication handler callback for a TURN server.
func NewAuthHandler(rt *objruntime.Runtime, log logging.LeveledLogger) a12n.AuthHandler {
	log.Trace("NewAuthHandler")

	// We must return a nil auth-handler to switch pure STUN on.
	a, ok := rt.GetConfig(objruntime.TypeAuth, "").(*stnrv1.AuthConfig)
	if !ok || a == nil {
		log.Warn("auth handler: no auth config in runtime")
		return nil
	}
	typeVal, err := stnrv1.NewAuthType(a.Type)
	if err != nil {
		log.Errorf("auth handler: invalid auth type %q", a.Type)
		return nil
	}
	if typeVal == stnrv1.AuthTypeNone {
		return nil
	}

	return func(ra *turn.RequestAttributes) (string, []byte, bool) {
		username := ra.Username
		realm := ra.Realm
		srcAddr := ra.SrcAddr

		auth, ok := rt.GetConfig(objruntime.TypeAuth, "").(*stnrv1.AuthConfig)
		if !ok || auth == nil {
			log.Infof("auth request: failed: auth config is unavailable")
			return "", nil, false
		}
		authType, err := stnrv1.NewAuthType(auth.Type)
		if err != nil {
			log.Errorf("auth request: invalid auth type %q", auth.Type)
			return "", nil, false
		}

		switch authType {
		case stnrv1.AuthTypeStatic:
			configuredUser := auth.Credentials["username"]
			configuredPass := auth.Credentials["password"]
			log.Tracef("static auth request: username=%q realm=%q srcAddr=%v", username, realm, srcAddr)
			key := a12n.GenerateAuthKey(configuredUser, auth.Realm, configuredPass)
			if username == configuredUser {
				log.Debug("static auth request: valid username")
				return username, key, true
			}
			log.Infof("static auth request: failed: invalid username")
			return "", nil, false

		case stnrv1.AuthTypeEphemeral:
			secret := auth.Credentials["secret"]
			log.Tracef("ephemeral auth request: username=%q realm=%q srcAddr=%v", username, realm, srcAddr)
			userID, err := a12n.CheckTimeWindowedUsername(username)
			if err != nil {
				log.Infof("ephemeral auth request: failed: %s", err)
				return "", nil, false
			}
			password, err := a12n.GetLongTermCredential(username, secret)
			if err != nil {
				log.Debugf("ephemeral auth request: error generating password: %s", err)
				return "", nil, false
			}
			log.Debug("ephemeral auth request: success")
			key := a12n.GenerateAuthKey(username, auth.Realm, password)
			return userID, key, true

		default:
			log.Errorf("internal error: unknown authentication mode %q", authType.String())
			return "", nil, false
		}
	}
}

// NewPermissionHandler returns a callback to handle client permission requests to access peers.
func NewPermissionHandler(name string, rt *objruntime.Runtime, log logging.LeveledLogger) a12n.PermissionHandler {
	log.Trace("NewPermissionHandler")

	return func(src net.Addr, peer net.IP) bool {
		peerIP := peer.String()
		log.Tracef("permission handler for listener %q: client %q, peer %q", name,
			src.String(), peerIP)

		// Grant if *any* routed cluster admits the peer. Each cluster admits on its own
		// terms: a direct cluster admits the peers its endpoints name, a TURN-* cluster
		// admits every peer, since admission is then the upstream TURN server's job.
		if c, ok := rt.Router.Route(name, func(c objruntime.Cluster) bool {
			return c.Admits(peer, 0)
		}); ok {
			log.Debugf("permission granted on listener %q for client %q to peer %s via cluster %q",
				name, src.String(), peerIP, c.Name())
			return true
		}

		log.Infof("permission denied on listener %q for client %q to peer %s: no route to endpoint",
			name, src.String(), peerIP)
		return false
	}
}

// AllocationEventType is a helper type to administer allocations.
type AllocationEventType int

const (
	AllocationCreated AllocationEventType = iota + 1
	AllocationDeleted
)

// Quota is the per-user session quota machinery shared by the packet engines: QuotaHandler
// gates new sessions (TURN allocations, L4 flows) and AllocationHandler administers the
// counters from the lifecycle events.
type Quota struct {
	runtime *objruntime.Runtime
}

// NewQuotaHandler creates a quota handler for a listener context.
func NewQuotaHandler(rt *objruntime.Runtime) *Quota {
	return &Quota{runtime: rt}
}

// QuotaHandler returns a callback that enforces per-user session quotas. The check and the
// quota reservation are one atomic step (CheckAndIncrement), so concurrent sessions cannot
// race past the cap; AllocationHandler releases the reservation on session teardown.
func (q *Quota) QuotaHandler() turn.QuotaHandler {
	return func(username, realm string, _ net.Addr) bool {
		quota := 0
		if admin, ok := q.runtime.GetConfig(objruntime.TypeAdmin, "").(*stnrv1.AdminConfig); ok && admin != nil {
			quota = admin.UserQuota
		}
		return q.runtime.QuotaHandler.CheckAndIncrement(username, realm, quota)
	}
}

// AllocationHandler updates quota accounting on session lifecycle events.
func (q *Quota) AllocationHandler(_ net.Addr, _ net.Addr, _ string, username, realm string, event AllocationEventType) {
	if event == AllocationDeleted {
		q.runtime.QuotaHandler.Decrement(username, realm)
	}
}

// NewEventHandler creates a set of callbacks for tracking the lifecycle of TURN allocations on a
// listener serving listenerProto.
func NewEventHandler(name string, listenerProto stnrv1.ListenerProtocol, rt *objruntime.Runtime, log logging.LeveledLogger, q *Quota) turn.EventHandler {
	// routeChannel resolves the cluster serving a channel to peerIP and its protocol.
	routeChannel := func(peerIP net.IP) (string, stnrv1.ClusterProtocol) {
		if cl, ok := rt.Router.RoutePeer(name, stnrv1.ClusterProtocolUDP, peerIP, 0); ok {
			return cl, stnrv1.ClusterProtocolUDP
		}
		if c, ok := rt.Router.Route(name, isTURNCluster); ok {
			return c.Name(), c.Protocol()
		}
		return "", stnrv1.ClusterProtocolUnknown
	}

	return turn.EventHandler{
		OnAuth: func(src, dst net.Addr, proto, username, realm string, method string, verdict bool) {
			status := "REJECTED"
			if verdict {
				status = "ACCEPTED"
			}
			log.Debugf("authentication request: client=%s, method=%s, verdict=%s",
				dumpClient(src, dst, proto, username, realm), method, status)
		},
		OnAllocationCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, reqPort int) {
			log.Debugf("allocation created: client=%s, relay-address=%s, requested-port=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), reqPort)
			q.AllocationHandler(src, dst, proto, username, realm, AllocationCreated)
		},
		OnAllocationDeleted: func(src, dst net.Addr, proto, username, realm string) {
			log.Debugf("allocation deleted: client=%s", dumpClient(src, dst, proto, username, realm))
			q.AllocationHandler(src, dst, proto, username, realm, AllocationDeleted)
		},
		OnAllocationError: func(src, dst net.Addr, proto, message string) {
			log.Debugf("allocation error: client=%s-%s:%s, error=%s", src, dst, proto, message)
		},
		OnPermissionCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, peer net.IP) {
			cluster := ""
			if c, ok := rt.Router.Route(name, func(c objruntime.Cluster) bool {
				return c.Admits(peer, 0)
			}); ok {
				cluster = c.Name()
			}
			log.Debugf("permission created: client=%s, relay-addr=%s, peer=%s, cluster=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String(), cluster)
		},
		OnPermissionDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, peer net.IP) {
			log.Debugf("permission deleted: client=%s, relay-addr=%s, peer=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String())
		},
		OnChannelCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			peerAddr, ok := peer.(*net.UDPAddr)
			if !ok {
				return
			}
			cluster, clusterProto := routeChannel(peerAddr.IP)
			log.Debugf("channel created: listener=%s, cluster=%s, client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				name, cluster, dumpClient(src, dst, proto, username, realm),
				relayAddr.String(), peer.String(), chanNum)
			if !offload.Offloadable(listenerProto, clusterProto) {
				log.Debugf("skipping offload for channel to peer %s: %s listener %s, %s cluster",
					peer.String(), listenerProto.String(), name, clusterProto.String())
				return
			}
			client := offload.Connection{RemoteAddr: src, LocalAddr: dst, Protocol: proto, ChannelID: uint32(chanNum)}
			peerConn := offload.Connection{RemoteAddr: peer, LocalAddr: relayAddr, Protocol: proto}
			if err := rt.OffloadEngine.Upsert(client, peerConn, name, cluster); err != nil {
				log.Errorf("could not create offload %s(listener:%s)->%s(cluster:%s): %s",
					client.String(), name, peerConn.String(), cluster, err.Error())
			}
		},
		OnChannelDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			log.Debugf("channel deleted: client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(),
				peer.String(), chanNum)
			peerAddr, ok := peer.(*net.UDPAddr)
			if !ok {
				return
			}
			if _, clusterProto := routeChannel(peerAddr.IP); !offload.Offloadable(listenerProto, clusterProto) {
				return
			}
			client := offload.Connection{RemoteAddr: src, LocalAddr: dst, Protocol: proto, ChannelID: uint32(chanNum)}
			peerConn := offload.Connection{RemoteAddr: peer, LocalAddr: relayAddr, Protocol: proto}
			if err := rt.OffloadEngine.Remove(client, peerConn); err != nil {
				log.Errorf("could not remove offload %s->%s: %s", client.String(), peerConn.String(), err.Error())
			}
		},
	}
}

// dumpClient renders a compact client identity string for logs.
func dumpClient(srcAddr, dstAddr net.Addr, protocol, username, realm string) string {
	return fmt.Sprintf("%s-%s:%s, username=%s, realm=%s", srcAddr.String(), dstAddr.String(),
		protocol, username, realm)
}
