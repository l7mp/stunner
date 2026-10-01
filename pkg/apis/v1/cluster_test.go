package v1

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClusterProtocol(t *testing.T) {
	valid := map[string]Protocol{
		"udp":       ProtocolUDP,
		"UDP":       ProtocolUDP,
		"tcp":       ProtocolTCP,
		"turn-udp":  ProtocolTURNUDP,
		"TURN-UDP":  ProtocolTURNUDP,
		"turn-tcp":  ProtocolTURNTCP,
		"turn-tls":  ProtocolTURNTLS,
		"turn-dtls": ProtocolTURNDTLS,
	}
	for raw, want := range valid {
		p, err := NewClusterProtocol(raw)
		assert.NoError(t, err, "cluster protocol %q", raw)
		assert.Equal(t, want, p, "cluster protocol %q", raw)
	}

	for _, raw := range []string{"", "dummy", "tls", "dtls", "unix", "ip"} {
		_, err := NewClusterProtocol(raw)
		assert.Error(t, err, "invalid cluster protocol %q", raw)
	}
}

func TestProtocolIsTURN(t *testing.T) {
	assert.True(t, ProtocolTURNUDP.IsTURN(), "turn-udp")
	assert.True(t, ProtocolTURNTCP.IsTURN(), "turn-tcp")
	assert.True(t, ProtocolTURNTLS.IsTURN(), "turn-tls")
	assert.True(t, ProtocolTURNDTLS.IsTURN(), "turn-dtls")
	assert.False(t, ProtocolUDP.IsTURN(), "udp")
	assert.False(t, ProtocolTCP.IsTURN(), "tcp")
	assert.False(t, ProtocolUnknown.IsTURN(), "unknown")
}

func TestClusterConfigValidate(t *testing.T) {
	testCases := []struct {
		name string
		conf ClusterConfig
		err  bool
	}{
		{
			name: "default cluster",
			conf: ClusterConfig{Name: "cluster"},
		},
		{
			name: "static udp cluster",
			conf: ClusterConfig{
				Name:      "cluster",
				Type:      "STATIC",
				Protocol:  "udp",
				Endpoints: []string{"10.0.0.0/8"},
			},
		},
		{
			name: "missing name",
			conf: ClusterConfig{},
			err:  true,
		},
		{
			name: "invalid protocol",
			conf: ClusterConfig{Name: "cluster", Protocol: "dummy"},
			err:  true,
		},
		{
			name: "turn cluster",
			conf: ClusterConfig{
				Name:       "cluster",
				Protocol:   "turn-udp",
				TURNServer: &TURNServer{Address: "turn.example.com", Port: 3478},
			},
		},
		{
			name: "turn cluster with endpoints",
			conf: ClusterConfig{
				Name:       "cluster",
				Protocol:   "turn-udp",
				Endpoints:  []string{"10.0.0.1:<5000-5000>"},
				TURNServer: &TURNServer{Address: "turn.example.com", Port: 3478},
			},
		},
		{
			name: "turn cluster with credentials",
			conf: ClusterConfig{
				Name:     "cluster",
				Protocol: "turn-tls",
				TURNServer: &TURNServer{
					Address: "turn.example.com",
					Port:    5349,
					Auth: &AuthConfig{Type: "static", Credentials: map[string]string{
						"username": "user1", "password": "pass1"}},
				},
			},
		},
		{
			name: "turn cluster without server",
			conf: ClusterConfig{Name: "cluster", Protocol: "turn-udp"},
			err:  true,
		},
		{
			name: "turn cluster with empty server address",
			conf: ClusterConfig{
				Name:       "cluster",
				Protocol:   "turn-tcp",
				TURNServer: &TURNServer{Port: 3478},
			},
			err: true,
		},
		{
			name: "turn cluster with missing server port",
			conf: ClusterConfig{
				Name:       "cluster",
				Protocol:   "turn-udp",
				TURNServer: &TURNServer{Address: "turn.example.com"},
			},
			err: true,
		},
		{
			name: "turn cluster with invalid server port",
			conf: ClusterConfig{
				Name:       "cluster",
				Protocol:   "turn-udp",
				TURNServer: &TURNServer{Address: "turn.example.com", Port: 100000},
			},
			err: true,
		},
		{
			name: "turn cluster with username only",
			conf: ClusterConfig{
				Name:     "cluster",
				Protocol: "turn-udp",
				TURNServer: &TURNServer{
					Address: "turn.example.com",
					Port:    3478,
					Auth: &AuthConfig{Type: "static", Credentials: map[string]string{
						"username": "user1"}},
				},
			},
			err: false,
		},
		{
			name: "turn cluster with password only",
			conf: ClusterConfig{
				Name:     "cluster",
				Protocol: "turn-udp",
				TURNServer: &TURNServer{
					Address: "turn.example.com",
					Port:    3478,
					Auth: &AuthConfig{Type: "static", Credentials: map[string]string{
						"password": "pass1"}},
				},
			},
			err: false,
		},
		{
			name: "direct cluster with turn server",
			conf: ClusterConfig{
				Name:       "cluster",
				Protocol:   "udp",
				TURNServer: &TURNServer{Address: "turn.example.com", Port: 3478},
			},
			err: true,
		},
		{
			name: "turn-tls cluster with SNI",
			conf: ClusterConfig{
				Name:     "cluster",
				Protocol: "turn-tls",
				TURNServer: &TURNServer{Address: "1.2.3.4", Port: 5349,
					SNI: "turn.example.com"},
			},
		},
		{
			name: "turn-dtls cluster with SNI",
			conf: ClusterConfig{
				Name:     "cluster",
				Protocol: "turn-dtls",
				TURNServer: &TURNServer{Address: "1.2.3.4", Port: 5349,
					SNI: "turn.example.com"},
			},
		},
		{
			name: "turn-udp cluster with SNI",
			conf: ClusterConfig{
				Name:     "cluster",
				Protocol: "turn-udp",
				TURNServer: &TURNServer{Address: "1.2.3.4", Port: 3478,
					SNI: "turn.example.com"},
			},
			err: true,
		},
		{
			name: "turn-tcp cluster with SNI",
			conf: ClusterConfig{
				Name:     "cluster",
				Protocol: "turn-tcp",
				TURNServer: &TURNServer{Address: "1.2.3.4", Port: 3478,
					SNI: "turn.example.com"},
			},
			err: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			conf := tc.conf
			err := conf.Validate()
			if tc.err {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)

			// defaults are filled in
			assert.NotEmpty(t, conf.Type, "type default")
			assert.NotEmpty(t, conf.Protocol, "protocol default")

			// protocol is normalized
			p, err := NewClusterProtocol(conf.Protocol)
			require.NoError(t, err)
			assert.Equal(t, p.String(), conf.Protocol, "protocol normalized")
		})
	}
}

