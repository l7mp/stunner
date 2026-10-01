//go:build linux

package stunner

import (
	"net"
	"testing"
	"time"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
	"github.com/l7mp/stunner/v2/pkg/logger"
)

const clientNum = 20

// multithreaded UDP tests
var TestStunnerConfigsMultithreadedUDP = []TestStunnerConfigCase{
	{
		config: stnrv2.StunnerConfig{
			// udp, plaintext
			ApiVersion: stnrv2.ApiVersion,
			Admin: stnrv2.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: stnrv2.AuthConfig{
				Type: "plaintext",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []stnrv2.ListenerConfig{{
				Name:       "udp",
				Protocol:   "UDP",
				Servers:    []string{"udp"},
				Addr:       "127.0.0.1",
				Port:       23478,
				PublicAddr: "1.2.3.4",
				PublicPort: 3478,
			}},
			Servers: []stnrv2.ServerConfig{{
				Name:     "udp",
				Type:     "turn",
				Clusters: []string{"allow-any"},
			}},
			Clusters: []stnrv2.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
				Protocol:  "UDP",
				Addrs:     []string{"127.0.0.1"},
			}},
		},
		uri: "turn:1.2.3.4:3478?transport=udp",
	},
}

func TestStunnerMultithreadedUDP(t *testing.T) {
	// the multithreaded UDP listener needs the reuse-port sockets of the OS network
	testStunnerLocalhost(t, TestStunnerConfigsMultithreadedUDP, true)
}

