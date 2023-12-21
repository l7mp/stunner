package stunner

import (
	"net"
	"testing"
	"time"

	"github.com/pion/transport/v3/test"
	"github.com/pion/transport/v3/vnet"
	"github.com/stretchr/testify/assert"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/pkg/logger"
)

var connTestLoglevel string = "all:ERROR"

// var connTestLoglevel string = stnrv1.DefaultLogLevel
// var connTestLoglevel string = "all:INFO"
// var connTestLoglevel string = "all:TRACE"
// var connTestLoglevel string = "all:TRACE,vnet:INFO,turn:ERROR,turnc:ERROR"

var testCluster = object.Cluster{Name: "test-cluster"}

func getChecker(minPort, maxPort int) PortRangeChecker {
	return func(addr net.Addr) (*object.Cluster, bool) {
		u, ok := addr.(*net.UDPAddr)
		if !ok {
			return nil, false
		}

		return &testCluster, u.Port >= minPort && u.Port <= maxPort
	}
}

func TestPortRangePacketConn(t *testing.T) {
	telemetry.Init()
	defer telemetry.Close()

	lim := test.TimeOut(time.Second * 30)
	defer lim.Stop()

	report := test.CheckRoutines(t)
	defer report()

	loggerFactory := logger.NewLoggerFactory(connTestLoglevel)
	log := loggerFactory.NewLogger("test")

	log.Debug("Creating vnet")
	nw, err := vnet.NewNet(&vnet.NetConfig{})
	if !assert.NoError(t, err, "should succeed") {
		return
	}

	t.Run("LoopbackOnValidPort", func(t *testing.T) {
		log.Debug("Creating base socket")
		addr := "127.0.0.1:15000"
		baseConn, err := nw.ListenPacket("udp", addr)
		assert.NoError(t, err, "should succeed")
		msg := "PING!"

		log.Debug("Creating filtered packet conn wrappeer socket")
		conn := NewPortRangePacketConn(baseConn, getChecker(10000, 20000), log)
		assert.NoError(t, err, "should create port-range filtered packetconn")

		log.Debug("Sending packet")
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		assert.NoError(t, err, "should resolve UDP address")
		n, err := conn.WriteTo([]byte(msg), udpAddr)
		assert.NoError(t, err, "should succeed")
		assert.Equal(t, len(msg), n, "should match")

		log.Debug("Receiving packet")
		buf := make([]byte, 1000)
		n, remoteAddr, err := conn.ReadFrom(buf)
		assert.NoError(t, err, "should succeed")
		assert.Equal(t, len(msg), n, "should match")
		assert.Equal(t, msg, string(buf[:n]), "should match")
		assert.Equal(t, udpAddr.String(), remoteAddr.String(), "should match") //nolint:forcetypeassert

		log.Debug("Closing connection")
		assert.NoError(t, conn.Close(), "should succeed") // should close baseConn
	})

	t.Run("LoopbackOnInvalidPort", func(t *testing.T) {
		log.Debug("Creating base socket")
		addr := "127.0.0.1:25000"
		baseConn, err := nw.ListenPacket("udp", addr)
		assert.NoError(t, err, "should succeed")
		msg := "PING!"

		log.Debug("Creating filtered packet conn wrappeer socket")
		conn := NewPortRangePacketConn(baseConn, getChecker(10000, 20000), log)
		assert.NoError(t, err, "should create port-range filtered packetconn")

		log.Debug("Sending packet")
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		assert.NoError(t, err, "should resolve UDP address")
		n, err := conn.WriteTo([]byte(msg), udpAddr)
		assert.Error(t, err, "should reject")
		assert.Equal(t, 0, n, "should match")

		log.Debug("Receiving packet")
		buf := make([]byte, 1000)
		// this would hang otherwise
		assert.NoError(t, conn.SetReadDeadline(time.Now().Add(10*time.Millisecond)), "read deadline")
		_, _, err = conn.ReadFrom(buf)
		assert.Error(t, err, "should be rejected")

		log.Debug("Closing connection")
		assert.NoError(t, conn.Close(), "should succeed")
	})

	t.Run("LoopbackOnSinglePort", func(t *testing.T) {
		log.Debug("Creating base socket")
		addr := "127.0.0.1:15000"
		baseConn, err := nw.ListenPacket("udp", addr)
		assert.NoError(t, err, "should succeed")
		msg := "PING!"

		log.Debug("Creating filtered packet conn wrappeer socket")
		conn := NewPortRangePacketConn(baseConn, getChecker(15000, 15000), log)
		assert.NoError(t, err, "should create port-range filtered packetconn")

		log.Debug("Sending packet")
		udpAddr, err := net.ResolveUDPAddr("udp", addr)
		assert.NoError(t, err, "should resolve UDP address")
		n, err := conn.WriteTo([]byte(msg), udpAddr)
		assert.NoError(t, err, "should succeed")
		assert.Equal(t, len(msg), n, "should match")

		log.Debug("Receiving packet")
		buf := make([]byte, 1000)
		n, remoteAddr, err := conn.ReadFrom(buf)
		assert.NoError(t, err, "should succeed")
		assert.Equal(t, len(msg), n, "should match")
		assert.Equal(t, msg, string(buf[:n]), "should match")
		assert.Equal(t, udpAddr.String(), remoteAddr.String(), "should match") //nolint:forcetypeassert

		log.Debug("Closing connection")
		assert.NoError(t, conn.Close(), "should succeed") // should close baseConn
	})
}

