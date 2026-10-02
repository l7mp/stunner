package client

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetURI(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		scheme   string
		host     string // u.Host, i.e. the authority including port and brackets
		hostname string // u.Hostname(), checked only when non-empty
		port     string
		path     string
		wantErr  bool
	}{
		// file origins
		{name: "file url", in: "file:///tmp/a", scheme: "file", host: "", path: "/tmp/a"},
		{name: "bare path defaults to http", in: "/tmp/a", scheme: "http", host: "", path: "/tmp/a"},

		// bare network address, default http scheme
		{name: "ipv4 host:port", in: "1.2.3.4:12345", scheme: "http",
			host: "1.2.3.4:12345", hostname: "1.2.3.4", port: "12345"},
		{name: "ipv4 host:port + path", in: "1.2.3.4:12345/a/b", scheme: "http",
			host: "1.2.3.4:12345", hostname: "1.2.3.4", port: "12345", path: "/a/b"},

		// explicit schemes
		{name: "http url", in: "http://1.2.3.4:12345/a/b", scheme: "http",
			host: "1.2.3.4:12345", hostname: "1.2.3.4", port: "12345", path: "/a/b"},
		{name: "ws url", in: "ws://1.2.3.4:12345/a/b", scheme: "ws",
			host: "1.2.3.4:12345", hostname: "1.2.3.4", port: "12345", path: "/a/b"},
		{name: "https url", in: "https://example.com:8443/x", scheme: "https",
			host: "example.com:8443", hostname: "example.com", port: "8443", path: "/x"},
		{name: "wss url", in: "wss://example.com:8443/x", scheme: "wss",
			host: "example.com:8443", hostname: "example.com", port: "8443", path: "/x"},

		// bracketed IPv6 is accepted in every shape
		{name: "bracketed ipv6 host:port", in: "[::1]:8080", scheme: "http",
			host: "[::1]:8080", hostname: "::1", port: "8080"},
		{name: "bracketed ipv6 url + path", in: "http://[2001:db8::1]:8080/a", scheme: "http",
			host: "[2001:db8::1]:8080", hostname: "2001:db8::1", port: "8080", path: "/a"},
		{name: "bracketed ipv6 production endpoint",
			in: "ws://[2600:1f14:1e98:4505:14dc::9]:13478/api/v1/configs", scheme: "ws",
			host: "[2600:1f14:1e98:4505:14dc::9]:13478", hostname: "2600:1f14:1e98:4505:14dc::9",
			port: "13478", path: "/api/v1/configs"},

		// unbracketed IPv6 is rejected (RFC 3986); the caller must bracket it
		{name: "unbracketed ipv6 host:port", in: "2001:db8::1:8080", wantErr: true},
		{name: "unbracketed ipv6 production endpoint",
			in: "http://2600:1f14:1e98:4505:14dc::9:13478", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := getURI(tc.in)
			if tc.wantErr {
				assert.Error(t, err, "expected error")
				return
			}
			assert.NoError(t, err, "parse")
			assert.Equal(t, tc.scheme, u.Scheme, "scheme")
			assert.Equal(t, tc.host, u.Host, "host")
			assert.Equal(t, tc.path, u.Path, "path")
			if tc.hostname != "" {
				assert.Equal(t, tc.hostname, u.Hostname(), "hostname")
				assert.Equal(t, tc.port, u.Port(), "port")
			}
		})
	}
}

func TestWsURI(t *testing.T) {
	const endpoint = "/api/v1/configs"

	cases := []struct {
		name    string
		addr    string
		labels  map[string]string
		want    string
		wantErr bool
	}{
		// scheme mapping: http/ws -> ws, https/wss -> wss
		{name: "bare host:port -> ws", addr: "1.2.3.4:12345",
			want: "ws://1.2.3.4:12345/api/v1/configs?watch=true"},
		{name: "http -> ws", addr: "http://1.2.3.4:12345",
			want: "ws://1.2.3.4:12345/api/v1/configs?watch=true"},
		{name: "ws stays ws", addr: "ws://1.2.3.4:12345",
			want: "ws://1.2.3.4:12345/api/v1/configs?watch=true"},
		{name: "https -> wss", addr: "https://1.2.3.4:12345",
			want: "wss://1.2.3.4:12345/api/v1/configs?watch=true"},
		{name: "wss stays wss", addr: "wss://1.2.3.4:12345",
			want: "wss://1.2.3.4:12345/api/v1/configs?watch=true"},

		// labels are added to the query as key=value, one parameter each, sorted
		{name: "labels included", addr: "1.2.3.4:12345",
			labels: map[string]string{"node": "node-1", "pod": "pod-1"},
			want:   "ws://1.2.3.4:12345/api/v1/configs?label=node%3Dnode-1&label=pod%3Dpod-1&watch=true"},

		// bracketed IPv6 round-trips; unbracketed is rejected
		{name: "bracketed ipv6", addr: "[2600:1f14:1e98:4505:14dc::9]:13478",
			want: "ws://[2600:1f14:1e98:4505:14dc::9]:13478/api/v1/configs?watch=true"},
		{name: "unbracketed ipv6 errors", addr: "2600:1f14:1e98:4505:14dc::9:13478", wantErr: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := wsURI(tc.addr, endpoint, tc.labels)
			if tc.wantErr {
				assert.Error(t, err, "expected error")
				return
			}
			assert.NoError(t, err, "build")
			assert.Equal(t, tc.want, got, "ws uri")
		})
	}
}
