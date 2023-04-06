//go:build linux

package stunner

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"

	"github.com/l7mp/stunner/pkg/logger"
)

const clientNum = 20

// multithreaded UDP tests
var TestStunnerConfigsMultithreadedUDP = []TestStunnerConfigCase{
	{
		config: v1alpha1.StunnerConfig{
			// udp, plaintext
			ApiVersion: "v1alpha1",
			Admin: v1alpha1.AdminConfig{
				LogLevel: stunnerTestLoglevel,
			},
			Auth: v1alpha1.AuthConfig{
				Type: "plaintext",
				Credentials: map[string]string{
					"username": "user1",
					"password": "passwd1",
				},
			},
			Listeners: []v1alpha1.ListenerConfig{{
				Name:       "udp",
				Protocol:   "udp",
				Addr:       "127.0.0.1",
				Port:       23478,
				PublicAddr: "1.2.3.4",
				PublicPort: 3478,
				Routes:     []string{"allow-any"},
			}},
			Clusters: []v1alpha1.ClusterConfig{{
				Name:      "allow-any",
				Endpoints: []string{"0.0.0.0/0"},
			}},
		},
		uri: "turn:1.2.3.4:3478?transport=udp",
	},
}

func TestStunnerMultithreadedUDP(t *testing.T) {
	testStunnerLocalhost(t, 4, TestStunnerConfigsMultithreadedUDP)
}

// Benchmark
func RunBenchmarkServer(b *testing.B, proto string, udpThreadNum int) {
	// loggerFactory := logger.NewLoggerFactory("all:TRACE")
	loggerFactory := logger.NewLoggerFactory(stunnerTestLoglevel)
	log := loggerFactory.NewLogger("test")
	initSeq := []byte("init-data")
	testSeq := []byte("benchmark-data")

	log.Debug("creating a stunnerd")
	stunner := NewStunner(Options{
		LogLevel:             stunnerTestLoglevel,
		SuppressRollback:     true,
		UDPListenerThreadNum: udpThreadNum, // ignored for anything but UDP
	})

	log.Debug("starting stunnerd")
	err := stunner.Reconcile(v1alpha1.StunnerConfig{
		ApiVersion: "v1alpha1",
		Admin: v1alpha1.AdminConfig{
			LogLevel: stunnerTestLoglevel,
		},
		Auth: v1alpha1.AuthConfig{
			Type: "plaintext",
			Credentials: map[string]string{
				"username": "user1",
				"password": "passwd1",
			},
		},
		Listeners: []v1alpha1.ListenerConfig{{
			Name:     "default-listener",
			Protocol: proto,
			Addr:     "127.0.0.1",
			Port:     23478,
			Cert:     certPem64,
			Key:      keyPem64,
			Routes:   []string{"allow-any"},
		}},
		Clusters: []v1alpha1.ClusterConfig{{
			Name:      "allow-any",
			Endpoints: []string{"0.0.0.0/0"},
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

	log.Debug("creating a turncat client")
	stunnerURI := fmt.Sprintf("turn://127.0.0.1:23478?transport=%s", proto)
	clientProto := "tcp"
	if proto == "udp" || proto == "dtls" {
		clientProto = "udp"
	}
	testTurncatConfig := TurncatConfig{
		ListenerAddr:  fmt.Sprintf("%s://127.0.0.1:25000", clientProto),
		ServerAddr:    stunnerURI,
		PeerAddr:      "udp://localhost:65432",
		AuthGen:       plaintextAuthGen,
		LoggerFactory: loggerFactory,
		InsecureMode:  true,
	}

	turncat, err := NewTurncat(&testTurncatConfig)
	if err != nil {
		b.Fatalf("Failed to create turncat client: %s", err)
	}

	// test with 20 clients
	log.Debugf("creating %d senders", clientNum)
	clients := make([]net.Conn, clientNum)
	for i := 0; i < clientNum; i++ {
		var client net.Conn
		var err error
		if clientProto == "udp" {
			turncatAddr, _ := net.ResolveUDPAddr("udp", "127.0.0.1:25000") //nolint:errcheck
			client, err = net.DialUDP("udp", nil, turncatAddr)
		} else {
			turncatAddr, _ := net.ResolveTCPAddr("tcp", "127.0.0.1:25000") //nolint:errcheck
			client, err = net.DialTCP("tcp", nil, turncatAddr)
		}
		if err != nil {
			b.Fatalf("Failed to allocate client socket: %s", err)
		}
		clients[i] = client
	}

	// Kick turncat so that it creates the allocation for us
	for i := 0; i < clientNum; i++ {
		if _, err := clients[i].Write(initSeq); err != nil {
			b.Fatalf("Client %d create allocation via turncat: %s", i, err)
		}
	}

	time.Sleep(150 * time.Millisecond)

	// Run benchmark
	for j := 0; j < b.N; j++ {
		for i := 0; i < clientNum; i++ {
			if _, err := clients[i].Write(testSeq); err != nil {
				b.Fatalf("Client %d cannot send to turncat: %s", i, err)
			}
		}
	}

	time.Sleep(750 * time.Millisecond)

	turncat.Close()
	stunner.Close()
	for i := 0; i < clientNum; i++ {
		clients[i].Close()
	}
	sink.Close() //nolint:errcheck
}

// BenchmarkUDPServer will benchmark the STUNner UDP server with a different number of readloop
// threads. Setup: `client --udp--> turncat --udp--> stunner --udp--> sink`
func BenchmarkUDPServer(b *testing.B) {
	for i := 1; i <= 4; i++ {
		b.Run(fmt.Sprintf("udp:thread_num=%d", i), func(b *testing.B) {
			RunBenchmarkServer(b, "udp", i)
		})
	}
}

// BenchmarkTCPServer will benchmark the STUNner TCP server with a different number of readloop
// threads. Setup: `client --tcp--> turncat --tcp--> stunner --udp--> sink`
func BenchmarkTCPServer(b *testing.B) {
	b.Run("tcp", func(b *testing.B) {
		RunBenchmarkServer(b, "tcp", 0)
	})
}

// BenchmarkTLSServer will benchmark the STUNner TLS server with a different number of readloop
// threads. Setup: `client --tcp--> turncat --tls--> stunner --udp--> sink`
func BenchmarkTLSServer(b *testing.B) {
	b.Run("tls", func(b *testing.B) {
		RunBenchmarkServer(b, "tls", 0)
	})
}

// BenchmarkDTLSServer will benchmark the STUNner DTLS server with a different number of readloop
// threads. Setup: `client --udp--> turncat --dtls--> stunner --udp--> sink`
func BenchmarkDTLSServer(b *testing.B) {
	b.Run("dtls", func(b *testing.B) {
		RunBenchmarkServer(b, "dtls", 0)
	})
}
