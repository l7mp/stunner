package netutil

import (
	"net"
	"testing"
	"time"

	"github.com/pion/transport/v4/test"
	"github.com/pion/transport/v4/vnet"
	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/pkg/logger"
)

var connTestLoglevel string = "all:ERROR"

var testCluster = "test-cluster"

func getChecker(minPort, maxPort int) AdmitFunc {
	return func(addr net.Addr) (string, bool) {
		u, ok := addr.(*net.UDPAddr)
		if !ok {
			return "", false
		}

		return testCluster, u.Port >= minPort && u.Port <= maxPort
	}
}

func TestPortRangePacketConn(t *testing.T) {
	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(connTestLoglevel)
	log := loggerFactory.NewLogger("test")

	log.Debug("creating vnet")
	nw, err := vnet.NewNet(&vnet.NetConfig{})
	if !assert.NoError(t, err, "should succeed") {
		return
	}

	tm, err := telemetry.New(telemetry.Callbacks{}, false, loggerFactory.NewLogger("metric"))
	assert.NoError(t, err, "should succeed")
	defer tm.Close() //nolint:errcheck

	t.Run("LoopbackOnValidPort", func(t *testing.T) {
		log.Debug("creating base socket")
		addr := "127.0.0.1:15000"
		baseConn, err := nw.ListenPacket("udp4", addr)
		assert.NoError(t, err, "should succeed")
		msg := "PING!"

		log.Debug("creating filtered packet conn wrappeer socket")
		conn := NewPacketConn(baseConn, testCluster, telemetry.ClusterType, tm, getChecker(10000, 20000), log)
		assert.NoError(t, err, "should create port-range filtered packetconn")

		log.Debug("sending packet")
		udpAddr, err := net.ResolveUDPAddr("udp4", addr)
		assert.NoError(t, err, "should resolve UDP address")
		n, err := conn.WriteTo([]byte(msg), udpAddr)
		assert.NoError(t, err, "should succeed")
		assert.Equal(t, len(msg), n, "should match")

		log.Debug("receiving packet")
		buf := make([]byte, 1000)
		n, remoteAddr, err := conn.ReadFrom(buf)
		assert.NoError(t, err, "should succeed")
		assert.Equal(t, len(msg), n, "should match")
		assert.Equal(t, msg, string(buf[:n]), "should match")
		assert.Equal(t, udpAddr.String(), remoteAddr.String(), "should match") //nolint:forcetypeassert

		log.Debug("closing connection")
		assert.NoError(t, conn.Close(), "should succeed") // should close baseConn
	})

	t.Run("LoopbackOnInvalidPort", func(t *testing.T) {
		log.Debug("creating base socket")
		addr := "127.0.0.1:25000"
		baseConn, err := nw.ListenPacket("udp4", addr)
		assert.NoError(t, err, "should succeed")
		msg := "PING!"

		log.Debug("creating filtered packet conn wrappeer socket")
		conn := NewPacketConn(baseConn, testCluster, telemetry.ClusterType, tm, getChecker(10000, 20000), log)
		assert.NoError(t, err, "should create port-range filtered packetconn")

		log.Debug("sending packet")
		udpAddr, err := net.ResolveUDPAddr("udp4", addr)
		assert.NoError(t, err, "should resolve UDP address")
		n, err := conn.WriteTo([]byte(msg), udpAddr)
		assert.Error(t, err, "should reject")
		assert.Equal(t, 0, n, "should match")

		log.Debug("receiving packet")
		buf := make([]byte, 1000)
		// this would hang otherwise
		assert.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Millisecond)), "read deadline")
		_, _, err = conn.ReadFrom(buf)
		assert.Error(t, err, "should be rejected")

		log.Debug("closing connection")
		assert.NoError(t, conn.Close(), "should succeed")
	})

	t.Run("LoopbackOnSinglePort", func(t *testing.T) {
		log.Debug("creating base socket")
		addr := "127.0.0.1:15000"
		baseConn, err := nw.ListenPacket("udp4", addr)
		assert.NoError(t, err, "should succeed")
		msg := "PING!"

		log.Debug("creating filtered packet conn wrappeer socket")
		conn := NewPacketConn(baseConn, testCluster, telemetry.ClusterType, tm, getChecker(15000, 15000), log)
		assert.NoError(t, err, "should create port-range filtered packetconn")

		log.Debug("sending packet")
		udpAddr, err := net.ResolveUDPAddr("udp4", addr)
		assert.NoError(t, err, "should resolve UDP address")
		n, err := conn.WriteTo([]byte(msg), udpAddr)
		assert.NoError(t, err, "should succeed")
		assert.Equal(t, len(msg), n, "should match")

		log.Debug("receiving packet")
		buf := make([]byte, 1000)
		n, remoteAddr, err := conn.ReadFrom(buf)
		assert.NoError(t, err, "should succeed")
		assert.Equal(t, len(msg), n, "should match")
		assert.Equal(t, msg, string(buf[:n]), "should match")
		assert.Equal(t, udpAddr.String(), remoteAddr.String(), "should match") //nolint:forcetypeassert

		log.Debug("closing connection")
		assert.NoError(t, conn.Close(), "should succeed") // should close baseConn
	})
}

