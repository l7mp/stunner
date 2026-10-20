package stunner

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	srt "github.com/datarhei/gosrt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/l7mp/stunner/v2/internal/api"
	srtsrv "github.com/l7mp/stunner/v2/internal/server/srt"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// srtBackend is an SRT media server for the tests: what the publisher of a stream sends goes to
// every reader of the same stream.
type srtBackend struct {
	ln       srt.Listener
	mu       sync.Mutex
	streams  map[string]srt.PubSub
	accepted []string // the streams of the flows it accepted, in order
}

func startSRTBackend(t *testing.T, addr, passphrase string) *srtBackend {
	t.Helper()
	ln, err := srt.Listen("srt", addr, srt.DefaultConfig())
	require.NoError(t, err, "SRT backend listen")
	b := &srtBackend{ln: ln, streams: map[string]srt.PubSub{}}
	t.Cleanup(ln.Close)
	go func() {
		for {
			req, err := ln.Accept2()
			if err != nil {
				return
			}
			if req.IsEncrypted() {
				if err := req.SetPassphrase(passphrase); err != nil {
					req.Reject(srt.REJ_BADSECRET)
					continue
				}
			}
			sid := req.StreamId()
			conn, err := req.Accept()
			if err != nil {
				continue
			}
			res := srtsrv.Resource(sid)
			b.mu.Lock()
			b.accepted = append(b.accepted, res)
			ps, ok := b.streams[res]
			if !ok {
				ps = srt.NewPubSub(srt.PubSubConfig{Logger: srt.NewLogger(nil)})
				b.streams[res] = ps
			}
			b.mu.Unlock()
			go func() {
				defer conn.Close() //nolint:errcheck
				if strings.Contains(sid, "m=publish") {
					_ = ps.Publish(conn)
				} else {
					_ = ps.Subscribe(conn)
				}
			}()
		}
	}()
	return b
}

func (b *srtBackend) streamsAccepted() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.accepted...)
}

func srtConfig(port int, endpoints ...string) *stnrv2.StunnerConfig {
	c := plainListenerConfig("UDP", "127.0.0.1", port, endpoints...)
	c.Listeners[0].Name, c.Listeners[0].Servers = "srt", []string{"srt"}
	c.Servers[0] = stnrv2.ServerConfig{ServerSpec: stnrv2.ServerSpec{Name: "srt", Type: "SRT",
		Clusters: []string{"peer"}}}
	return c
}

// srtDial calls the SRT server with a stream ID, encrypted with the passphrase unless it is empty.
func srtDial(addr, sid, passphrase string) (srt.Conn, error) {
	cfg := srt.DefaultConfig()
	cfg.StreamId, cfg.Passphrase = sid, passphrase
	return srt.Dial("srt", addr, cfg)
}

