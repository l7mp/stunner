package sip

import (
	"bufio"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	sipmsg "github.com/emiago/sipgo/sip"
	"github.com/pion/stun/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/netconn"
	objruntime "github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

// registrar is the carrier end of a registration flow, answering by hand.
type registrar struct {
	t    *testing.T
	conn net.Conn
	r    *bufio.Reader
}

// next reads the next REGISTER of the flow.
func (r *registrar) next() *sipmsg.Request {
	r.t.Helper()
	require.NoError(r.t, r.conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	var msg strings.Builder
	for !strings.HasSuffix(msg.String(), "\r\n\r\n") {
		line, err := r.r.ReadString('\n')
		require.NoError(r.t, err, "a REGISTER arrives")
		msg.WriteString(line)
	}
	m, err := sipmsg.ParseMessage([]byte(msg.String()))
	require.NoError(r.t, err)
	req, ok := m.(*sipmsg.Request)
	require.True(r.t, ok)
	require.Equal(r.t, sipmsg.REGISTER, req.Method)
	return req
}

// answer answers a REGISTER with a status and headers.
func (r *registrar) answer(req *sipmsg.Request, code int, reason string, headers ...sipmsg.Header) {
	r.t.Helper()
	res := sipmsg.NewResponseFromRequest(req, code, reason, nil)
	for _, h := range headers {
		res.AppendHeader(h)
	}
	_, err := r.conn.Write([]byte(res.String()))
	require.NoError(r.t, err)
}

// datagram reads the next message of a datagram flow.
func (r *registrar) datagram() []byte {
	r.t.Helper()
	require.NoError(r.t, r.conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf := make([]byte, 65535)
	n, err := r.conn.Read(buf)
	require.NoError(r.t, err, "a datagram arrives")
	return buf[:n]
}

// closed tells whether the server closed the flow.
func (r *registrar) closed() bool {
	_ = r.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, err := r.r.ReadByte()
	return err == io.EOF || err == io.ErrClosedPipe
}

// newRegistration starts a SIP server with the registration "carrier" and hands it a flow of a
// protocol labeled with it; the other end of the flow is returned.
func newRegistration(t *testing.T, proto stnrv2.Protocol) *registrar {
	t.Helper()
	rt := objruntime.New(objruntime.Config{Logger: logger.NewLoggerFactory("all:ERROR")})
	conf := &stnrv2.ServerConfig{ServerSpec: stnrv2.ServerSpec{Name: "sip", Type: "SIP"},
		SIP: &stnrv2.SIPServerConfig{Registrations: []stnrv2.SIPRegistrationConfig{
			{Name: "carrier", Auth: "carrier-digest", AOR: "sip:+3612345678@sip.carrier.example"}}}}
	require.NoError(t, conf.Validate())
	s, err := newServer(conf, rt)
	require.NoError(t, err)
	require.NoError(t, s.Start())
	t.Cleanup(func() { _ = s.Close(false) })

	client, server := net.Pipe()
	t.Cleanup(func() { _ = client.Close() })
	_, _, err = s.Serve(netconn.NewConn(server, api.Tag{Name: "carrier", Proto: proto,
		Labels: map[string]string{stnrv2.RegistrationLabel: "carrier"}}, nil))
	require.NoError(t, err)
	return &registrar{t: t, conn: client, r: bufio.NewReader(client)}
}

func TestRegistration(t *testing.T) {
	t.Run("register", func(t *testing.T) {
		r := newRegistration(t, stnrv2.ProtocolTCP)
		req := r.next()
		assert.Equal(t, "sip:sip.carrier.example", req.Recipient.String())
		assert.Equal(t, "sip:+3612345678@sip.carrier.example", req.To().Address.String())
		assert.Equal(t, "600", req.GetHeader("Expires").Value(), "the default expiry")
		ct := req.Contact()
		require.NotNil(t, ct)
		assert.Equal(t, "+3612345678", ct.Address.User)
		assert.True(t, ct.Address.UriParams.Has("ob"))
		transport, _ := ct.Address.UriParams.Get("transport")
		assert.Equal(t, "tcp", transport)
		assert.True(t, ct.Params.Has("+sip.instance"))
		assert.True(t, ct.Params.Has("reg-id"))
		assert.Contains(t, req.GetHeader("Supported").Value(), "outbound")
		assert.Equal(t, "TCP", req.Via().Transport)

		// a refresh comes at half of the granted expiry, with the same Call-ID and a new CSeq
		granted := ct.Clone()
		granted.Params.Add("expires", "2")
		r.answer(req, sipmsg.StatusOK, "OK", granted)
		refresh := r.next()
		assert.Equal(t, req.CallID().Value(), refresh.CallID().Value())
		assert.Equal(t, req.CSeq().SeqNo+1, refresh.CSeq().SeqNo)
	})

	t.Run("interval too brief", func(t *testing.T) {
		r := newRegistration(t, stnrv2.ProtocolTCP)
		req := r.next()
		r.answer(req, sipmsg.StatusIntervalToBrief, "Interval Too Brief",
			sipmsg.NewHeader("Min-Expires", "3600"))
		retry := r.next()
		assert.Equal(t, "3600", retry.GetHeader("Expires").Value(), "the registrar's minimum")
	})

	t.Run("challenge", func(t *testing.T) {
		// the open-source build answers no digest challenge: the flow is closed, so its dialing
		// listener redials
		r := newRegistration(t, stnrv2.ProtocolTCP)
		req := r.next()
		r.answer(req, sipmsg.StatusUnauthorized, "Unauthorized",
			sipmsg.NewHeader("WWW-Authenticate", `Digest realm="carrier", nonce="n"`))
		assert.True(t, r.closed(), "the flow is closed")
	})

	t.Run("refused", func(t *testing.T) {
		r := newRegistration(t, stnrv2.ProtocolTCP)
		r.answer(r.next(), sipmsg.StatusForbidden, "Forbidden")
		assert.True(t, r.closed(), "the flow is closed")
	})

	t.Run("keepalives", func(t *testing.T) {
		defer func(i [2]time.Duration) { KeepaliveInterval = i }(KeepaliveInterval)
		KeepaliveInterval = [2]time.Duration{10 * time.Millisecond, 20 * time.Millisecond}

		// register over a datagram flow, granted for long
		register := func(t *testing.T) *registrar {
			r := newRegistration(t, stnrv2.ProtocolUDP)
			m, err := sipmsg.ParseMessage(r.datagram())
			require.NoError(t, err)
			req, ok := m.(*sipmsg.Request)
			require.True(t, ok)
			granted := req.Contact().Clone()
			granted.Params.Add("expires", "600")
			r.answer(req, sipmsg.StatusOK, "OK", granted)
			return r
		}
		keepalive := func(t *testing.T, r *registrar) *stun.Message {
			m := &stun.Message{Raw: r.datagram()}
			require.NoError(t, m.Decode())
			require.Equal(t, stun.BindingRequest, m.Type)
			return m
		}
		answer := func(t *testing.T, r *registrar, req *stun.Message) {
			res := stun.MustBuild(stun.NewTransactionIDSetter(req.TransactionID), stun.BindingSuccess)
			_, err := r.conn.Write(res.Raw)
			require.NoError(t, err)
		}

		t.Run("answered", func(t *testing.T) {
			r := register(t)
			for range KeepaliveMisses + 3 {
				answer(t, r, keepalive(t, r))
			}
			keepalive(t, r)
		})

		t.Run("never answered", func(t *testing.T) {
			// a registrar without outbound support: the flow is never failed by keepalives
			r := register(t)
			for range KeepaliveMisses + 3 {
				keepalive(t, r)
			}
		})

		t.Run("unanswered after an answer", func(t *testing.T) {
			r := register(t)
			answer(t, r, keepalive(t, r))
			for range KeepaliveMisses {
				keepalive(t, r)
			}
			assert.True(t, r.closed(), "the flow is closed")
		})
	})
}
