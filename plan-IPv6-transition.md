# STUNner IPv6 Transition Plan

## 1. Root Cause Analysis: Why IPv4-Only Stopped Working

### The Problem

After migrating from pion/turn/v4 to v5, localhost tests started failing with:
```
listen udp6: address 0.0.0.0: no suitable address found
```

### Technical Explanation

**pion/turn/v5 API Change:**
- Old: `AllocatePacketConn(network string, requestedPort int)`
- New: `AllocatePacketConn(AllocateListenerConfig)` where `AllocateListenerConfig.Network` is `"udp4"`, `"udp6"`, `"tcp4"`, or `"tcp6"`

**RFC 6156 Implementation:**
pion/turn/v5 properly implements RFC 6156 (TURN Extension for IPv6):
1. TURN server parses `REQUESTED-ADDRESS-FAMILY` attribute from client ALLOCATE requests
2. If attribute is present: uses client's preference (IPv4=0x01, IPv6=0x02)
3. If attribute is absent: RFC 6156 says default to IPv4
4. Server then sets `AllocateListenerConfig.Network` accordingly

**The Mismatch:**
- STUNner's relay generator was hardcoded to bind to `"0.0.0.0"` (IPv4-only)
- The pion TURN client sent IPv6 allocation requests (based on system network config or client library behavior)
- Result: `net.ListenPacket("udp6", "0.0.0.0:0")` fails because `0.0.0.0` is not a valid IPv6 address

---

### Phase 1: Core Server Changes

#### Task 1.1: Update `server.go` - Listener Address Derivation
**File:** `/export/l7mp/stunner/server.go`
**Line:** 40

**Current (hardcoded IPv4):**
```go
addr := fmt.Sprintf("0.0.0.0:%d", l.Port)
```

**Change to:**
```go
// Derive bind address from listener's configured address
bindIP := "0.0.0.0"
if l.Addr.To4() == nil {
    bindIP = "::"
}
addr := fmt.Sprintf("%s:%d", bindIP, l.Port)
```

#### Task 1.2: Update `server.go` - Network Type for UDP Listeners
**File:** `/export/l7mp/stunner/server.go`
**Lines:** 48, 44

**Current:**
```go
conns, err := socketPool.Make("udp", addr)
```

**Change to:**
```go
network := "udp"
if l.Addr.To4() == nil {
    network = "udp6"
}
conns, err := socketPool.Make(network, addr)
```

#### Task 1.3: Update `server.go` - TCP Listener Network Type
**File:** `/export/l7mp/stunner/server.go`
**Line:** 67

**Current:**
```go
tcpListener, err := net.Listen("tcp", addr)
```

**Change to:**
```go
tcpNetwork := "tcp"
if l.Addr.To4() == nil {
    tcpNetwork = "tcp6"
}
tcpListener, err := net.Listen(tcpNetwork, addr)
```

#### Task 1.4: Update `server.go` - TLS Listener Network Type
**File:** `/export/l7mp/stunner/server.go`
**Line:** 92

**Current:**
```go
tlsListener, err := tls.Listen("tcp", addr, &tls.Config{...})
```

**Change to:**
```go
tlsListener, err := tls.Listen(tcpNetwork, addr, &tls.Config{...})
```

#### Task 1.5: Update `server.go` - DTLS Listener Network Type
**File:** `/export/l7mp/stunner/server.go`
**Lines:** 121-125

**Current:**
```go
udpAddr, err := net.ResolveUDPAddr("udp", addr)
dtlsListener, err := dtls.Listen("udp", udpAddr, &dtls.Config{...})
```

**Change to:**
```go
udpNetwork := "udp"
if l.Addr.To4() == nil {
    udpNetwork = "udp6"
}
udpAddr, err := net.ResolveUDPAddr(udpNetwork, addr)
dtlsListener, err := dtls.Listen(udpNetwork, udpAddr, &dtls.Config{...})
```

---

### Phase 2: Relay Generator Changes

#### Task 2.1: Update `relay.go` - NewRelayGen Bind Address
**File:** `/export/l7mp/stunner/relay.go`
**Line:** 69