// publish sends a numbered payload every 10 ms on the conn until it fails.
func publish(c srt.Conn) {
	for i := 0; ; i++ {
		p := bytes.Repeat([]byte{byte(i)}, 1316)
		if _, err := c.Write(p); err != nil {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// readSome reads from the conn until it got n bytes.
func readSome(t *testing.T, c srt.Conn, n int) {
	t.Helper()
	require.NoError(t, c.SetReadDeadline(time.Now().Add(5*time.Second)))
	buf, got := make([]byte, 1500), 0
	for got < n {
		k, err := c.Read(buf)
		require.NoError(t, err, "read after %d bytes", got)
		got += k
	}
}

// TestStunnerSRT relays SRT flows through the SRT server: a reader gets the stream its publisher
// sends, streams are placed on the least loaded backend, the encryption is end to end, and a
// stream with nowhere to go is rejected.
func TestStunnerSRT(t *testing.T) {
	b1 := startSRTBackend(t, "127.0.0.1:26001", "e2e-secret-phrase")
	b2 := startSRTBackend(t, "127.0.0.1:26002", "e2e-secret-phrase")

	s := NewStunner(Options{Name: "srt", LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
	defer closeStunner(s)
	require.NoError(t, s.Reconcile(srtConfig(23478, "127.0.0.1:26001", "127.0.0.1:26002")), "server started")
	gw := "127.0.0.1:23478"

	t.Run("publish-and-read", func(t *testing.T) {
		pub, err := srtDial(gw, "#!::r=live/a,m=publish", "")
		require.NoError(t, err, "publisher")
		defer pub.Close() //nolint:errcheck
		go publish(pub)
		reader, err := srtDial(gw, "#!::r=live/a,m=request", "")
		require.NoError(t, err, "reader")
		defer reader.Close() //nolint:errcheck
		readSome(t, reader, 10*1316)

		srv := s.GetServer("srt").(api.Server)
		assert.Equal(t, 2, srv.Sessions(), "two relayed flows")
	})

	t.Run("placement", func(t *testing.T) {
		pub, err := srtDial(gw, "#!::r=live/b,m=publish", "")
		require.NoError(t, err, "second publisher")
		defer pub.Close() //nolint:errcheck
		require.Eventually(t, func() bool {
			return len(b1.streamsAccepted())+len(b2.streamsAccepted()) >= 3
		}, 5*time.Second, 10*time.Millisecond)
		a1, a2 := b1.streamsAccepted(), b2.streamsAccepted()
		withA, withB := a1, a2
		if !assert.Contains(t, withA, "live/a") {
			withA, withB = a2, a1
		}
		assert.Equal(t, []string{"live/a", "live/a"}, withA, "the publisher and the reader of live/a meet")
		assert.Equal(t, []string{"live/b"}, withB, "live/b goes to the less loaded backend")
	})

	t.Run("encrypted", func(t *testing.T) {
		pub, err := srtDial(gw, "#!::r=live/c,m=publish", "e2e-secret-phrase")
		require.NoError(t, err, "encrypted publisher")
		defer pub.Close() //nolint:errcheck
		go publish(pub)
		reader, err := srtDial(gw, "#!::r=live/c,m=request", "e2e-secret-phrase")
		require.NoError(t, err, "encrypted reader")
		defer reader.Close() //nolint:errcheck
		readSome(t, reader, 5*1316)
	})

	t.Run("no-stream", func(t *testing.T) {
		_, err := srtDial(gw, "#!::m=publish", "")
		assert.Error(t, err, "a stream ID naming no stream is rejected")
	})

	t.Run("no-endpoint", func(t *testing.T) {
		require.NoError(t, s.Reconcile(srtConfig(23478)), "no endpoints")
		_, err := srtDial(gw, "#!::r=live/d,m=publish", "")
		assert.Error(t, err, "a stream with nowhere to go is rejected")
	})
}

// TestStunnerSRTQuota pins the stream quota and the linger: a stream over the user quota is
// rejected, a stream keeps its endpoint while it lingers after its last flow, and gives its quota
// back once it expired.
func TestStunnerSRTQuota(t *testing.T) {
	b1 := startSRTBackend(t, "127.0.0.1:26003", "")
	b2 := startSRTBackend(t, "127.0.0.1:26004", "")

	s := NewStunner(Options{Name: "srt-quota", LogOptions: LogOptions{Level: stunnerTestLoglevel}, SuppressRollback: true})
	defer closeStunner(s)
	c := srtConfig(23479, "127.0.0.1:26003", "127.0.0.1:26004")
	c.Admin.UserQuota = 1
	c.Servers[0].SRT = &stnrv2.SRTServerConfig{Linger: "500ms"}
	require.NoError(t, s.Reconcile(c), "server started")
	gw := "127.0.0.1:23479"
	backendOf := func(stream string) string {
		for name, b := range map[string]*srtBackend{"b1": b1, "b2": b2} {
			for _, res := range b.streamsAccepted() {
				if res == stream {
					return name
				}
			}
		}
		return ""
	}

	pub, err := srtDial(gw, "#!::r=live/x,m=publish", "")
	require.NoError(t, err, "the first stream fits the quota")
	reader, err := srtDial(gw, "#!::r=live/x,m=request", "")
	require.NoError(t, err, "a reader of a placed stream takes no quota")
	_, err = srtDial(gw, "#!::r=live/y,m=publish", "")
	assert.Error(t, err, "a second stream is over the quota")

	// the publisher reconnects while the reader holds the stream: the same endpoint
	first := backendOf("live/x")
	require.NotEmpty(t, first)
	require.NoError(t, pub.Close())
	pub, err = srtDial(gw, "#!::r=live/x,m=publish", "")
	require.NoError(t, err, "the publisher reconnects")
	require.NoError(t, pub.Close())
	require.NoError(t, reader.Close())

	// lingering: a new publisher of the stream returns to the same endpoint, other streams wait
	pub, err = srtDial(gw, "#!::r=live/x,m=publish", "")
	require.NoError(t, err, "the publisher returns within the linger")
	_, err = srtDial(gw, "#!::r=live/y,m=publish", "")
	assert.Error(t, err, "the lingering stream still holds the quota")
	require.NoError(t, pub.Close())
	accepted := 0
	for _, b := range []*srtBackend{b1, b2} {
		for _, res := range b.streamsAccepted() {
			if res == "live/x" {
				accepted++
			}
		}
	}
	assert.Equal(t, 4, accepted, "every flow of live/x reached the endpoint")
	assert.Equal(t, first, backendOf("live/x"), "live/x never moved")

	// expired: the quota is back
	assert.Eventually(t, func() bool {
		c, err := srtDial(gw, "#!::r=live/y,m=publish", "")
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 2*time.Second, 50*time.Millisecond, "the expired stream gave its quota back")
}
