package stunner

import (
	"strings"
	"strconv"
	"syscall"
	"fmt"
	"net"
	"net/url"

	"github.com/pion/logging"
)

// StunnerUri is the specification of a STUNner listener URI
type StunnerUri struct {
	Protocol, Address, Username, Password string
	Port int
	Addr net.Addr 
}

// NewLoggerFactory sets up a scoped logger for STUNner
func NewLoggerFactory(levelSpec string) *logging.DefaultLoggerFactory{
	logger := logging.NewDefaultLoggerFactory()

	logLevels := map[string]logging.LogLevel{
		"DISABLE": logging.LogLevelDisabled,
		"ERROR":   logging.LogLevelError,
		"WARN":    logging.LogLevelWarn,
		"INFO":    logging.LogLevelInfo,
		"DEBUG":   logging.LogLevelDebug,
		"TRACE":   logging.LogLevelTrace,
	}

	levels := strings.Split(levelSpec, ",")
	for _, s := range levels {
		scopedLevel := strings.SplitN(s, ":", 2)
		if len(scopedLevel) != 2 {
			continue
		}
		scope := scopedLevel[0]
		level := scopedLevel[1]
		l, found := logLevels[strings.ToUpper(level)]
		if found == false {
			continue
		}

		if strings.ToLower(scope) == "all" {
			logger.DefaultLogLevel = l
			continue
		}

		logger.ScopeLevels[scope] = l
	}
	return logger
}

// ParseUri parses a STUN/TURN server URI, e.g., "turn://user1:passwd1@127.0.0.1:3478?transport=udp"
func ParseUri(uri string) (*StunnerUri, error){
	s := StunnerUri{}
	
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
	case "udp", "udp4", "udp6":
		a, err := net.ResolveUDPAddr("udp", s.Address + ":" + u.Port())
		if err != nil {
			return nil, err
		}
		s.Addr = a
	case "tcp", "tcp4", "tcp6":
		a, err := net.ResolveTCPAddr("tcp", s.Address + ":" + u.Port())
		if err != nil {
			return nil, err
		}
		s.Addr = a
	case "ip":
		a, err := net.ResolveIPAddr("ip", s.Address + ":" + u.Port())
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

func reuseAddr(network, address string, conn syscall.RawConn) error {
	return conn.Control(func(descriptor uintptr) {
		syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		// syscall.SetsockoptInt(int(descriptor), syscall.SOL_SOCKET, syscall.SO_REUSEPORT, 1)
	})
}