**Current:**
```go
return &RelayGen{
    ...
    Address:      "0.0.0.0",
    ...
}
```

**Change to:**
```go
bindAddr := "0.0.0.0"
if l.Addr.To4() == nil {
    bindAddr = "::"
}
return &RelayGen{
    ...
    Address:      bindAddr,
    ...
}
```

---

### Phase 3: Turncat Changes

#### Task 3.1: Update `turncat.go` - TURN Client Bind Address
**File:** `/export/l7mp/stunner/turncat.go`
**Line:** 252

**Current:**
```go
t, err := net.ListenPacket(t.serverAddr.Network(), "0.0.0.0:0")
```

**Change to:**
```go
// Determine bind address based on server address family
bindAddr := "0.0.0.0:0"
if udpAddr, ok := t.serverAddr.(*net.UDPAddr); ok && udpAddr.IP.To4() == nil {
    bindAddr = "[::]:0"
}
tc, err := net.ListenPacket(t.serverAddr.Network(), bindAddr)
```

---

### Phase 4: API/Config Changes

#### Task 4.1: Update `pkg/apis/v1/listener.go` - IPv6 URI Formatting
**File:** `/export/l7mp/stunner/pkg/apis/v1/listener.go`
**Method:** `String()` (around line 101)

**Add IPv6 bracket handling for URIs:**
```go
// Format IPv6 addresses with brackets for URI compatibility
ip := net.ParseIP(addr)
if ip != nil && ip.To4() == nil {
    addr = fmt.Sprintf("[%s]", addr)
}
```

#### Task 4.2: Update `pkg/apis/v1/listener.go` - GetListenerURI
**File:** `/export/l7mp/stunner/pkg/apis/v1/listener.go`
**Method:** `GetListenerURI()` (around line 138)

**Same change as 4.1 - add IPv6 bracket handling**

#### Task 4.3: Update `internal/object/listener.go` - localhost6 support
**File:** `/export/l7mp/stunner/internal/object/listener.go`
**Lines:** 132-137

**Add explicit IPv6 localhost:**
```go
if ipAddr == nil && req.Addr == "localhost" {
    ipAddr = net.ParseIP("127.0.0.1")
}
if ipAddr == nil && req.Addr == "localhost6" {
    ipAddr = net.ParseIP("::1")
}
```

---

### Phase 5: Test Suite Updates

#### Task 5.1: Add IPv6 Unit Tests for Relay Generator
**File:** `/export/l7mp/stunner/relay_test.go`

**New tests to add:**
```go
func TestRelayGenIPv6BindAddress(t *testing.T) {
    // Test that IPv6 listener gets "::" bind address
}

func TestRelayGenIPv6FallbackToIPv4(t *testing.T) {
    // Test that IPv4 relay falls back when IPv6 requested
}

func TestRelayGenIPv6WithIPv6Request(t *testing.T) {
    // Test proper IPv6 allocation when both match
}
```

#### Task 5.2: Add IPv6 Integration Tests
**File:** `/export/l7mp/stunner/stunner_test.go`

**New test configurations:**
```go
var TestStunnerConfigsWithIPv6 = []TestStunnerConfigCase{
    {
        // IPv6 localhost listener
        config: stnrv1.StunnerConfig{
            Listeners: []stnrv1.ListenerConfig{{
                Name:     "udp-ipv6",
                Protocol: "turn-udp",
                Addr:     "::1",
                Port:     23478,
                Routes:   []string{"allow-any"},
            }},
            Clusters: []stnrv1.ClusterConfig{{
                Name:      "allow-any",
                Endpoints: []string{"::/0"},
            }},
        },
        uri: "turn://[::1]:23478?transport=udp",
    },
}

func TestStunnerIPv6Localhost(t *testing.T) {
    // Skip if IPv6 not available
    if _, err := net.Listen("tcp6", "[::1]:0"); err != nil {
        t.Skip("IPv6 not available on this system")
    }
    testStunnerLocalhost(t, 1, TestStunnerConfigsWithIPv6)
}
```

