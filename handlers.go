package stunner

import (
	"errors"
	"fmt"
	"net"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/util"
	"github.com/pion/turn/v4"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	a12n "github.com/l7mp/stunner/pkg/authentication"
)

// NewAuthHandler returns an authentication handler callback to be used with a TURN server for
// authenticating clients.
func (s *Stunner) NewAuthHandler() a12n.AuthHandler {
	s.log.Trace("NewAuthHandler")

	// Run witthout auth
	if s.GetAuth().Type == stnrv1.AuthTypeNone {
		return nil
	}

	return func(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
		// dynamic: auth mode might have changed behind ur back
		auth := s.GetAuth()

		switch auth.Type {
		case stnrv1.AuthTypeStatic:
			auth.Log.Tracef("static auth request: username=%q realm=%q srcAddr=%v\n",
				username, realm, srcAddr)

			key := a12n.GenerateAuthKey(auth.Username, auth.Realm, auth.Password)
			if username == auth.Username {
				auth.Log.Debug("static auth request: valid username")
				return key, true
			}

			auth.Log.Infof("static auth request: failed: invalid username")
			return nil, false

		case stnrv1.AuthTypeEphemeral:
			auth.Log.Tracef("ephemeral auth request: username=%q realm=%q srcAddr=%v",
				username, realm, srcAddr)

			if err := a12n.CheckTimeWindowedUsername(username); err != nil {
				auth.Log.Infof("ephemeral auth request: failed: %s", err)
				return nil, false
			}

			password, err := a12n.GetLongTermCredential(username, auth.Secret)
			if err != nil {
				auth.Log.Debugf("ephemeral auth request: error generating password: %s",
					err)
				return nil, false
			}

			auth.Log.Debug("ephemeral auth request: success")
			return a12n.GenerateAuthKey(username, auth.Realm, password), true

		default:
			auth.Log.Errorf("internal error: unknown authentication mode %q",
				auth.Type.String())
			return nil, false
		}
	}
}

// NewPermissionHandler returns a callback to handle client permission requests to access peers.
func (s *Stunner) NewPermissionHandler(l *object.Listener) a12n.PermissionHandler {
	s.log.Trace("NewPermissionHandler")

	return func(src net.Addr, peer net.IP) bool {
		// need auth for logging
		// dynamic: authHandler might have changed behind our back
		auth := s.GetAuth()

		peerIP := peer.String()
		auth.Log.Tracef("permission handler for listener %q: client %q, peer %q", l.Name,
			src.String(), peerIP)

		clusters := s.clusterManager.Keys()
		for _, r := range l.Routes {
			auth.Log.Tracef("considering route to cluster %q", r)
			if util.Member(clusters, r) {
				auth.Log.Tracef("considering cluster %q", r)
				c := s.GetCluster(r)
				if c.Route(peer) {
					auth.Log.Debugf("permission granted on listener %q for client "+
						"%q to peer %s via cluster %q", l.Name, src.String(),
						peerIP, c.Name)
					return true
				}
			}
		}
		auth.Log.Infof("permission denied on listener %q for client %q to peer %s: "+
			"no route to endpoint", l.Name, src.String(), peerIP)
		return false
	}
}

// NewReadinessHandler creates a helper function for checking the readiness of STUNner.
func (s *Stunner) NewReadinessHandler() object.ReadinessHandler {
	return func() error {
		if s.IsReady() {
			return nil
		} else {
			return errors.New("stunnerd not ready")
		}
	}
}

// NewRealmHandler creates a helper function for listeners to find out the authentication realm.
func (s *Stunner) NewRealmHandler() object.RealmHandler {
	return func() string {
		if s != nil {
			return s.GetRealm()
		}
		return ""
	}
}

// NewStatusHandler creates a helper function for printing the status of STUNner.
func (s *Stunner) NewStatusHandler() object.StatusHandler {
	return func() stnrv1.Status { return s.Status() }
}

// AllocationEventType is a helper type to administer allocations.
type AllocationEventType int

const (
	AllocationCreated AllocationEventType = iota + 1
	AllocationDeleted
)

// Quota handler handles user quotas.
type QuotaHandler interface {
	AllocationHandler(src, dst net.Addr, proto, username, realm string, event AllocationEventType)
	QuotaHandler() turn.QuotaHandler
}