// BenchmarkPortRangePacketConn sends lots of invalid packets: this is mostly for testing the logger
func BenchmarkPortRangePacketConn(b *testing.B) {
	loggerFactory := logger.NewLoggerFactory(connTestLoglevel)
	log := loggerFactory.NewLogger("test")
	relayLog := log

	log.Debug("creating vnet")
	nw, err := vnet.NewNet(&vnet.NetConfig{})
	if err != nil {
		b.Fatalf("Cannot allocate vnet: %s", err.Error())
	}

	tm, err := telemetry.New(telemetry.Callbacks{}, false, loggerFactory.NewLogger("metric"))
	assert.NoError(b, err, "should succeed")
	defer tm.Close() //nolint:errcheck

	log.Debug("creating base socket")
	addr := "127.0.0.1:25000"
	baseConn, err := nw.ListenPacket("udp4", addr)
	if err != nil {
		b.Fatalf("Cannot listen on vnet: %s", err.Error())
	}
	msg := "PING!"

	log.Debug("creating filtered packet conn wrappeer socket")
	conn := withCounter(NewPacketConn(baseConn, testCluster, telemetry.ClusterType, tm, getChecker(15000, 15000), relayLog))

	udpAddr, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		b.Fatalf("Cannot resove UDP addess: %s", err.Error())
	}

	// Run benchmark
	buffer := make([]byte, 1024)
	for j := 0; j < b.N; j++ {
		_, err := conn.WriteTo([]byte(msg), udpAddr)
		if err == nil {
			b.Fatal("Conn should reject write to invalid port")
		}

		// should never receive: we drop everything
		err = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
		if err != nil {
			b.Fatalf("Could not set read deadline: %s", err)
		}

		conn.ReadFrom(buffer) //nolint:errcheck
	}

	readCounter := conn.(*counterPacketConn).ReadCounter()
	if readCounter != 0 {
		b.Fatalf("Read counter (%d) should be 0", readCounter)
	}

	writeCounter := conn.(*counterPacketConn).WriteCounter()
	if writeCounter != 0 {
		b.Fatalf("Write counter (%d) should be %d", writeCounter, b.N)
	}

	log.Debug("closing connection")
	err = conn.Close()
	if err != nil {
		b.Fatalf("Cannot close connection: %s", err.Error())
	}
}

// counterPacketConn is a net.PacketConn that counts successful reads/writes.
type counterPacketConn struct {
	net.PacketConn
	readCounter, writeCounter int
}

func withCounter(c net.PacketConn) net.PacketConn {
	return &counterPacketConn{PacketConn: c}
}

func (c *counterPacketConn) WriteTo(p []byte, peerAddr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, peerAddr)
	if err == nil {
		c.writeCounter++
	}
	return n, err
}

func (c *counterPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if err == nil {
		c.readCounter++
	}
	return n, addr, err
}

func (c *counterPacketConn) ReadCounter() int  { return c.readCounter }
func (c *counterPacketConn) WriteCounter() int { return c.writeCounter }