// BenchmarkPortRangePacketConn sends lots of invalid packets: this is mostly for testing the logger
func BenchmarkPortRangePacketConn(b *testing.B) {
	telemetry.Init()
	defer telemetry.Close()

	loggerFactory := logger.NewLoggerFactory(connTestLoglevel)
	log := loggerFactory.NewLogger("test")
	//	relayLog := loggerFactory.WithRateLimiter(.25, 1).NewLogger("relay")
	relayLog := log

	log.Debug("Creating vnet")
	nw, err := vnet.NewNet(&vnet.NetConfig{})
	if err != nil {
		b.Fatalf("Cannot allocate vnet: %s", err.Error())
	}

	log.Debug("Creating base socket")
	addr := "127.0.0.1:25000"
	baseConn, err := nw.ListenPacket("udp", addr)
	if err != nil {
		b.Fatalf("Cannot listen on vnet: %s", err.Error())
	}
	msg := "PING!"

	log.Debug("Creating filtered packet conn wrappeer socket")
	conn := WithCounter(NewPortRangePacketConn(baseConn, getChecker(15000, 15000), relayLog))
	if err != nil {
		b.Fatalf("Cannot create port-range packetconn: %s", err.Error())
	}

	udpAddr, err := net.ResolveUDPAddr("udp", addr)
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

	readCounter := conn.(*CounterPacketConn).ReadCounter()
	if readCounter != 0 {
		b.Fatalf("Read counter (%d) should be 0", readCounter)
	}

	writeCounter := conn.(*CounterPacketConn).WriteCounter()
	if writeCounter != 0 {
		b.Fatalf("Write counter (%d) should be %d", writeCounter, b.N)
	}

	log.Debug("Closing connection")
	err = conn.Close()
	if err != nil {
		b.Fatalf("Cannot close connection: %s", err.Error())
	}
}

// CounterPacketConn is a net.PacketConn that filters on the target port range.
type CounterPacketConn struct {
	net.PacketConn
	readCounter, writeCounter int
}

// WithCounter decorates a PacketConn with a counter.
func WithCounter(c net.PacketConn) net.PacketConn {
	return &CounterPacketConn{
		PacketConn: c,
	}
}

// WriteTo writes to the PacketConn.
func (c *CounterPacketConn) WriteTo(p []byte, peerAddr net.Addr) (int, error) {
	n, err := c.PacketConn.WriteTo(p, peerAddr)
	if err == nil {
		c.writeCounter++
	}
	return n, err
}

// ReadFrom reads from the CounterPacketConn.
func (c *CounterPacketConn) ReadFrom(p []byte) (int, net.Addr, error) {
	n, addr, err := c.PacketConn.ReadFrom(p)
	if err == nil {
		c.readCounter++
	}
	return n, addr, err
}

func (c *CounterPacketConn) ReadCounter() int  { return c.readCounter }
func (c *CounterPacketConn) WriteCounter() int { return c.writeCounter }
