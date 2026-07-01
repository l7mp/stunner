package client

import "testing"

func TestBracketIPv6HostPort(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		// Pass-through cases — no change expected.
		{"ipv4 with port", "1.2.3.4:8080", "1.2.3.4:8080"},
		{"hostname with port", "example.com:8080", "example.com:8080"},
		{"hostname no port", "example.com", "example.com"},
		{"already bracketed", "[::1]:8080", "[::1]:8080"},
		{"already bracketed long", "[2001:db8::1]:8080", "[2001:db8::1]:8080"},
		{"bare ipv4", "1.2.3.4", "1.2.3.4"},
		{"bare ipv6 short", "::1", "::1"},
		{"bare ipv6 full", "2001:db8::1", "2001:db8::1"},
		{"empty string", "", ""},
		{"single colon, non-numeric port", "foo:bar", "foo:bar"},

		// Bug-fix cases — bracketing expected.
		{"unbracketed ipv6 short + port", "::1:8080", "[::1]:8080"},
		{"unbracketed ipv6 full + port", "2001:db8::1:8080", "[2001:db8::1]:8080"},
		{
			// The exact production input observed when reproducing #213
			// on an IPv6-only EKS cluster.
			name: "production stunner CDS endpoint (issue #213)",
			in:   "2600:1f14:1e98:4505:14dc::9:13478",
			want: "[2600:1f14:1e98:4505:14dc::9]:13478",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bracketIPv6HostPort(tc.in)
			if got != tc.want {
				t.Errorf("bracketIPv6HostPort(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestBracketIPv6InURL(t *testing.T) {
	// Direct coverage of the wrapper. The prefixed-URL cases are the
	// production input shape — the operator hands stunnerd CDS addresses
	// with the scheme already attached, and the original #213 patch
	// missed this branch.
	cases := []struct {
		name string
		in   string
		want string
	}{
		// No scheme — wrapper just delegates to bracketIPv6HostPort.
		{"bare ipv4:port", "1.2.3.4:8080", "1.2.3.4:8080"},
		{"bare ipv6:port", "2001:db8::1:8080", "[2001:db8::1]:8080"},
		{"bare hostname:port", "example.com:8080", "example.com:8080"},
		{"empty", "", ""},

		// http:// prefix only.
		{"http ipv4", "http://1.2.3.4:8080", "http://1.2.3.4:8080"},
		{"http ipv6 short", "http://::1:8080", "http://[::1]:8080"},
		{
			name: "http production stunner CDS endpoint",
			in:   "http://2600:1f14:1e98:4505:14dc::9:13478",
			want: "http://[2600:1f14:1e98:4505:14dc::9]:13478",
		},
		{"http already bracketed", "http://[2001:db8::1]:8080", "http://[2001:db8::1]:8080"},

		// With path and query, which is what wsURI ultimately builds.
		{
			name: "ws ipv6 with path and query",
			in:   "ws://2600:1f14:1e98:4505:14dc::9:13478/api/v1/configs/ns/gw?watch=true",
			want: "ws://[2600:1f14:1e98:4505:14dc::9]:13478/api/v1/configs/ns/gw?watch=true",
		},
		{
			name: "http ipv6 with path only",
			in:   "http://2001:db8::1:8080/foo/bar",
			want: "http://[2001:db8::1]:8080/foo/bar",
		},

		// Don't mangle file:// URLs (no authority).
		{"file path", "file:///etc/stunnerd.conf", "file:///etc/stunnerd.conf"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bracketIPv6InURL(tc.in)
			if got != tc.want {
				t.Errorf("bracketIPv6InURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestGetURI_IPv6HostPort(t *testing.T) {
	// Regression test for #213: getURI must not return a URL whose host
	// retains unbracketed IPv6, because url.Parse can't read that form
	// and downstream dial calls fail with "too many colons in address".
	cases := []struct {
		name     string
		in       string
		wantHost string
		wantPort string
	}{
		{
			name:     "unbracketed ipv6 host:port",
			in:       "2001:db8::1:8080",
			wantHost: "2001:db8::1",
			wantPort: "8080",
		},
		{
			name:     "production stunner CDS endpoint",
			in:       "2600:1f14:1e98:4505:14dc::9:13478",
			wantHost: "2600:1f14:1e98:4505:14dc::9",
			wantPort: "13478",
		},
		{
			name:     "ipv4 host:port still works",
			in:       "10.0.0.1:8080",
			wantHost: "10.0.0.1",
			wantPort: "8080",
		},
		// Production input shape: operator pre-prefixes the scheme.
		// These cases failed silently before the bracketIPv6InURL fix —
		// the test gap that let #213 ship as broken.
		{
			name:     "http-prefixed unbracketed ipv6",
			in:       "http://2001:db8::1:8080",
			wantHost: "2001:db8::1",
			wantPort: "8080",
		},
		{
			name:     "ws-prefixed unbracketed ipv6 — production CDS shape",
			in:       "ws://2600:1f14:1e98:4505:14dc::9:13478",
			wantHost: "2600:1f14:1e98:4505:14dc::9",
			wantPort: "13478",
		},
		{
			name:     "http-prefixed already-bracketed (no-op)",
			in:       "http://[2001:db8::1]:8080",
			wantHost: "2001:db8::1",
			wantPort: "8080",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u, err := getURI(tc.in)
			if err != nil {
				t.Fatalf("getURI(%q) returned error: %v", tc.in, err)
			}
			if u.Hostname() != tc.wantHost {
				t.Errorf("hostname = %q, want %q", u.Hostname(), tc.wantHost)
			}
			if u.Port() != tc.wantPort {
				t.Errorf("port = %q, want %q", u.Port(), tc.wantPort)
			}
		})
	}
}
