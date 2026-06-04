package object

import (
	"fmt"
	"net"

	"github.com/pion/turn/v5"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	a12n "github.com/l7mp/stunner/pkg/authentication"
)

// NewTURNAuthHandler returns an authentication handler callback for a TURN server. The returned
// closure queries the Auth object via the Registry on every request, so a reconcile that swaps
// credentials or auth mode takes effect immediately without needing the listener to bounce.
func NewTURNAuthHandler(l *Listener) a12n.AuthHandler {
	l.log.Trace("NewTURNAuthHandler")

	// Run without auth.
	if a := l.lookupAuth(); a != nil && a.Type == stnrv1.AuthTypeNone {
		return nil
	}

	return func(ra *turn.RequestAttributes) (string, []byte, bool) {
		username := ra.Username
		realm := ra.Realm
		srcAddr := ra.SrcAddr

		// Dynamic lookup: auth mode might have changed behind our back.
		auth := l.lookupAuth()
		if auth == nil {
			l.log.Errorf("no auth object registered at TURN-auth time")
			return "", nil, false
		}

		switch auth.Type {
		case stnrv1.AuthTypeStatic:
			auth.Log.Tracef("static auth request: username=%q realm=%q srcAddr=%v",
				username, realm, srcAddr)
			key := a12n.GenerateAuthKey(auth.Username, auth.Realm, auth.Password)
			if username == auth.Username {
				auth.Log.Debug("static auth request: valid username")
				return username, key, true
			}
			auth.Log.Infof("static auth request: failed: invalid username")
			return "", nil, false

		case stnrv1.AuthTypeEphemeral:
			auth.Log.Tracef("ephemeral auth request: username=%q realm=%q srcAddr=%v",
				username, realm, srcAddr)
			userID, err := a12n.CheckTimeWindowedUsername(username)
			if err != nil {
				auth.Log.Infof("ephemeral auth request: failed: %s", err)
				return "", nil, false
			}
			password, err := a12n.GetLongTermCredential(username, auth.Secret)
			if err != nil {
				auth.Log.Debugf("ephemeral auth request: error generating password: %s", err)
				return "", nil, false
			}
			auth.Log.Debug("ephemeral auth request: success")
			key := a12n.GenerateAuthKey(username, auth.Realm, password)
			return userID, key, true

		default:
			auth.Log.Errorf("internal error: unknown authentication mode %q", auth.Type.String())
			return "", nil, false
		}
	}
}

// NewTURNPermissionHandler returns a callback to handle client permission requests to access peers.
func NewTURNPermissionHandler(l *Listener) a12n.PermissionHandler {
	l.log.Trace("NewTURNPermissionHandler")

	return func(src net.Addr, peer net.IP) bool {
		peerIP := peer.String()
		l.log.Tracef("permission handler for listener %q: client %q, peer %q", l.Name,
			src.String(), peerIP)

		// Dynamic cluster lookup via Registry — picks up cluster reconfigs without a listener
		// restart, just like the auth lookup above.
		for _, c := range l.clustersForRoutes() {
			l.log.Tracef("considering cluster %q", c.Name)
			if c.Route(peer) {
				l.log.Debugf("permission granted on listener %q for client %q to peer %s via cluster %q",
					l.Name, src.String(), peerIP, c.Name)
				return true
			}
		}
		l.log.Infof("permission denied on listener %q for client %q to peer %s: no route to endpoint",
			l.Name, src.String(), peerIP)
		return false
	}
}

// TURNAllocationEventType is a helper type to administer allocations.
type TURNAllocationEventType int

const (
	TURNAllocationCreated TURNAllocationEventType = iota + 1
	TURNAllocationDeleted
)

// TURNQuotaHandler handles user quotas.
type TURNQuotaHandler interface {
	AllocationHandler(src, dst net.Addr, proto, username, realm string, event TURNAllocationEventType)
	QuotaHandler() turn.QuotaHandler
}

// NewTURNQuotaHandler creates a quota handler that defaults to a stub. Downstream non-OSS builds
// can swap turnQuotaHandlerConstructor to inject a real implementation.
func NewTURNQuotaHandler(l *Listener) TURNQuotaHandler {
	return turnQuotaHandlerConstructor(l)
}

var turnQuotaHandlerConstructor = newTURNQuotaHandlerStub

type turnQuotaHandlerStub struct {
	turnQuotaHandler turn.QuotaHandler
}

func (q *turnQuotaHandlerStub) QuotaHandler() turn.QuotaHandler { return q.turnQuotaHandler }
func (q *turnQuotaHandlerStub) AllocationHandler(_, _ net.Addr, _, _, _ string, _ TURNAllocationEventType) {
}