#### Task 5.3: Add IPv6 Endpoint Parsing Tests
**File:** `/export/l7mp/stunner/internal/util/endpoint_test.go`

**Additional test cases:**
```go
{name: "ipv6-wildcard", input: "::/0", output: "::/0", success: true},
{name: "ipv6-link-local", input: "fe80::1/10", output: "fe80::/10", success: true},
{name: "ipv6-localhost", input: "::1", output: "::1/128", success: true},
{name: "ipv6-with-port-range", input: "2001:db8::1:<80-443>", output: "2001:db8::1/128:<80-443>", success: true},
```

#### Task 5.4: Add IPv6 Turncat Tests
**File:** `/export/l7mp/stunner/turncat_test.go`

**New test:**
```go
func TestTurncatIPv6(t *testing.T) {
    if _, err := net.Listen("tcp6", "[::1]:0"); err != nil {
        t.Skip("IPv6 not available")
    }
    // Test turncat with IPv6 server address
}
```

#### Task 5.5: Add IPv6 Route Matching Tests
**File:** `/export/l7mp/stunner/internal/util/endpoint_test.go`

**Existing `TestRouteMatch` only has IPv4 - add:**
```go
{
    name:   "ipv6-match-in-range",
    cidr:   "2001:db8::/32",
    ip:     "2001:db8::1",
    result: true,
},
{
    name:   "ipv6-no-match-outside-range",
    cidr:   "2001:db8::/32",
    ip:     "2001:db9::1",
    result: false,
},
```

---

### Phase 6: Documentation

#### Task 6.1: Update CLAUDE.md
Add IPv6 configuration examples to the project documentation.

#### Task 6.2: Create IPv6 Configuration Example
**Example config snippet:**
```yaml
listeners:
  - name: turn-ipv6
    protocol: turn-udp
    addr: "::"           # Listen on all IPv6 interfaces
    port: 3478
    routes:
      - allow-any-v6

clusters:
  - name: allow-any-v6
    endpoints:
      - "::/0"           # Allow all IPv6 peers
```

---

## 4. Verification Checklist

### Build Verification
- [ ] `go build ./...` succeeds
- [ ] `go vet ./...` passes
- [ ] No new linter warnings

### Unit Tests
- [ ] `go test ./... -v` passes
- [ ] `go test ./... -run IPv6` passes (new IPv6 tests)

### Manual Verification

#### IPv4 (backward compatibility)
```bash
# Should still work exactly as before
./bin/stunnerd -c 'turn://user:pass@127.0.0.1:3478'
```

#### IPv6 localhost
```bash
# Should work with IPv6
./bin/stunnerd -c 'turn://user:pass@[::1]:3478'
```

#### IPv6 turncat
```bash
./bin/turncat 'udp://[::1]:25000' 'turn://user:pass@[::1]:3478' 'udp://[::1]:25678'
```

### Integration Verification
- [ ] VNet-based tests pass (existing)
- [ ] Localhost tests pass (IPv4)
- [ ] Localhost tests pass (IPv6)
- [ ] Mixed IPv4/IPv6 cluster endpoints work

---

## 5. Files to Modify Summary

| File | Changes |
|------|---------|
| `server.go` | Line 40: bind address; Lines 48,67,92,121: network types |
| `relay.go` | Line 69: NewRelayGen bind address |
| `turncat.go` | Line 252: TURN client bind address |
| `pkg/apis/v1/listener.go` | Lines ~101, ~138: IPv6 URI bracket formatting |
| `internal/object/listener.go` | Lines 132-137: localhost6 support |
| `relay_test.go` | Add IPv6 relay generation tests |
| `stunner_test.go` | Add IPv6 integration tests |
| `internal/util/endpoint_test.go` | Add IPv6 route matching tests |
| `turncat_test.go` | Add IPv6 turncat tests |

---

## 6. Backward Compatibility Notes

1. **Default remains IPv4**: `"0.0.0.0"` is still the default listener address
2. **No config schema changes**: Existing configs work unchanged
3. **Graceful fallback**: IPv6 requests to IPv4 relays fall back to IPv4
4. **Warning logging**: Add debug/info logs when fallback occurs
