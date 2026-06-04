package stunner

import (
	"net"

	"github.com/l7mp/stunner/internal/object"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	a12n "github.com/l7mp/stunner/pkg/authentication"
	"github.com/pion/turn/v5"
)

// Temporary legacy helpers for root-level reconcile tests.
//
// Keep these test-only while we gradually migrate TURN-specific assertions into
// internal/object tests.
func (s *Stunner) NewAuthHandler() a12n.AuthHandler {
	if a := s.GetAuth(); a != nil && a.Type == stnrv1.AuthTypeNone {
		return nil
	}

	return func(ra *turn.RequestAttributes) (string, []byte, bool) {
		auth := s.GetAuth()
		if auth == nil {
			return "", nil, false
		}

		switch auth.Type {
		case stnrv1.AuthTypeStatic:
			key := a12n.GenerateAuthKey(auth.Username, auth.Realm, auth.Password)
			if ra.Username == auth.Username {
				return ra.Username, key, true
			}
			return "", nil, false

		case stnrv1.AuthTypeEphemeral:
			userID, err := a12n.CheckTimeWindowedUsername(ra.Username)
			if err != nil {
				return "", nil, false
			}
			password, err := a12n.GetLongTermCredential(ra.Username, auth.Secret)
			if err != nil {
				return "", nil, false
			}
			key := a12n.GenerateAuthKey(ra.Username, auth.Realm, password)
			return userID, key, true

		default:
			return "", nil, false
		}
	}
}

func (s *Stunner) NewPermissionHandler(l *object.Listener) a12n.PermissionHandler {
	if l == nil {
		return func(net.Addr, net.IP) bool { return false }
	}
	return object.NewTURNPermissionHandler(l)
}
