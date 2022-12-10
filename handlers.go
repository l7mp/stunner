package stunner

import (
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec,gci
	"encoding/base64"
	"errors"
	"net"
	"strconv"
	"time"

	"strings"

	"github.com/pion/turn/v2"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/util"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// time-windowed TURN auth username separator defined in
// https://datatracker.ietf.org/doc/html/draft-uberti-behave-turn-rest-00
const usernameSeparator = ":"

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

			key := turn.GenerateAuthKey(auth.Username, auth.Realm, auth.Password)
			if username == auth.Username {
				return key, true
			}

			return nil, false

		case v1alpha1.AuthTypeLongTerm:
			auth.Log.Infof("longterm auth request: username=%q realm=%q srcAddr=%v",
				username, realm, srcAddr)

			// find the first thing that looks like a UNIX timestamp in the username
			// and use that for checking the time-windowed credential, drop everything
			// else
			var timestamp int = 0
			for _, ts := range strings.Split(username, usernameSeparator) {
				t, err := strconv.Atoi(ts)
				if err == nil {
					timestamp = t
					break
				}
			}

			if timestamp == 0 {
				auth.Log.Errorf("invalid time-windowed username %q", username)
				return nil, false
			}

			if int64(timestamp) < time.Now().Unix() {
				auth.Log.Errorf("expired time-windowed username %q", username)
				return nil, false
			}

			mac := hmac.New(sha1.New, []byte(auth.Secret))
			_, err := mac.Write([]byte(username))
			if err != nil {
				auth.Log.Errorf("failed to hash username: %w", err.Error())
				return nil, false
			}
			password := base64.StdEncoding.EncodeToString(mac.Sum(nil))

			return turn.GenerateAuthKey(username, auth.Realm, password), true

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
		auth.Log.Debugf("permission denied on listener %q for client %q to peer %s: no route to endpoint",
			l.Name, src.String(), peerIP)
		return false
	}
}

// NewHandlerFactory creates helper functions that allow a listener to generate STUN/TURN
// authentication and permission handlers.
func (s *Stunner) NewHandlerFactory() object.HandlerFactory {
	return object.HandlerFactory{
		GetRealm: func() string {
			auth := s.GetAuth()
			if auth == nil {
				return ""
			}
			return auth.Realm
		},
		GetAuthHandler:       func() turn.AuthHandler { return s.NewAuthHandler() },
		GetPermissionHandler: func(l *object.Listener) turn.PermissionHandler { return s.NewPermissionHandler(l) },
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
