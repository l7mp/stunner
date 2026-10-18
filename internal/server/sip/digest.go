package sip

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"strconv"
	"strings"
	"time"

	sipmsg "github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"

	"github.com/l7mp/stunner/v2/internal/siptx"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/authentication"
)

// nonceLifetime is how long a digest nonce of the proxy stays valid; an expired nonce with the
// right credentials is answered stale, so the client retries with a fresh one.
const nonceLifetime = 5 * time.Minute

// nonce makes a digest nonce: the issue time and its MAC, so the proxy keeps no nonce state.
func (s *Server) nonce() string {
	ts := strconv.FormatInt(time.Now().Unix(), 16)
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(ts))
	return ts + "." + hex.EncodeToString(mac.Sum(nil)[:12])
}

// authenticate tells whether a request a trunk starts passes the digest auth of the server's auth
// policy, challenging it if not. A server naming no auth policy, or one of type none, admits every
// request the listener filters let through; one naming a missing policy refuses every request.
func (s *Server) authenticate(tx *siptx.ServerTx, req *sipmsg.Request) bool {
	s.confMu.RLock()
	name := s.auth
	s.confMu.RUnlock()
	if name == "" {
		return true
	}
	auth := s.rt.AuthConfig(name)
	if auth == nil {
		s.log.Warnf("server %s: refusing %s: no auth policy %q", s.Name(), req.Method, name)
		_ = tx.Respond(sipmsg.NewResponseFromRequest(req, sipmsg.StatusForbidden, "Forbidden", nil))
		return false
	}
	if t, _ := stnrv2.NewAuthType(auth.Type); t == stnrv2.AuthTypeNone {
		return true
	}
	ok, stale := s.checkDigest(req, auth)
	if !ok {
		_ = tx.Respond(s.challenge(req, auth, stale))
	}
	return ok
}

// challenge answers a request with a digest challenge of the proxy.
func (s *Server) challenge(req *sipmsg.Request, auth *stnrv2.AuthConfig, stale bool) *sipmsg.Response {
	res := sipmsg.NewResponseFromRequest(req, sipmsg.StatusProxyAuthRequired,
		"Proxy Authentication Required", nil)
	chal := &digest.Challenge{Realm: auth.Realm, Nonce: s.nonce(), Algorithm: "MD5",
		QOP: []string{"auth"}, Stale: stale}
	res.AppendHeader(sipmsg.NewHeader("Proxy-Authenticate", chal.String()))
	return res
}

// checkDigest checks the proxy credentials of a request against an auth policy and consumes them:
// valid credentials are removed from the request. Credentials count only for the Request-URI they
// were computed for (RFC 2617 section 3.2.2.5). stale is true when the credentials were right but
// the nonce has expired.
func (s *Server) checkDigest(req *sipmsg.Request, auth *stnrv2.AuthConfig) (ok, stale bool) {
	t, _ := stnrv2.NewAuthType(auth.Type)
	var keep []sipmsg.Header
	for _, h := range req.GetHeaders("Proxy-Authorization") {
		cred, err := digest.ParseCredentials(h.Value())
		if err != nil || cred.Realm != auth.Realm {
			keep = append(keep, h)
			continue
		}
		var uri sipmsg.Uri
		if err := sipmsg.ParseUri(cred.URI, &uri); err != nil || !strings.EqualFold(uri.Scheme, req.Recipient.Scheme) ||
			uri.User != req.Recipient.User || !strings.EqualFold(uri.Host, req.Recipient.Host) || uri.Port != req.Recipient.Port {
			continue
		}
		password := ""
		switch t {
		case stnrv2.AuthTypeStatic:
			if cred.Username == auth.Credentials["username"] {
				password = auth.Credentials["password"]
			}
		case stnrv2.AuthTypeEphemeral:
			if _, err := authentication.CheckTimeWindowedUsername(cred.Username); err == nil {
				password, _ = stnrv2.GetLongTermCredential(cred.Username,
					auth.Credentials["secret"])
			}
		}
		if password == "" {
			continue
		}
		chal := &digest.Challenge{Realm: cred.Realm, Nonce: cred.Nonce, Algorithm: cred.Algorithm,
			Opaque: cred.Opaque}
		if cred.QOP != "" {
			chal.QOP = []string{cred.QOP}
		}
		want, err := digest.Digest(chal, digest.Options{Method: string(req.Method), URI: cred.URI,
			Username: cred.Username, Password: password, Cnonce: cred.Cnonce, Count: cred.Nc})
		if err != nil || subtle.ConstantTimeCompare([]byte(want.Response), []byte(cred.Response)) != 1 {
			continue
		}
		ts, mac, found := strings.Cut(cred.Nonce, ".")
		issued, err := strconv.ParseInt(ts, 16, 64)
		check := hmac.New(sha256.New, s.key)
		check.Write([]byte(ts))
		if !found || err != nil || !hmac.Equal([]byte(mac), []byte(hex.EncodeToString(check.Sum(nil)[:12]))) {
			continue
		}
		if time.Since(time.Unix(issued, 0)) > nonceLifetime {
			stale = true
			continue
		}
		ok = true
	}
	if ok {
		for req.RemoveHeader("Proxy-Authorization") {
		}
		for _, h := range keep {
			req.AppendHeader(h)
		}
	}
	return ok, stale && !ok
}
