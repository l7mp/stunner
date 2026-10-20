package siptx

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/emiago/sipgo/sip"
	"github.com/pion/logging"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/pkg/udp"
)

// pair serves the two ends of a loopback UDP flow, each with a layer of its own.
func pair(t *testing.T) (uac *Layer, uacConn net.Conn, uas *Layer) {
	t.Helper()
	log := logging.NewDefaultLoggerFactory().NewLogger("siptx")
	uac, uas = New(log), New(log)
	l, err := udp.ListenConfig{}.Listen("udp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = l.Close() })
	uacConn, err = net.Dial("udp", l.Addr().String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = uacConn.Close() })
	go func() { _ = uac.Serve(uacConn, false) }()
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go func() { _ = uas.Serve(c, false) }()
		}
	}()
	return uac, uacConn, uas
}

func request(method sip.RequestMethod, c net.Conn) *sip.Request {
	req := sip.NewRequest(method, sip.Uri{Scheme: "sip", User: "x", Host: "127.0.0.1"})
	params := sip.NewParams()
	params.Add("branch", sip.RFC3261BranchMagicCookie+time.Now().Format("150405.000000000"))
	host, port, _ := net.SplitHostPort(c.LocalAddr().String())
	p, _ := net.LookupPort("udp", port)
	req.AppendHeader(&sip.ViaHeader{ProtocolName: "SIP", ProtocolVersion: "2.0", Transport: "UDP",
		Host: host, Port: p, Params: params})
	from := &sip.FromHeader{Address: sip.Uri{Scheme: "sip", User: "a", Host: host}, Params: sip.NewParams()}
	from.Params.Add("tag", "a1")
	req.AppendHeader(from)
	req.AppendHeader(&sip.ToHeader{Address: req.Recipient, Params: sip.NewParams()})
	cid := sip.CallIDHeader("call-" + string(method))
	req.AppendHeader(&cid)
	req.AppendHeader(&sip.CSeqHeader{SeqNo: 1, MethodName: method})
	req.SetBody(nil)
	return req
}

func TestTransactions(t *testing.T) {
	uac, c, uas := pair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	invites, acks := make(chan *ServerTx, 4), make(chan *sip.Request, 4)
	uas.OnRequest(func(req *sip.Request, tx *ServerTx, _ net.Conn) {
		switch {
		case tx == nil:
			acks <- req
		case req.Method == sip.INVITE:
			_ = tx.Respond(sip.NewResponseFromRequest(req, sip.StatusRinging, "Ringing", nil))
			invites <- tx
		default:
			_ = tx.Respond(sip.NewResponseFromRequest(req, sip.StatusOK, "OK", nil))
		}
	})
	strays := make(chan *sip.Response, 4)
	uac.OnResponse(func(res *sip.Response, _ net.Conn) { strays <- res })

	// a request and its final response
	res, err := uac.Do(ctx, request(sip.OPTIONS, c), c)
	require.NoError(t, err)
	assert.Equal(t, sip.StatusOK, res.StatusCode)

	// a retransmitted INVITE reaches the handler once; cancelling it brings the 487
	inv := request(sip.INVITE, c)
	tx, err := uac.Request(inv, c)
	require.NoError(t, err)
	require.NoError(t, uac.Write(c, inv))
	stx := <-invites
	cctx, ccancel := context.WithCancel(ctx)
	go func() {
		<-stx.Cancelled()
		ccancel()
	}()
	go func() {
		time.Sleep(100 * time.Millisecond)
		ccancel()
	}()
	res, err = tx.Wait(cctx)
	require.NoError(t, err)
	assert.Equal(t, sip.StatusRequestTerminated, res.StatusCode)
	select {
	case <-stx.Cancelled():
	default:
		t.Fatal("the server transaction was not cancelled")
	}
	assert.Empty(t, invites, "the retransmission is absorbed")

	// the ACK of a 2xx has no transaction
	require.NoError(t, uac.Write(c, request(sip.ACK, c)))
	assert.Equal(t, sip.ACK, (<-acks).Method)

	// a CANCEL matching nothing gets 481, a malformed request 400, both as strays here
	require.NoError(t, uac.Write(c, request(sip.CANCEL, c)))
	assert.Equal(t, sip.StatusCallTransactionDoesNotExists, (<-strays).StatusCode)
	bad := request(sip.OPTIONS, c)
	bad.RemoveHeader("Call-ID")
	require.NoError(t, uac.Write(c, bad))
	assert.Equal(t, sip.StatusBadRequest, (<-strays).StatusCode)

	// a branch without the RFC 3261 magic cookie makes no transaction key: 400 as well
	legacy := request(sip.OPTIONS, c)
	legacy.Via().Params.Add("branch", "1234")
	require.NoError(t, uac.Write(c, legacy))
	assert.Equal(t, sip.StatusBadRequest, (<-strays).StatusCode)
}

func TestConnClose(t *testing.T) {
	uac, c, uas := pair(t)
	uas.OnRequest(func(*sip.Request, *ServerTx, net.Conn) {}) // never answers
	tx, err := uac.Request(request(sip.OPTIONS, c), c)
	require.NoError(t, err)
	require.NoError(t, c.Close())
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, err = tx.Wait(ctx)
	require.Error(t, err)
	assert.False(t, errors.Is(err, context.DeadlineExceeded), "the closed conn ends the transaction")
}

func TestStream(t *testing.T) {
	a, b := net.Pipe()
	defer a.Close()
	log := logging.NewDefaultLoggerFactory().NewLogger("siptx")
	l := New(log)
	got := make(chan sip.RequestMethod, 2)
	l.OnRequest(func(req *sip.Request, tx *ServerTx, _ net.Conn) { got <- req.Method })
	go func() { _ = l.Serve(b, true) }()
	// two messages in one write, framed by Content-Length
	_, err := a.Write([]byte(request(sip.OPTIONS, a).String() + request(sip.MESSAGE, a).String()))
	require.NoError(t, err)
	assert.ElementsMatch(t, []sip.RequestMethod{sip.OPTIONS, sip.MESSAGE}, []sip.RequestMethod{<-got, <-got})
}