func TestClusterConfigDeepCopyDeepEqual(t *testing.T) {
	conf := ClusterConfig{
		Name:     "cluster",
		Protocol: "turn-udp",
		TURNServer: &TURNServer{
			Address: "turn.example.com",
			Port:    3478,
			Auth: &AuthConfig{Type: "static", Credentials: map[string]string{
				"username": "user1", "password": "pass1"}},
		},
	}
	require.NoError(t, conf.Validate())

	copied := ClusterConfig{}
	conf.DeepCopyInto(&copied)
	assert.True(t, conf.DeepEqual(&copied), "deep copy is deep equal")

	// the TURN server is copied, not aliased, down to the credential map
	copied.TURNServer.Auth.Credentials["password"] = "changed"
	assert.Equal(t, "pass1", conf.TURNServer.Auth.Credentials["password"],
		"credential map not aliased")
	assert.False(t, conf.DeepEqual(&copied), "credential change detected")
}

func TestClusterConfigStringRedactsPassword(t *testing.T) {
	conf := ClusterConfig{
		Name:     "cluster",
		Protocol: "turn-udp",
		TURNServer: &TURNServer{
			Address: "turn.example.com",
			Port:    3478,
			Auth: &AuthConfig{Type: "static", Credentials: map[string]string{
				"username": "user1", "password": "supersecret"}},
		},
	}

	s := conf.String()
	assert.NotContains(t, s, "supersecret", "password redacted")
	assert.NotContains(t, s, "user1", "username redacted")
	assert.Contains(t, s, "turn.example.com:3478", "server address printed")
	assert.Contains(t, s, "static-auth", "auth type printed")

	// an ephemeral secret is redacted too
	conf.TURNServer.Auth = &AuthConfig{Type: "ephemeral",
		Credentials: map[string]string{"secret": "topsecret"}}
	s = conf.String()
	assert.NotContains(t, s, "topsecret", "shared secret redacted")
	assert.Contains(t, s, "ephemeral-auth", "auth type printed")
}

