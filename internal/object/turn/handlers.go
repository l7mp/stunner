package turn

import (
	"fmt"
	"net"

	"github.com/pion/logging"
	"github.com/pion/turn/v5"

	objruntime "github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	a12n "github.com/l7mp/stunner/pkg/authentication"
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
		conf, ok := rt.GetConfig(objruntime.TypeListener, name).(*stnrv1.ListenerConfig)
		if !ok || conf == nil {
			log.Infof("permission denied on listener %q for client %q to peer %s: listener config unavailable",
				name, src.String(), peerIP)
			return false
		}

		relay, ok := rt.Router.Route(name, conf.Routes, peer, 0)
		if ok {
			log.Debugf("permission granted on listener %q for client %q to peer %s via cluster %q",
				name, src.String(), peerIP, relay.ClusterName())
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

type quotaHandler struct {
	runtime *objruntime.Runtime
}

// NewQuotaHandler creates a quota handler for a listener context.
func NewQuotaHandler(rt *objruntime.Runtime) *quotaHandler {
	return &quotaHandler{runtime: rt}
}

// QuotaHandler returns a callback that enforces per-user allocation quotas.
func (q *quotaHandler) QuotaHandler() turn.QuotaHandler {
	return func(username, realm string, _ net.Addr) bool {
		admin := q.runtime.GetConfig(objruntime.TypeAdmin, "").(*stnrv1.AdminConfig)
		return q.runtime.QuotaStore.CheckAndIncrement(username, realm, admin.UserQuota)
	}
}

// AllocationHandler updates quota accounting on allocation lifecycle events.
func (q *quotaHandler) AllocationHandler(_ net.Addr, _ net.Addr, _ string, username, realm string, event AllocationEventType) {
	if event == AllocationDeleted {
		q.runtime.QuotaStore.Decrement(username, realm)
	}
}

// NewEventHandler creates a set of callbacks for tracking the lifecycle of TURN allocations.
func NewEventHandler(name string, rt *objruntime.Runtime, log logging.LeveledLogger, q *quotaHandler, o OffloadHandler) turn.EventHandler {
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
			if conf, ok := rt.GetConfig(objruntime.TypeListener, name).(*stnrv1.ListenerConfig); ok && conf != nil {
				if relay, ok := rt.Router.Route(name, conf.Routes, peer, 0); ok {
					cluster = relay.ClusterName()
				}
			}
			log.Debugf("permission created: client=%s, relay-addr=%s, peer=%s, cluster=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String(), cluster)
		},
		OnPermissionDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, peer net.IP) {
			log.Debugf("permission deleted: client=%s, relay-addr=%s, peer=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String())
		},
		OnChannelCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			cluster := ""
			peerAddr, ok := peer.(*net.UDPAddr)
			if !ok {
				return
			}
			if conf, ok := rt.GetConfig(objruntime.TypeListener, name).(*stnrv1.ListenerConfig); ok && conf != nil {
				if relay, ok := rt.Router.Route(name, conf.Routes, peerAddr.IP, 0); ok {
					cluster = relay.ClusterName()
				}
			}
			log.Debugf("channel created: listener=%s, cluster=%s, client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				name, cluster, dumpClient(src, dst, proto, username, realm),
				relayAddr.String(), peer.String(), chanNum)
			o.HandleChannelCreate(src, dst, proto, username, realm, relayAddr, peer, chanNum, name, cluster)
		},
		OnChannelDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			log.Debugf("channel deleted: client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(),
				peer.String(), chanNum)
			o.HandleChannelDelete(src, dst, proto, username, realm, relayAddr, peer, chanNum)
		},
	}
}

// dumpClient renders a compact client identity string for logs.
func dumpClient(srcAddr, dstAddr net.Addr, protocol, username, realm string) string {
	return fmt.Sprintf("%s-%s:%s, username=%s, realm=%s", srcAddr.String(), dstAddr.String(),
		protocol, username, realm)
}