// NewUserQuotaHandler creates a quota handler that defaults to a stub.
func (s *Stunner) NewQuotaHandler() QuotaHandler {
	return quotaHandlerConstructor(s)
}

var quotaHandlerConstructor = newQuotaHandlerStub

// quotaHandlerStub is a stub quota handler that does nothing.
type quotaHandlerStub struct {
	quotaHandler turn.QuotaHandler
}

func (q *quotaHandlerStub) QuotaHandler() turn.QuotaHandler {
	return q.quotaHandler
}

func (q *quotaHandlerStub) AllocationHandler(_, _ net.Addr, _, _, _ string, _ AllocationEventType) {}

func newQuotaHandlerStub(_ *Stunner) QuotaHandler {
	return &quotaHandlerStub{
		quotaHandler: func(_, _ string, _ net.Addr) (ok bool) {
			return true
		},
	}
}

// OffloadEventType is a helper type to administer offload events.
type OffloadEventType int

const (
	ChannnelOffloadCreated OffloadEventType = iota + 1
	ChannnelOffloadDeleted
)

// TURN offload handler.
type OffloadHandler interface {
	OffloadHandler(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16, event OffloadEventType)
}

// NewOffloadHandler creates a offload handler that defaults to a stub.
func (s *Stunner) NewOffloadHandler() OffloadHandler {
	return offloadHandlerConstructor(s)
}

var offloadHandlerConstructor = newOffloadHandlerStub

// offloadHandlerStub is a stub offload handler that does nothing.
type offloadHandlerStub struct{}

func (o *offloadHandlerStub) OffloadHandler(_, _ net.Addr, _, _, _ string, _, _ net.Addr, _ uint16, _ OffloadEventType) {
}

func newOffloadHandlerStub(_ *Stunner) OffloadHandler {
	return &offloadHandlerStub{}
}

// NewEventHandler creates a set of callbcks for tracking the lifecycle of TURN allocations.
func (s *Stunner) NewEventHandler() turn.EventHandlers {
	return turn.EventHandlers{
		OnAuth: func(src, dst net.Addr, proto, username, realm string, method string, verdict bool) {
			status := "REJECTED"
			if verdict {
				status = "ACCEPTED"
			}
			s.log.Infof("Authentication request: client=%s, method=%s, verdict=%s",
				dumpClient(src, dst, proto, username, realm), method, status)
		},
		OnAllocationCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, reqPort int) {
			s.log.Infof("Allocation created: client=%s, relay-address=%s, requested-port=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), reqPort)

			s.quotaHandler.AllocationHandler(src, dst, proto, username, realm, AllocationCreated)
		},
		OnAllocationDeleted: func(src, dst net.Addr, proto, username, realm string) {
			s.log.Infof("Allocation deleted: client=%s", dumpClient(src, dst, proto, username, realm))

			s.quotaHandler.AllocationHandler(src, dst, proto, username, realm, AllocationDeleted)
		},
		OnAllocationError: func(src, dst net.Addr, proto, message string) {
			s.log.Infof("Allocation error: client=%s-%s:%s, error=%s", src, dst, proto, message)
		},
		OnPermissionCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, peer net.IP) {
			s.log.Infof("Permission created: client=%s, relay-addr=%s, peer=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String())
		},
		OnPermissionDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, peer net.IP) {
			s.log.Infof("Permission deleted: client=%s, relay-addr=%s, peer=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String())
		},
		OnChannelCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			s.log.Infof("Channel created: client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(),
				peer.String(), chanNum)

			s.offloadHandler.OffloadHandler(src, dst, proto, username, realm, relayAddr, peer, chanNum, ChannnelOffloadCreated)
		},
		OnChannelDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			s.log.Infof("Channel deleted: client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(),
				peer.String(), chanNum)

			s.offloadHandler.OffloadHandler(src, dst, proto, username, realm, relayAddr, peer, chanNum, ChannnelOffloadDeleted)
		},
	}
}

func dumpClient(srcAddr, dstAddr net.Addr, protocol, username, realm string) string {
	return fmt.Sprintf("%s-%s:%s, username=%s, realm=%s", srcAddr.String(), dstAddr.String(),
		protocol, username, realm)
}
