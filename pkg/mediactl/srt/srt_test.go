package srt

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/pkg/mediactl"
)

// TestServe runs an SRT listener under mediactl.Serve: a reader gets the stream of its publisher, an echo caller its
// own packets, and a caller with a wrong passphrase nothing.
func TestServe(t *testing.T) {
	mediactl.EchoDrain = 50 * time.Millisecond
	ln, err := Listen("127.0.0.1:0", "srt-test-phrase", nil)
	require.NoError(t, err)
	var wg sync.WaitGroup
	wg.Go(func() { _ = mediactl.Serve(ln, true, nil) })
	defer func() { _ = ln.Close(); wg.Wait() }()
	addr := ln.Addr().String()
	dial := func(stream string, m Mode, passphrase string) (net.Conn, error) {
		return Dialer{Stream: stream, Mode: m, Passphrase: passphrase}.DialContext(context.Background(), "srt", addr)
	}
	c := mediactl.Config{Length: 1000, Rate: 200}

	t.Run("publish-and-read", func(t *testing.T) {
		pub, err := dial("live/a", Publish, "srt-test-phrase")
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		go mediactl.Stream(ctx, pub, Config(Publish, c))
		reader, err := dial("live/a", Request, "srt-test-phrase")
		require.NoError(t, err)
		r := mediactl.Stream(ctx, reader, Config(Request, c)).Result()
		assert.Zero(t, r.Sent, "a reader sends nothing")
		assert.NotZero(t, r.Received, "the reader got the stream")
	})

	t.Run("echo", func(t *testing.T) {
		conn, err := dial("live/echo", Echo, "srt-test-phrase")
		require.NoError(t, err)
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		r := mediactl.Stream(ctx, conn, Config(Echo, c)).Result()
		assert.NotZero(t, r.Received, "the echo came back")
		assert.NotZero(t, r.RTTMax, "the round trip")
	})

	t.Run("bad-passphrase", func(t *testing.T) {
		_, err := dial("live/x", Echo, "another-passphrase")
		assert.Error(t, err)
	})

	assert.Equal(t, "#!::r=live/a,m=publish", StreamID("live/a", Publish))
	assert.Equal(t, "#!::r=live/a", StreamID("live/a", Echo))
}