// Benchmark
func RunBenchmarkServer(b *testing.B, proto string) {
	//loggerFactory := logger.NewLoggerFactory("all:TRACE")
	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")
	initSeq := []byte("init-data")
	testSeq := []byte("benchmark-data")

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{
		LogOptions:       LogOptions{Level: stunnerTestLoglevel},
		SuppressRollback: true,
	})
	defer stunner.Close()

	log.Debug("starting stunnerd")
	err := stunner.Reconcile(&stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin: stnrv2.AdminConfig{
			LogLevel: stunnerTestLoglevel,
		},
		Auth: stnrv2.AuthConfig{
			Type: "plaintext",
			Credentials: map[string]string{
				"username": "user1",
				"password": "passwd1",
			},
		},
		Listeners: []stnrv2.ListenerConfig{{
			Name:     "default-listener",
			Protocol: proto,
			Servers:  []string{"default-server"},
			Addr:     "127.0.0.1",
			Port:     23478,
			Cert:     certPem64,
			Key:      keyPem64,
		}},
		Servers: []stnrv2.ServerConfig{{
			Name:     "default-server",
			Type:     "turn",
			Clusters: []string{"allow-any"},
		}},
		Clusters: []stnrv2.ClusterConfig{{
			Name:      "allow-any",
			Endpoints: []string{"0.0.0.0/0"},
			Protocol:  "UDP",
			Addrs:     []string{"127.0.0.1"},
		}},
	})

	if err != nil {
		b.Fatalf("Failed to start stunnerd: %s", err)
	}

	log.Debug("creating a sink")
	sinkAddr, err := net.ResolveUDPAddr("udp4", "0.0.0.0:65432")
	if err != nil {
		b.Fatalf("Failed to resolve sink address: %s", err)
	}

	sink, err := net.ListenPacket(sinkAddr.Network(), sinkAddr.String())
	if err != nil {
		b.Fatalf("Failed to allocate sink: %s", err)
	}
	defer sink.Close() //nolint:errcheck

	go func() {
		buf := make([]byte, 1600)
		for {
			// Ignore "use of closed network connection" errors
			if _, _, err := sink.ReadFrom(buf); err != nil {
				// b.Logf("Failed to receive packet at sink: %s", err)
				return
			}

			// Do not care about received data
		}
	}()

	log.Debug("creating a tunnel client")
	clientProto := "TCP"
	if proto == "UDP" || proto == "DTLS" {
		clientProto = "UDP"
	}
	tunnelProto, err := stnrv2.NewProtocol("TURN-" + proto)
	if err != nil {
		b.Fatalf("Invalid tunnel protocol: %s", err)
	}
	noHealthCheck := ""
	tunnel := NewStunner(Options{
		Name:             "tunnel",
		LogOptions:       LogOptions{Level: stunnerTestLoglevel},
		SuppressRollback: true,
	})
	defer tunnel.Close()
	if err := tunnel.Reconcile(&stnrv2.StunnerConfig{
		ApiVersion: stnrv2.ApiVersion,
		Admin: stnrv2.AdminConfig{
			LogLevel:            stunnerTestLoglevel,
			HealthCheckEndpoint: &noHealthCheck,
		},
		Auth: stnrv2.AuthConfig{Type: "none"},
		Listeners: []stnrv2.ListenerConfig{{
			Name:     "tunnel-listener",
			Protocol: clientProto,
			Servers:  []string{"tunnel"},
			Addr:     "127.0.0.1",
			Port:     25000,
		}},
		Servers: []stnrv2.ServerConfig{{
			Name:     "tunnel",
			Type:     "l4",
			Clusters: []string{"sink"},
		}},
		Clusters: []stnrv2.ClusterConfig{{
			Name:          "sink",
			Endpoints:     []string{"127.0.0.1:65432"},
			RoutingPolicy: "ROUND_ROBIN",
			Protocol:      "UDP",
			Tunnel: &stnrv2.TunnelConfig{
				URL: (&stnrv2.URI{Protocol: tunnelProto, Host: "127.0.0.1", Port: 23478}).String(),
				Auth: &stnrv2.AuthConfig{Type: "static", Credentials: map[string]string{
					"username": "user1", "password": "passwd1"}},
				Insecure: true,
			},
		}},
	}); err != nil {
		b.Fatalf("Failed to create tunnel client: %s", err)
	}

	// test with 20 clients
	log.Debugf("creating %d senders", clientNum)
	clients := make([]net.Conn, clientNum)
	for i := 0; i < clientNum; i++ {
		var client net.Conn
		var err error
		if clientProto == "UDP" {
			tunnelAddr, _ := net.ResolveUDPAddr("udp4", "127.0.0.1:25000") //nolint:errcheck
			client, err = net.DialUDP("udp", nil, tunnelAddr)
		} else {
			tunnelAddr, _ := net.ResolveTCPAddr("tcp4", "127.0.0.1:25000") //nolint:errcheck
			client, err = net.DialTCP("tcp", nil, tunnelAddr)
		}
		if err != nil {
			b.Fatalf("Failed to allocate client socket: %s", err)
		}
		clients[i] = client
	}

	// Kick the tunnel so that it creates the flows and upstream allocations for us
	for i := 0; i < clientNum; i++ {
		if _, err := clients[i].Write(initSeq); err != nil {
			b.Fatalf("Client %d create allocation via the tunnel: %s", i, err)
		}
	}

	time.Sleep(150 * time.Millisecond)

	// Run benchmark
	for j := 0; j < b.N; j++ {
		for i := 0; i < clientNum; i++ {
			if _, err := clients[i].Write(testSeq); err != nil {
				b.Fatalf("Client %d cannot send to the tunnel: %s", i, err)
			}
		}
	}

	time.Sleep(750 * time.Millisecond)

	for i := 0; i < clientNum; i++ {
		clients[i].Close() //nolint:errcheck
	}
}

// BenchmarkUDPServer will benchmark the STUNner UDP server. Setup: `client --udp--> tunnel
// --udp--> stunner --udp--> sink`
func BenchmarkUDPServer(b *testing.B) {
	b.Run("udp", func(b *testing.B) {
		RunBenchmarkServer(b, "UDP")
	})
}

// BenchmarkTCPServer will benchmark the STUNner TCP server with a different number of readloop
// threads. Setup: `client --tcp--> tunnel --tcp--> stunner --udp--> sink`
func BenchmarkTCPServer(b *testing.B) {
	b.Run("tcp", func(b *testing.B) {
		RunBenchmarkServer(b, "TCP")
	})
}

// BenchmarkTLSServer will benchmark the STUNner TLS server with a different number of readloop
// threads. Setup: `client --tcp--> tunnel --tls--> stunner --udp--> sink`
func BenchmarkTLSServer(b *testing.B) {
	b.Run("tls", func(b *testing.B) {
		RunBenchmarkServer(b, "TLS")
	})
}

// BenchmarkDTLSServer will benchmark the STUNner DTLS server with a different number of readloop
// threads. Setup: `client --udp--> tunnel --dtls--> stunner --udp--> sink`
func BenchmarkDTLSServer(b *testing.B) {
	b.Run("dtls", func(b *testing.B) {
		RunBenchmarkServer(b, "DTLS")
	})
}
