package ng

import (
	"context"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/fifo"
)

func TestBencode(t *testing.T) {
	for _, tc := range []struct {
		v    any
		wire string
	}{
		{"spam", "4:spam"},
		{"", "0:"},
		{int64(42), "i42e"},
		{int64(-3), "i-3e"},
		{[]any{"a", int64(1)}, "l1:ai1ee"},
		{Message{"b": "x", "a": []any{}}, "d1:ale1:b1:xe"},
		{Message{"command": "ping"}, "d7:command4:pinge"},
	} {
		b, err := Encode(tc.v)
		require.NoError(t, err)
		assert.Equal(t, tc.wire, string(b))
		v, err := Decode(b)
		require.NoError(t, err)
		assert.Equal(t, tc.v, v)
	}
	for _, bad := range []string{"", "i12", "l1:a", "d1:a", "5:abc", "di1e1:ae", "x", "1:ab"} {
		_, err := Decode([]byte(bad))
		assert.Error(t, err, bad)
	}
}

func TestRequestRoundTrip(t *testing.T) {
	req := &Request{Command: CommandOffer, CallID: "c1", FromTag: "f1", SDP: "v=0\r\n",
		Direction: []string{DirectionExternal, DirectionInternal}, ICE: "remove",
		Replace: []string{"origin"}, RTCPMux: []string{"offer"}}
	b, err := Encode(req.Message())
	require.NoError(t, err)
	v, err := Decode(b)
	require.NoError(t, err)
	got, err := ParseRequest(v.(Message))
	require.NoError(t, err)
	assert.Equal(t, req, got)

	_, err = ParseRequest(Message{"call-id": "c1"})
	assert.Error(t, err, "a request names its command")
	_, err = ParseRequest(Message{"command": "offer", "direction": "external"})
	assert.Error(t, err, "direction is a list")
}

func TestClientServer(t *testing.T) {
	RetransmitInterval = 20 * time.Millisecond
	for _, enc := range []Encoding{Bencode, JSON} {
		t.Run(map[Encoding]string{Bencode: "bencode", JSON: "json"}[enc], func(t *testing.T) {
			fifos := fifo.New()
			l, err := fifos.Listen(context.Background(), fifo.Network, "ng")
			require.NoError(t, err)

			// the server drops the first copy of every offer, so the client must send it again, and it
			// counts the offers it handles: a retransmission after the reply is answered from the cache
			var handled atomic.Int32
			go func() {
				c, err := l.Accept()
				if err != nil {
					return
				}
				_ = Serve(dropFirst{Conn: c, seen: map[string]bool{}}, func(req *Request) *Response {
					switch req.Command {
					case CommandPing:
						return &Response{Result: ResultPong}
					case CommandOffer:
						handled.Add(1)
						return &Response{Result: ResultOK, SDP: "rewritten:" + req.SDP}
					default:
						return Errorf("unsupported command %q", req.Command)
					}
				})
			}()

			conn, err := fifos.Dial(fifo.Network, "ng")
			require.NoError(t, err)
			cl := NewClient(conn, enc)
			defer cl.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			resp, err := cl.Do(ctx, &Request{Command: CommandPing})
			require.NoError(t, err)
			assert.Equal(t, ResultPong, resp.Result)

			resp, err = cl.Do(ctx, &Request{Command: CommandOffer, CallID: "c1", SDP: "sdp"})
			require.NoError(t, err)
			assert.Equal(t, "rewritten:sdp", resp.SDP)
			assert.Equal(t, int32(1), handled.Load())

			resp, err = cl.Do(ctx, &Request{Command: "list"})
			assert.Error(t, err)
			require.NotNil(t, resp)
			assert.Equal(t, ResultError, resp.Result)
			assert.Contains(t, resp.ErrorReason, "unsupported")
		})
	}
}

// dropFirst loses the first datagram of every offer it reads.
type dropFirst struct {
	net.Conn
	seen map[string]bool
}

func (d dropFirst) Read(p []byte) (int, error) {
	for {
		n, err := d.Conn.Read(p)
		if err != nil {
			return n, err
		}
		cookie, m, _, err := unmarshal(p[:n])
		if err != nil || m["command"] != CommandOffer || d.seen[cookie] {
			return n, nil
		}
		d.seen[cookie] = true
	}
}