func TestTURNServerHostPort(t *testing.T) {
	for _, c := range []struct {
		name   string
		server TURNServer
		want   string
	}{
		{"ipv4", TURNServer{Address: "1.2.3.4", Port: 3478}, "1.2.3.4:3478"},
		{"ipv6 gets bracketed", TURNServer{Address: "2001:db8::1", Port: 3478}, "[2001:db8::1]:3478"},
		{"ipv6 loopback", TURNServer{Address: "::1", Port: 5349}, "[::1]:5349"},
		{"dns name", TURNServer{Address: "turn.example.com", Port: 443}, "turn.example.com:443"},
	} {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, c.server.HostPort())
		})
	}
}

func TestTURNServerValidate(t *testing.T) {
	static := func(u, p string) *AuthConfig {
		return &AuthConfig{Type: "static", Credentials: map[string]string{
			"username": u, "password": p}}
	}

	for _, c := range []struct {
		name     string
		server   TURNServer
		err      bool
		wantType string
	}{
		{
			name: "an omitted auth block means no credentials",
			// nil, not a defaulted-into-static block that would then demand a password
			server:   TURNServer{Address: "1.2.3.4", Port: 3478},
			wantType: "",
		},
		{
			name: "an explicit none stays none",
			// "none" turns off authentication deliberately: it must never be defaulted
			server:   TURNServer{Address: "1.2.3.4", Port: 3478, Auth: &AuthConfig{Type: "none"}},
			wantType: "none",
		},
		{
			name:     "static auth",
			server:   TURNServer{Address: "1.2.3.4", Port: 3478, Auth: static("u", "p")},
			wantType: "static",
		},
		{
			name: "ephemeral auth",
			server: TURNServer{Address: "1.2.3.4", Port: 3478, Auth: &AuthConfig{
				Type: "ephemeral", Credentials: map[string]string{"secret": "s"}}},
			wantType: "ephemeral",
		},
		{
			name: "ephemeral auth with a lifetime",
			server: TURNServer{Address: "1.2.3.4", Port: 3478, Auth: &AuthConfig{
				Type: "ephemeral", Lifetime: "30m",
				Credentials: map[string]string{"secret": "s"}}},
			wantType: "ephemeral",
		},
		{
			name: "an unparseable lifetime is rejected",
			server: TURNServer{Address: "1.2.3.4", Port: 3478, Auth: &AuthConfig{
				Type: "ephemeral", Lifetime: "soon",
				Credentials: map[string]string{"secret": "s"}}},
			err: true,
		},
		{
			// missing credentials are no config error: the upstream server refuses them
			name: "ephemeral auth without a secret is accepted",
			server: TURNServer{Address: "1.2.3.4", Port: 3478, Auth: &AuthConfig{
				Type: "ephemeral", Credentials: map[string]string{}}},
			wantType: "ephemeral",
		},
		{name: "missing address", server: TURNServer{Port: 3478}, err: true},
		{name: "zero port", server: TURNServer{Address: "1.2.3.4"}, err: true},
		{name: "port out of range", server: TURNServer{Address: "1.2.3.4", Port: 65536}, err: true},
		{
			name:     "static auth without a password is accepted",
			server:   TURNServer{Address: "1.2.3.4", Port: 3478, Auth: &AuthConfig{Type: "static", Credentials: map[string]string{"username": "u"}}},
			wantType: "static",
		},
	} {
		t.Run(c.name, func(t *testing.T) {
			s := c.server
			err := s.Validate()
			if c.err {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			if c.wantType == "" {
				assert.Nil(t, s.Auth, "no auth block")
				return
			}
			require.NotNil(t, s.Auth, "auth block")
			assert.Equal(t, c.wantType, s.Auth.Type, "auth type")
		})
	}
}

func TestAuthConfigCredentialLifetime(t *testing.T) {
	a := AuthConfig{Type: "ephemeral", Credentials: map[string]string{"secret": "s"}}
	require.NoError(t, a.Validate())
	assert.Equal(t, DefaultCredentialLifetime, a.CredentialLifetime(), "unset falls back")

	a.Lifetime = "30m"
	require.NoError(t, a.Validate())
	assert.Equal(t, 30*time.Minute, a.CredentialLifetime(), "explicit lifetime")
}

func TestTURNServerAuthOmittedFromJSON(t *testing.T) {
	// a struct-typed Auth would always serialize, omitempty or not: only a pointer is omitted
	b, err := json.Marshal(&TURNServer{Address: "1.2.3.4", Port: 3478})
	require.NoError(t, err)
	assert.Equal(t, `{"address":"1.2.3.4","port":3478}`, string(b), "no empty auth block")

	s := TURNServer{}
	require.NoError(t, json.Unmarshal(b, &s))
	assert.Nil(t, s.Auth, "auth stays nil over a round-trip")
}
