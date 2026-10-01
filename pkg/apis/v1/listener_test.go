package v1

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewListenerProtocol(t *testing.T) {
	valid := map[string]Protocol{
		"udp":       ProtocolUDP,
		"UDP":       ProtocolUDP,
		"tcp":       ProtocolTCP,
		"tls":       ProtocolTLS,
		"dtls":      ProtocolDTLS,
		"stdin":     ProtocolSTDIN,
		"STDIN":     ProtocolSTDIN,
		"turn-udp":  ProtocolTURNUDP,
		"TURN-UDP":  ProtocolTURNUDP,
		"turn-tcp":  ProtocolTURNTCP,
		"turn-tls":  ProtocolTURNTLS,
		"turn-dtls": ProtocolTURNDTLS,
	}
	for raw, want := range valid {
		p, err := NewListenerProtocol(raw)
		assert.NoError(t, err, "listener protocol %q", raw)
		assert.Equal(t, want, p, "listener protocol %q", raw)
	}

	for _, raw := range []string{"", "dummy", "unix", "ip", "file"} {
		_, err := NewListenerProtocol(raw)
		assert.Error(t, err, "invalid listener protocol %q", raw)
	}
}

func TestListenerConfigValidate(t *testing.T) {
	testCases := []struct {
		name string
		conf ListenerConfig
		err  bool
	}{
		{
			name: "default listener",
			conf: ListenerConfig{Name: "listener"},
		},
		{
			name: "turn-udp listener",
			conf: ListenerConfig{Name: "listener", Protocol: "turn-udp"},
		},
		{
			name: "missing name",
			conf: ListenerConfig{Protocol: "turn-udp"},
			err:  true,
		},
		{
			name: "invalid protocol",
			conf: ListenerConfig{Name: "listener", Protocol: "dummy"},
			err:  true,
		},
		{
			name: "turn-tls listener without cert",
			conf: ListenerConfig{Name: "listener", Protocol: "turn-tls", Key: "key"},
			err:  true,
		},
		{
			name: "turn-tls listener without key",
			conf: ListenerConfig{Name: "listener", Protocol: "turn-tls", Cert: "cert"},
			err:  true,
		},
		{
			name: "plain udp listener",
			conf: ListenerConfig{Name: "listener", Protocol: "udp"},
		},
		{
			name: "plain tcp listener",
			conf: ListenerConfig{Name: "listener", Protocol: "tcp"},
		},
		{
			name: "stdin listener",
			conf: ListenerConfig{Name: "listener", Protocol: "stdin"},
		},
		{
			name: "stdin listener with an address",
			conf: ListenerConfig{Name: "listener", Protocol: "stdin",
				Addr: "127.0.0.1"},
			err: true,
		},
		{
			name: "stdin listener with a port",
			conf: ListenerConfig{Name: "listener", Protocol: "stdin",
				Port: 3478},
			err: true,
		},
		{
			name: "stdin listener with TLS credentials",
			conf: ListenerConfig{Name: "listener", Protocol: "stdin",
				Cert: "cert", Key: "key"},
			err: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.conf.Validate()
			if tc.err {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
		})
	}
}

func TestListenerConfigValidateDefaults(t *testing.T) {
	c := ListenerConfig{Name: "listener"}
	assert.NoError(t, c.Validate())
	assert.Equal(t, "TURN-UDP", c.Protocol, "protocol default")
	assert.Equal(t, "0.0.0.0", c.Addr, "address default")
	assert.Equal(t, DefaultPort, c.Port, "port default")

	// STDIN listeners have no listener socket: no address/port defaulting
	s := ListenerConfig{Name: "listener", Protocol: "stdin"}
	assert.NoError(t, s.Validate())
	assert.Equal(t, "STDIN", s.Protocol, "protocol normalized")
	assert.Empty(t, s.Addr, "no address default for stdin")
	assert.Zero(t, s.Port, "no port default for stdin")
}
