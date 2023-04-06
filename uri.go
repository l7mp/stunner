package stunner

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/l7mp/stunner/internal/util"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// StunnerUri is the specification of a STUNner listener URI
type StunnerUri struct {
	Protocol, Address, Username, Password string
	Port                                  int
	Addr                                  net.Addr
}

// ParseUri parses a STUN/TURN server URI, e.g., "turn://user1:passwd1@127.0.0.1:3478?transport=udp"
func ParseUri(uri string) (*StunnerUri, error) {
	s := StunnerUri{}

	// handle stdin/out
	if uri == "-" || uri == "file://-" {
		s.Protocol = "file"
		// make turncat conf happy
		s.Port = 1
		s.Addr = &util.FileConnAddr{File: os.Stdin}
		return &s, nil
	}

	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("Invalid URI '%s': %s", uri, err)
	}

	s.Address = u.Hostname()
	s.Username = u.User.Username()
	password, found := u.User.Password()
	if found {
		s.Password = password
	}

	proto := strings.ToLower(u.Scheme)
	if proto == "turn" {
		q := u.Query()
		if len(q["transport"]) > 0 {
			proto = strings.ToLower(q["transport"][0])
		} else {
			proto = "udp"
		}
	}
	s.Protocol = proto

	port, _ := strconv.Atoi(u.Port())
	s.Port = port

	switch proto {
	case "udp", "udp4", "udp6", "dtls":
		a, err := net.ResolveUDPAddr("udp", s.Address+":"+u.Port())
		if err != nil {
			return nil, err
		}
		s.Addr = a
	case "tcp", "tcp4", "tcp6", "tls":
		a, err := net.ResolveTCPAddr("tcp", s.Address+":"+u.Port())
		if err != nil {
			return nil, err
		}
		s.Addr = a
	case "ip":
		a, err := net.ResolveIPAddr("ip", s.Address+":"+u.Port())
		if err != nil {
			return nil, err
		}
		s.Addr = a
	case "unix", "unixgram", "unixpacket":
		a, err := net.ResolveUnixAddr("unix", s.Address)
		if err != nil {
			return nil, err
		}
		s.Addr = a
	default:
		return nil, fmt.Errorf("invalid protocol: %s", proto)
	}

	return &s, nil
}

// GetUriFromListener returns a standard TURN URI from a listener config
func GetUriFromListener(req *v1alpha1.ListenerConfig) (string, error) {
	proto, err := v1alpha1.NewListenerProtocol(req.Protocol)
	if err != nil {
		return "", err
	}

	service, protocol := "", ""
	switch proto {
	case v1alpha1.ListenerProtocolUDP:
		service = "turn"
		protocol = "udp"
	case v1alpha1.ListenerProtocolTCP:
		service = "turn"
		protocol = "tcp"
	case v1alpha1.ListenerProtocolDTLS:
		service = "turns"
		protocol = "udp"
	case v1alpha1.ListenerProtocolTLS:
		service = "turns"
		protocol = "tcp"
	}

	addr := req.PublicAddr
	if addr == "" {
		// fallback to server addr
		addr = req.Addr
	}

	port := req.PublicPort
	if port == 0 {
		// fallback to server addr
		port = req.Port
	}

	uri := fmt.Sprintf("%s:%s:%d?transport=%s", service, addr, port, protocol)
	return uri, nil
}

// GetUriFromListener returns a standard TURN URI from a listener config
func GetTurnUris(req *v1alpha1.StunnerConfig) ([]string, error) {
	ret := []string{}
	for i := range req.Listeners {
		uri, err := GetUriFromListener(&req.Listeners[i])
		if err != nil {
			return []string{}, err
		}
		ret = append(ret, uri)
	}

	return ret, nil
}

func reuseAddr(network, address string, conn syscall.RawConn) error {
	return conn.Control(func(descriptor uintptr) {
		_ = syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		// syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
	})
}
