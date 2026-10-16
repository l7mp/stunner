package softphone

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/pion/logging"
	"github.com/pion/transport/v5/vnet"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	call, err := caller.Dial(ctx, "sip:echo@10.0.0.2:5060")
	require.NoError(t, err)
	var answered *Call
	select {
	case answered = <-callee.Incoming():
	case <-ctx.Done():
		t.Fatal("the callee never answered")
	}

	require.Eventually(t, func() bool {
		s := call.Stats()
		return s.Received >= 50 && s.RTCPReceived > 0
	}, 5*time.Second, 50*time.Millisecond, "the silence comes back: %s", call.Stats())
	assert.Zero(t, call.Stats().Lost)

	require.NoError(t, call.Hangup(ctx))
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
