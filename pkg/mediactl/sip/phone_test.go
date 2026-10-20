package sip

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v5/vnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/pkg/mediactl"
)

// twoNets makes a virtual network with two hosts, 10.0.0.1 and 10.0.0.2.
func twoNets(t *testing.T) (*vnet.Net, *vnet.Net) {
	t.Helper()
	r, err := vnet.NewRouter(&vnet.RouterConfig{CIDR: "10.0.0.0/24",
		LoggerFactory: logging.NewDefaultLoggerFactory()})
	require.NoError(t, err)
	a, err := vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{"10.0.0.1"}})
	require.NoError(t, err)
	b, err := vnet.NewNet(&vnet.NetConfig{StaticIPs: []string{"10.0.0.2"}})
	require.NoError(t, err)
	require.NoError(t, r.AddNet(a))
	require.NoError(t, r.AddNet(b))
	require.NoError(t, r.Start())
	t.Cleanup(func() { _ = r.Stop() })
	return a, b
}

func TestPing(t *testing.T) {
	na, nb := twoNets(t)
	peer, err := New(Config{Net: nb, Addr: "10.0.0.2:5060"})
	require.NoError(t, err)
	defer peer.Close()
	p, err := New(Config{Net: na})
	require.NoError(t, err)
	defer p.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	assert.NoError(t, p.Ping(ctx, "sip:10.0.0.2:5060"), "the peer answers the OPTIONS")

	ctx2, cancel2 := context.WithTimeout(context.Background(), time.Second)
	defer cancel2()
	assert.Error(t, p.Ping(ctx2, "sip:10.0.0.2:5070"), "nobody answers on another port")
}

func TestCall(t *testing.T) {
	na, nb := twoNets(t)
	callee, err := New(Config{Net: nb, Addr: "10.0.0.2:5060"})
	require.NoError(t, err)
	defer callee.Close()
	caller, err := New(Config{Net: na})
	require.NoError(t, err)
	defer caller.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := caller.DialContext(ctx, "udp", "sip:echo@10.0.0.2:5060")
	require.NoError(t, err)
	call := conn.(*Call)
	in, err := callee.Accept()
	require.NoError(t, err, "the callee answered")
	answered := in.(*Call)
	go mediactl.Echo(answered, true)

	// a call is a conn of its media: a stream runs over it, here 50 packets of 20 ms G.711
	sctx, scancel := context.WithTimeout(ctx, time.Second)
	defer scancel()
	r := mediactl.Stream(sctx, call, mediactl.Config{Length: 160, Rate: 50, Echo: true}).Result()
	assert.InDelta(t, 50, r.Sent, 2)
	assert.Zero(t, r.Lost, "the echo came back: %+v", r)
	assert.NotZero(t, r.RTTMax, "the round trip through the RTP payloads")
	s := call.Stats()
	assert.Equal(t, int(r.Sent), s.Sent, "every write is an RTP packet")
	assert.Positive(t, s.RTCPReceived, "the echo sends its own sender reports: %s", s)

	// the stream closed the call: a BYE
	select {
	case <-answered.Done():
		assert.Equal(t, "remote hangup", answered.EndReason())
	case <-ctx.Done():
		t.Fatal("the callee did not see the BYE")
	}

	// a callee ringing forever: the caller cancels
	callee.cfg.AnswerDelay = time.Hour
	dctx, dcancel := context.WithTimeout(ctx, 500*time.Millisecond)
	_, err = caller.Dial(dctx, "sip:echo@10.0.0.2:5060")
	dcancel()
	var se *StatusError
	require.True(t, errors.As(err, &se), "a cancelled call ends with a final response: %v", err)
	assert.Equal(t, 487, se.Code)
}
