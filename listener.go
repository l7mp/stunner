package stunner

import (
	"net"
	"fmt"
	"strings"
	"crypto/tls"

	"github.com/pion/logging"
	"github.com/pion/turn/v2"
	"github.com/pion/dtls/v2"
	"github.com/google/go-cmp/cmp"
)

// listenerProtocol
type ListenerProtocol int

const (
	ListenerProtocolUdp ListenerProtocol = iota + 1
	ListenerProtocolTcp
	ListenerProtocolTls
	ListenerProtocolDtls
	ListenerProtocolUnknown
)

const (
	listenerProtocolUdpStr  = "udp"
	listenerProtocolTcpStr  = "tcp"
	listenerProtocolTlsStr  = "tls"
	listenerProtocolDtlsStr = "dtls"
)

func NewListenerProtocol(raw string) (ListenerProtocol, error) {
	switch strings.ToLower(raw) {
	case listenerProtocolUdpStr:
		return ListenerProtocolUdp, nil
	case listenerProtocolTcpStr:
		return ListenerProtocolTcp, nil
	case listenerProtocolTlsStr:
		return ListenerProtocolTls, nil
	case listenerProtocolDtlsStr:
		return ListenerProtocolDtls, nil
	default:
		return ListenerProtocol(ListenerProtocolUnknown), fmt.Errorf("unknown listener protocol: %s", raw)
	}
}

func (l ListenerProtocol) String() string {
	switch l {
	case ListenerProtocolUdp:
		return listenerProtocolUdpStr
	case ListenerProtocolTcp:
		return listenerProtocolTcpStr
	case ListenerProtocolTls:
		return listenerProtocolTlsStr
	case ListenerProtocolDtls:
		return listenerProtocolDtlsStr
	default:
		return "<unknown>"
	}
}

// Authenticator
type listener struct {
	name string
	proto ListenerProtocol
	addr net.IP
	log logging.LeveledLogger
	port, minPort, maxPort int
	cert, key, rawAddr string  // net.IP.String() may rewrite the string representation
	conn interface{}  // either turn.ListenerConfig or turn.PacketConnConfig
}

func (s *Stunner) newListener(req ListenerConfig) (*listener, error) {
	l := listener { log: s.logger.NewLogger("stunner-listener") }

	proto, err := NewListenerProtocol(req.Protocol)
	if err != nil {
		return nil, err
	}
	l.proto = proto

	if req.Addr == "" {req.Addr = "0.0.0.0"}
	l.addr = net.ParseIP(req.Addr)
	if l.addr == nil {
		return nil, fmt.Errorf("invalid listener address: %s", req.Addr)
	}
	l.rawAddr = req.Addr
	
	if req.Port == 0 {req.Port = DefaultPort }
	if req.MinRelayPort == 0 {req.MinRelayPort = DefaultMinRelayPort }
	if req.MaxRelayPort == 0 {req.MaxRelayPort = DefaultMaxRelayPort }
	for _, p := range []int{req.Port, req.MinRelayPort, req.MaxRelayPort} {
		if p <=0 || p > 65535 {
			return nil, fmt.Errorf("invalid port: %d", p)
		}
	}
	l.port, l.minPort, l.maxPort = req.Port, req.MinRelayPort, req.MaxRelayPort
	
	relay := &turn.RelayAddressGeneratorPortRange{
		RelayAddress: l.addr,
		Address:      l.addr.String(),
		MinPort:      uint16(l.minPort),
		MaxPort:      uint16(l.maxPort),
		Net:          s.net,
	}
	
	addr := fmt.Sprintf("%s:%d", l.addr.String(), l.port)
	switch l.proto {
	case ListenerProtocolUdp:
		l.log.Tracef("setting up UDP listener at %s", addr)
		udpListener, err := s.net.ListenPacket("udp", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to create UDP listener at %s: %s",
				addr, err)
		}
		
		l.conn = turn.PacketConnConfig{
			PacketConn: udpListener,
			RelayAddressGenerator: relay,
		}
		
	// cannot test this on vnet, no Listen/ListenTCP in vnet.Net
	case ListenerProtocolTcp:
		l.log.Tracef("setting up TCP listener at %s", addr)
		tcpListener, err := net.Listen("tcp", addr)
		if err != nil {
			return nil, fmt.Errorf("failed to create TCP listener at %s: %s", addr, err)
		}
		l.conn = turn.ListenerConfig{
			Listener: tcpListener,
			RelayAddressGenerator: relay,
		}

	// cannot test this on vnet, no TLS in vnet.Net
	case ListenerProtocolTls:
		l.log.Tracef("setting up TLS/TCP listener at %s", addr)
		cer, errTls := tls.LoadX509KeyPair(req.Cert, req.Key)
		if errTls != nil {
			return nil, fmt.Errorf("cannot load cert/key pair for creating TLS listener at %s: %s",
				addr, errTls)
		}
		l.cert = req.Cert
		l.key = req.Key
		
		tlsListener, err := tls.Listen("tcp", addr, &tls.Config{
			MinVersion:   tls.VersionTLS12,
			Certificates: []tls.Certificate{cer},
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create TLS listener at %s: %s", addr, err)
		}
		l.conn = turn.ListenerConfig{
			Listener: tlsListener,
			RelayAddressGenerator: relay,
		}
		
	// cannot test this on vnet, no DTLS in vnet.Net
	case ListenerProtocolDtls:
		l.log.Tracef("setting up DTLS/UDP listener at %s", addr)

		cer, errTls := tls.LoadX509KeyPair(req.Cert, req.Key)
		if errTls != nil {
			return nil, fmt.Errorf("cannot load cert/ley pair for creating DTLS listener at %s: %s",
				addr, errTls)
		}
		l.cert = req.Cert
		l.key = req.Key

		// for some reason dtls.Listen requires a UDPAddr and not an addr string
		udpAddr := &net.UDPAddr{IP: l.addr, Port: l.port}
		dtlsListener, err := dtls.Listen("udp", udpAddr, &dtls.Config{
			Certificates:         []tls.Certificate{cer},
			// ExtendedMasterSecret: dtls.RequireExtendedMasterSecret,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to create DTLS listener at %s: %s", addr, err)
		}

		l.conn = turn.ListenerConfig{
			Listener: dtlsListener,
			RelayAddressGenerator: relay,
		}

	default:
		panic("internal error: unknown listener protocol " + l.proto.String())
	}

	if req.Name == "" {l.name = "listener-" + l.String()}
	
	return &l, nil
}

func (l *listener) String() string {
	uri := fmt.Sprintf("%s://%s:%d [%d:%d]", l.proto, l.addr, l.port, l.minPort, l.maxPort)
	if l.cert != "" && l.key != "" { uri += " (cert/key)" }
	return uri
}

func (l *listener) getConfig() ListenerConfig {
	return ListenerConfig{
		Protocol: l.proto.String(),
		Addr: l.rawAddr,
		Port: l.port,
		MinRelayPort: l.minPort,
		MaxRelayPort: l.maxPort,
		Cert: l.cert,
		Key: l.key,
	}
}

// Compare two StunnerConfigs: listener's position in the listener list does not matter
// disambiguate by listener's string representation
func transformListenerList(in []ListenerConfig) map[string]ListenerConfig {
	out := make(map[string]ListenerConfig)
	for _, l := range in {
		out[l.String()] = l
	}
	return out
}

var listenerListTransformer = cmp.Transformer("ListenerList", transformListenerList)
