package stunner

import (
	"errors"
	"net"

	"github.com/pion/turn/v2"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/util"

	stnrauth "github.com/l7mp/stunner/pkg/apis/auth"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// NewAuthHandler returns an authentication handler callback to be used with a TURN server for
// authenticating clients.
func (s *Stunner) NewAuthHandler() turn.AuthHandler {
	s.log.Trace("NewAuthHandler")

	return func(username string, realm string, srcAddr net.Addr) ([]byte, bool) {
		// dynamic: auth mode might have changed behind ur back
		auth := s.GetAuth()

		switch auth.Type {
		case v1alpha1.AuthTypePlainText:
			auth.Log.Infof("plaintext auth request: username=%q realm=%q srcAddr=%v\n",
				username, realm, srcAddr)

			key := stnrauth.GenerateAuthKey(auth.Username, auth.Realm, auth.Password)
			if username == auth.Username {
				auth.Log.Debug("plaintext auth request: valid username found")
				return key, true
			}

			auth.Log.Info("plaintext auth request: failed: invalid username")
			return nil, false

		case v1alpha1.AuthTypeLongTerm:
			auth.Log.Infof("longterm auth request: username=%q realm=%q srcAddr=%v",
				username, realm, srcAddr)

			if err := stnrauth.CheckTimeWindowedUsername(username); err != nil {
				auth.Log.Infof("longterm auth request: failed: %s", err)
				return nil, false
			}

			password, err := stnrauth.GetLongTermCredential(username, auth.Secret)
			if err != nil {
				auth.Log.Infof("longterm auth request: error generating password: %s",
					err)
				return nil, false
			}

			auth.Log.Info("longterm auth request: success")
			return stnrauth.GenerateAuthKey(username, auth.Realm, password), true

		default:
			auth.Log.Errorf("internal error: unknown authentication mode %q",
				auth.Type.String())
			return nil, false
		}
	}
}

// NewPermissionHandler returns a callback to handle client permission requests to access peers.
func (s *Stunner) NewPermissionHandler(l *object.Listener) turn.PermissionHandler {
	s.log.Trace("NewPermissionHandler")

	return func(src net.Addr, peer net.IP) bool {
		// need auth for logging
		// dynamic: authHandler might have changed behind ur back
		auth := s.GetAuth()

		peerIP := peer.String()
		auth.Log.Debugf("permission handler for listener %q: client %q, peer %q",
			l.Name, src.String(), peerIP)
		clusters := s.clusterManager.Keys()

		for _, r := range l.Routes {
			auth.Log.Tracef("considering route to cluster %q", r)
			if util.Member(clusters, r) {
				auth.Log.Tracef("considering cluster %q", r)
				c := s.GetCluster(r)
				if c.Route(peer) {
					auth.Log.Infof("permission granted on listener %q for client "+
						"%q to peer %s via cluster %q", l.Name, src.String(),
						peerIP, c.Name)
					return true
				}
			}
		}
		auth.Log.Debugf("permission denied on listener %q for client %q to peer %s: "+
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
