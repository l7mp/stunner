package main

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/l7mp/stunner/v2/pkg/mediactl"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMain runs the tests on short timers.
func TestMain(m *testing.M) {
	mediactl.EchoDrain, mediactl.FlowIdle = 50*time.Millisecond, 100*time.Millisecond
	os.Exit(m.Run())
}

// run runs mediactl with the args until it returns or the context ends.
func run(ctx context.Context, args ...string) error {
	cmd := newRootCmd()
	cmd.SetArgs(append(args, "--log", "all:WARN"))
	return cmd.ExecuteContext(ctx)
}

// serve runs a mediactl server until the test ends.
func serve(t *testing.T, args ...string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Go(func() { assert.NoError(t, run(ctx, args...), "server %v", args) })
	t.Cleanup(func() { cancel(); wg.Wait() })
}

// eventually runs a mediactl sender until it succeeds, so it need not wait for its server.
func eventually(t *testing.T, args ...string) {
	t.Helper()
	assert.Eventually(t, func() bool {
		return run(context.Background(), append(args, "--timeout", "300ms")...) == nil
	}, 10*time.Second, 100*time.Millisecond, "sender %v", args)
}

func TestUDP(t *testing.T) {
	serve(t, "udp", "echo", "127.0.0.1:27001")
	eventually(t, "udp", "send", "127.0.0.1:27001", "-k", "200", "--length", "100", "-v")
}

func TestTCP(t *testing.T) {
	serve(t, "tcp", "echo", "127.0.0.1:27002")
	eventually(t, "tcp", "send", "127.0.0.1:27002")
}

func TestNothingReceived(t *testing.T) {
	err := run(context.Background(), "udp", "send", "127.0.0.1:27003", "--timeout", "100ms")
	assert.ErrorIs(t, err, mediactl.ErrNothingReceived, "no echo is a failure")
}

func TestSIP(t *testing.T) {
	serve(t, "sip", "answer", "127.0.0.1:27004")
	eventually(t, "sip", "call", "sip:echo@127.0.0.1:27004")
}

func TestSRT(t *testing.T) {
	serve(t, "srt", "serve", "127.0.0.1:27006", "--passphrase", "srt-test-phrase")

	t.Run("echo", func(t *testing.T) {
		eventually(t, "srt", "send", "127.0.0.1:27006", "live/echo")
	})

	t.Run("publish-and-read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var wg sync.WaitGroup
		wg.Go(func() {
			_ = run(ctx, "srt", "publish", "127.0.0.1:27006", "live/a", "--passphrase", "srt-test-phrase")
		})
		eventually(t, "srt", "read", "127.0.0.1:27006", "live/a", "--passphrase", "srt-test-phrase")
		cancel()
		wg.Wait()
	})

	t.Run("bad-passphrase", func(t *testing.T) {
		err := run(context.Background(), "srt", "send", "127.0.0.1:27006", "live/x", "--passphrase",
			"another-passphrase", "--timeout", "100ms")
		assert.Error(t, err, "a wrong passphrase is rejected")
	})
}

func TestEnv(t *testing.T) {
	t.Setenv("MEDIACTL_TIMEOUT", "100ms")
	start := time.Now()
	err := run(context.Background(), "udp", "send", "127.0.0.1:27007")
	assert.ErrorIs(t, err, mediactl.ErrNothingReceived)
	assert.Less(t, time.Since(start), 5*time.Second, "the environment sets the timeout")
}

func TestParallelJSON(t *testing.T) {
	serve(t, "udp", "echo", "127.0.0.1:27011")
	// the echo may not be up yet: wait for one stream through it first
	eventually(t, "udp", "send", "127.0.0.1:27011")

	out := captureStdout(t, func() {
		require.NoError(t, run(context.Background(), "udp", "send", "127.0.0.1:27011", "-P", "3", "-o", "json",
			"-k", "100", "-t", "300ms"))
	})
	var res struct {
		Streams []map[string]any `json:"streams"`
		Total   map[string]any   `json:"total"`
	}
	require.NoError(t, json.Unmarshal([]byte(out), &res), out)
	assert.Len(t, res.Streams, 3)
	assert.Positive(t, res.Total["received"])
	assert.NotEmpty(t, res.Total["rtt_avg"])
}

func TestThresholds(t *testing.T) {
	serve(t, "udp", "echo", "127.0.0.1:27012")
	eventually(t, "udp", "send", "127.0.0.1:27012")
	err := run(context.Background(), "udp", "send", "127.0.0.1:27012", "-t", "200ms", "--max-rtt", "1ns")
	assert.ErrorContains(t, err, "average round trip", "no round trip is that short")
	assert.NoError(t, run(context.Background(), "udp", "send", "127.0.0.1:27012", "-t", "200ms", "--max-loss", "0"))
}

func TestTS(t *testing.T) {
	serve(t, "udp", "echo", "127.0.0.1:27013")
	eventually(t, "udp", "send", "127.0.0.1:27013")
	ts := filepath.Join(t.TempDir(), "test.ts")
	require.NoError(t, os.WriteFile(ts, make([]byte, 188*10), 0o600))
	assert.NoError(t, run(context.Background(), "udp", "send", "127.0.0.1:27013", "-t", "200ms",
		"--ts", ts, "--length", "1316", "-k", "100"), "the file loops at its end")
}

func TestK8sAddress(t *testing.T) {
	err := run(context.Background(), "udp", "send", "k8s://only-namespace", "-t", "100ms")
	assert.ErrorContains(t, err, "invalid Gateway address")
	err = run(context.Background(), "turn", "send", "turn:127.0.0.1:3478", "k8s://ns/svc", "-t", "100ms")
	assert.ErrorContains(t, err, "invalid Service address")
}

// captureStdout returns what f prints on the standard output.
func captureStdout(t *testing.T, f func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	stdout := os.Stdout
	os.Stdout = w
	f()
	os.Stdout = stdout
	require.NoError(t, w.Close())
	b, err := io.ReadAll(r)
	require.NoError(t, err)
	return string(b)
}
