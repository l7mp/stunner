package turn

import (
	"fmt"
	"net"
	"net/netip"

	"github.com/pion/turn/v5"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/offload"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	a12n "github.com/l7mp/stunner/v2/pkg/authentication"
)

// AuthHandler returns the authentication handler callback of the server, which authenticates by
// the policy the server names, looked up on every request. A policy of type none makes a pure STUN
// server (a nil handler); a missing one refuses every client.
func (s *Server) AuthHandler() a12n.AuthHandler {
	log := s.log
	policy := func() *stnrv2.AuthConfig {
		s.mu.RLock()
		name := s.auth
		s.mu.RUnlock()
		return s.rt.AuthConfig(name)
	}
	if a := policy(); a == nil {
		log.Warnf("auth handler: server %s names no authentication policy: every client is refused",
			s.Name())
	} else if typeVal, err := stnrv2.NewAuthType(a.Type); err == nil && typeVal == stnrv2.AuthTypeNone {
		// a nil handler switches pure STUN on
		return nil
	} else if (typeVal == stnrv2.AuthTypeStatic && (a.Credentials["username"] == "" || a.Credentials["password"] == "")) ||
		(typeVal == stnrv2.AuthTypeEphemeral && a.Credentials["secret"] == "") {
		log.Warnf("auth handler: %s auth without credentials: every client is refused",
			typeVal.String())
	}

	return func(ra *turn.RequestAttributes) (string, []byte, bool) {
		username := ra.Username
		realm := ra.Realm
		srcAddr := ra.SrcAddr

		auth := policy()
		if auth == nil {
			log.Infof("auth request: failed: no authentication policy")
			return "", nil, false
		}
		authType, err := stnrv2.NewAuthType(auth.Type)
		if err != nil {
			log.Errorf("auth request: invalid auth type %q", auth.Type)
			return "", nil, false
		}

		switch authType {
		case stnrv2.AuthTypeStatic:
			configuredUser := auth.Credentials["username"]
			configuredPass := auth.Credentials["password"]
			log.Tracef("static auth request: username=%q realm=%q srcAddr=%v", username, realm, srcAddr)
			if configuredUser == "" || configuredPass == "" {
				log.Infof("static auth request: failed: no credentials configured")
				return "", nil, false
			}
			key := a12n.GenerateAuthKey(configuredUser, auth.Realm, configuredPass)
			if username == configuredUser {
				log.Debug("static auth request: valid username")
				return username, key, true
			}
			log.Infof("static auth request: failed: invalid username")
			return "", nil, false

		case stnrv2.AuthTypeEphemeral:
			secret := auth.Credentials["secret"]
			log.Tracef("ephemeral auth request: username=%q realm=%q srcAddr=%v", username, realm, srcAddr)
			if secret == "" {
				log.Infof("ephemeral auth request: failed: no secret configured")
				return "", nil, false
			}
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

// AllocationEventType is a helper type to administer allocations.
type AllocationEventType int

const (
	AllocationCreated AllocationEventType = iota + 1
	AllocationDeleted
)

// Quota is the per-user session quota shared by the TURN and L4 servers.
type Quota struct {
	runtime *objruntime.Runtime
}

// NewQuotaHandler creates a quota handler.
func NewQuotaHandler(rt *objruntime.Runtime) *Quota {
	return &Quota{runtime: rt}
}

// QuotaHandler returns a callback that checks and reserves a session's quota in one step.
func (q *Quota) QuotaHandler() turn.QuotaHandler {
	return func(username, realm string, _ net.Addr) bool {
		quota := 0
		if admin, ok := q.runtime.GetConfig(objruntime.TypeAdmin, "").(*stnrv2.AdminConfig); ok && admin != nil {
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

// permissionHandler grants a permission when the permission filters admit the peer IP and a
// cluster routes it.
func (s *Server) permissionHandler(src net.Addr, peer net.IP) bool {
	ip, ok := netip.AddrFromSlice(peer)
	if !ok {
		return false
	}
	dst := netip.AddrPortFrom(ip.Unmap(), 0)
	s.mu.RLock()
	filters := s.permissionFilters
	s.mu.RUnlock()
	if !s.rt.Admits(filters, net.UDPAddrFromAddrPort(dst)) {
		s.log.Infof("permission denied on server %q for client %q to peer %s: filtered",
			s.Name(), src.String(), peer.String())
		return false
	}
	for _, name := range s.Clusters() {
		r, found := s.rt.Router(name)
		if !found {
			continue
		}
		_, ok, err := r.Route(dst)
		if err != nil {
			s.log.Warnf("server %q: skipping cluster %q: %s", s.Name(), name, err.Error())
			continue
		}
		if ok {
			s.log.Debugf("permission granted on server %q for client %q to peer %s via cluster %q",
				s.Name(), src.String(), peer.String(), name)
			return true
		}
	}
	s.log.Infof("permission denied on server %q for client %q to peer %s: no cluster routes it",
		s.Name(), src.String(), peer.String())
	return false
}

// offloadPair is an offloaded channel as registered with the offload engine.
type offloadPair struct {
	client, peer offload.Connection
}

// eventHandler returns the allocation lifecycle callbacks.
func (s *Server) eventHandler(ct *server.Conntrack) turn.EventHandler {
	return turn.EventHandler{
		OnAuth: func(src, dst net.Addr, proto, username, realm string, method string, verdict bool) {
			status := "REJECTED"
			if verdict {
				status = "ACCEPTED"
			}
			s.log.Debugf("authentication request: client=%s, method=%s, verdict=%s",
				dumpClient(src, dst, proto, username, realm), method, status)
		},
		OnAllocationCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, reqPort int) {
			s.log.Debugf("allocation created: client=%s, relay-address=%s, requested-port=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), reqPort)
			if e, ok := ct.Get(server.Key(src, dst)); ok {
				e.Disarm()
			}
			s.quota.AllocationHandler(src, dst, proto, username, realm, AllocationCreated)
		},
		OnAllocationDeleted: func(src, dst net.Addr, proto, username, realm string) {
			s.log.Debugf("allocation deleted: client=%s", dumpClient(src, dst, proto, username, realm))
			s.quota.AllocationHandler(src, dst, proto, username, realm, AllocationDeleted)
			ct.Remove(server.Key(src, dst))
		},
		OnAllocationError: func(src, dst net.Addr, proto, message string) {
			s.log.Debugf("allocation error: client=%s-%s:%s, error=%s", src, dst, proto, message)
		},
		OnPermissionCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, peer net.IP) {
			s.log.Debugf("permission created: client=%s, relay-addr=%s, peer=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String())
		},
		OnPermissionDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr net.Addr, peer net.IP) {
			s.log.Debugf("permission deleted: client=%s, relay-addr=%s, peer=%s",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(), peer.String())
		},
		OnChannelCreated: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			s.log.Debugf("channel created: server=%s, client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				s.Name(), dumpClient(src, dst, proto, username, realm), relayAddr.String(),
				peer.String(), chanNum)

			// Only a plain UDP client conn relaying through a local UDP socket is offloadable.
			// pion reports every client 5-tuple as UDP (DTLS, TCP and TLS included), so the
			// transport is read from the conn's tag.
			e, found := ct.Get(server.Key(src, dst))
			if !found || e.Client.Tag().Proto != stnrv2.ProtocolUDP {
				return
			}
			c := e.Client
			// the cluster that made the relay socket must be direct
			rc, found := s.relayDialer()
			if !found {
				return
			}
			conf := s.rt.ClusterConfig(rc.Name())
			if conf == nil || conf.Tunnel != nil {
				return
			}
			if _, isUDP := peer.(*net.UDPAddr); !isUDP {
				return
			}
			// the kernel path skips the cluster's filters: offload only a peer they admit
			if !s.rt.Admits(conf.Filters, peer) {
				return
			}
			pair := offloadPair{
				client: offload.Connection{RemoteAddr: src, LocalAddr: dst, Protocol: proto,
					ChannelID: uint32(chanNum)},
				peer: offload.Connection{RemoteAddr: peer, LocalAddr: relayAddr, Protocol: proto},
			}
			if err := s.rt.OffloadEngine.Upsert(pair.client, pair.peer, c.Tag().Name, rc.Name()); err != nil {
				s.log.Errorf("could not create offload %s(listener:%s)->%s(cluster:%s): %s",
					pair.client.String(), c.Tag().Name, pair.peer.String(), rc.Name(), err.Error())
				return
			}
			s.offloads.Store(pair.client.String(), pair)
		},
		OnChannelDeleted: func(src, dst net.Addr, proto, username, realm string, relayAddr, peer net.Addr, chanNum uint16) {
			s.log.Debugf("channel deleted: client=%s, relay-addr=%s, peer=%s, channel-num=%d",
				dumpClient(src, dst, proto, username, realm), relayAddr.String(),
				peer.String(), chanNum)
			// remove exactly what was registered: the client conn may be gone by now
			key := (&offload.Connection{RemoteAddr: src, LocalAddr: dst, Protocol: proto,
				ChannelID: uint32(chanNum)}).String()
			v, registered := s.offloads.LoadAndDelete(key)
			if !registered {
				return
			}
			pair := v.(offloadPair)
			if err := s.rt.OffloadEngine.Remove(pair.client, pair.peer); err != nil {
				s.log.Errorf("could not remove offload %s->%s: %s", pair.client.String(),
					pair.peer.String(), err.Error())
			}
		},
	}
}

// dumpClient renders a compact client identity string for logs.
func dumpClient(srcAddr, dstAddr net.Addr, protocol, username, realm string) string {
	return fmt.Sprintf("%s-%s:%s, username=%s, realm=%s", srcAddr.String(), dstAddr.String(),
		protocol, username, realm)
}

// relayDialer returns the dialer that makes the relay sockets of the server's allocations: the
// first one reaching UDP peers, the one AllocatePacketConn uses.
func (s *Server) relayDialer() (api.Dialer, bool) {
	for _, d := range s.dialers() {
		if d.Protocol() == stnrv2.ProtocolUDP {
			return d, true
		}
	}
	return nil, false
}