func newTURNQuotaHandlerStub(_ *Listener) TURNQuotaHandler {
	return &turnQuotaHandlerStub{
		turnQuotaHandler: func(_, _ string, _ net.Addr) (ok bool) { return true },
	}
}

// TURNOffloadHandler is the interface the per-listener TURN server uses to surface channel-create/
// channel-delete events into the offload engine. The Offload Object owns one instance and exposes
// it via Offload.Handler().
type TURNOffloadHandler interface {
	Start() error
	Close() error
	HandleChannelCreate(net.Addr, net.Addr, string, string, string, net.Addr, net.Addr, uint16, string, string)
	HandleChannelDelete(net.Addr, net.Addr, string, string, string, net.Addr, net.Addr, uint16)
	Stats(name string, marker stnrv1.StatType) stnrv1.OffloadDirStat
}

// offloadHandlerStub is the default TURNOffloadHandler used in OSS builds.
type offloadHandlerStub struct{}

func (o *offloadHandlerStub) Start() error { return nil }
func (o *offloadHandlerStub) Close() error { return nil }
func (o *offloadHandlerStub) HandleChannelCreate(_, _ net.Addr, _, _, _ string, _, _ net.Addr, _ uint16, _, _ string) {
}
func (o *offloadHandlerStub) HandleChannelDelete(_, _ net.Addr, _, _, _ string, _, _ net.Addr, _ uint16) {
}
func (o *offloadHandlerStub) Stats(_ string, _ stnrv1.StatType) stnrv1.OffloadDirStat {
	return stnrv1.OffloadDirStat{}
}

// NewTURNEventHandler creates a set of callbacks for tracking the lifecycle of TURN allocations.
// The cluster lookup on channel-create is dynamic (via Registry) so cluster reconfigs are picked
// up at runtime without requiring a listener restart.
func NewTURNEventHandler(l *Listener, q TURNQuotaHandler, o TURNOffloadHandler) turn.EventHandler {
	return turn.EventHandler{
		OnAuth: func(src, dst net.Addr, proto, username, realm string, method string, verdict bool) {
			status := "REJECTED"
			if verdict {
				status = "ACCEPTED"
			}
			l.log.Debugf("authentication request: client=%s, method=%s, verdict=%s",
				dumpClient(src, dst, proto, username, realm), method, status)
		},
		OnAllocationCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, reqPort int) {
			l.log.Debugf("allocation created: client=%s, relay-address=%s, requested-port=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), reqPort)
			q.AllocationHandler(src, dst, proto, username, realm, TURNAllocationCreated)
		},
		OnAllocationDeleted: func(src, dst net.Addr, proto, username, realm string) {
			l.log.Debugf("allocation deleted: client=%s", dumpClient(src, dst, proto, username, realm))
			q.AllocationHandler(src, dst, proto, username, realm, TURNAllocationDeleted)
		},
		OnAllocationError: func(src, dst net.Addr, proto, message string) {
			l.log.Debugf("allocation error: client=%s-%s:%s, error=%s", src, dst, proto, message)
		},
		OnPermissionCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, peer net.IP) {
			l.log.Debugf("permission created: client=%s, relay-addr=%s, peer=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String())
		},
		OnPermissionDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, peer net.IP) {
			l.log.Debugf("permission deleted: client=%s, relay-addr=%s, peer=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String())
		},
		OnChannelCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			listener := l.Name
			cluster := ""
			peerAddr, ok := peer.(*net.UDPAddr)
			if !ok {
				return
			}
			for _, c := range l.clustersForRoutes() {
				if c.Route(peerAddr.IP) {
					cluster = c.Name
					break
				}
			}
			l.log.Debugf("channel created: listener=%s, cluster=%s, client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				listener, cluster, dumpClient(src, dst, proto, username, realm),
				relayAddr.String(), peer.String(), chanNum)
			o.HandleChannelCreate(src, dst, proto, username, realm, relayAddr, peer, chanNum, listener, cluster)
		},
		OnChannelDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			l.log.Debugf("channel deleted: client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(),
				peer.String(), chanNum)
			o.HandleChannelDelete(src, dst, proto, username, realm, relayAddr, peer, chanNum)
		},
	}
}

func dumpClient(srcAddr, dstAddr net.Addr, protocol, username, realm string) string {
	return fmt.Sprintf("%s-%s:%s, username=%s, realm=%s", srcAddr.String(), dstAddr.String(),
		protocol, username, realm)
}
