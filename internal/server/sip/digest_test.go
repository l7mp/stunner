package sip

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"testing"
	"time"

	sipmsg "github.com/emiago/sipgo/sip"
	"github.com/icholy/digest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

func TestCheckDigest(t *testing.T) {
	rt := objruntime.New(objruntime.Config{Logger: logger.NewLoggerFactory("all:ERROR")})
	auth := &stnrv2.AuthConfig{Name: "carriers", Type: stnrv2.AuthTypeStatic.String(), Realm: "sip.example.com",
		Credentials: map[string]string{"username": "carrier", "password": "secret"}}
	require.NoError(t, rt.Registry.Add(authPolicy{auth}, nil))
	conf := &stnrv2.ServerConfig{ServerSpec: stnrv2.ServerSpec{Name: "sip", Type: "SIP"},
		SIP: &stnrv2.SIPServerConfig{Auth: "carriers"}}
	require.NoError(t, conf.Validate())
	s, err := newServer(conf, rt)
	require.NoError(t, err)

	const target = "sip:echo@sip.example.com:5060"
	// invite makes an INVITE of the target carrying the digest of a nonce, a URI and a password
	invite := func(nonce, uri, password string) *sipmsg.Request {
		var recipient sipmsg.Uri
		require.NoError(t, sipmsg.ParseUri(target, &recipient))
		req := sipmsg.NewRequest(sipmsg.INVITE, recipient)
		cred, err := digest.Digest(&digest.Challenge{Realm: auth.Realm, Nonce: nonce, Algorithm: "MD5",
			QOP: []string{"auth"}}, digest.Options{Method: "INVITE", URI: uri, Username: "carrier",
			Password: password, Count: 1, Cnonce: "0a4f113b"})
		require.NoError(t, err)
		req.AppendHeader(sipmsg.NewHeader("Proxy-Authorization", cred.String()))
		return req
	}

	t.Run("valid", func(t *testing.T) {
		req := invite(s.nonce(), target, "secret")
		ok, stale := s.checkDigest(req, auth)
		assert.True(t, ok)
		assert.False(t, stale)
		assert.Nil(t, req.GetHeader("Proxy-Authorization"), "the consumed credentials are removed")
	})

	t.Run("case-insensitive scheme and host", func(t *testing.T) {
		ok, _ := s.checkDigest(invite(s.nonce(), "SIP:echo@SIP.EXAMPLE.COM:5060", "secret"), auth)
		assert.True(t, ok)
	})

	t.Run("wrong password", func(t *testing.T) {
		ok, stale := s.checkDigest(invite(s.nonce(), target, "guess"), auth)
		assert.False(t, ok)
		assert.False(t, stale)
	})

	t.Run("another URI", func(t *testing.T) {
		// a digest computed for another Request-URI does not count for this one, even when it is
		// right for its own URI
		req := invite(s.nonce(), "sip:voicemail@sip.example.com:5060", "secret")
		ok, stale := s.checkDigest(req, auth)
		assert.False(t, ok)
		assert.False(t, stale)
		assert.NotNil(t, req.GetHeader("Proxy-Authorization"), "failed credentials stay")
	})

	t.Run("forged nonce", func(t *testing.T) {
		ts := strconv.FormatInt(time.Now().Unix(), 16)
		ok, stale := s.checkDigest(invite(ts+".000000000000000000000000", target, "secret"), auth)
		assert.False(t, ok)
		assert.False(t, stale)
	})

	t.Run("expired nonce", func(t *testing.T) {
		ts := strconv.FormatInt(time.Now().Add(-nonceLifetime-time.Minute).Unix(), 16)
		mac := hmac.New(sha256.New, s.key)
		mac.Write([]byte(ts))
		nonce := ts + "." + hex.EncodeToString(mac.Sum(nil)[:12])
		ok, stale := s.checkDigest(invite(nonce, target, "secret"), auth)
		assert.False(t, ok)
		assert.True(t, stale, "right credentials with an expired nonce are stale")
	})
}
