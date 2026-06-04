# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

STUNner is a Kubernetes ingress gateway for WebRTC that exposes a standards-compliant STUN/TURN server to allow WebRTC clients to access virtualized WebRTC infrastructure running in Kubernetes. Built on top of the pion/webrtc framework, it implements the Kubernetes Gateway API for declarative configuration.

**Key Architecture:**
- **Control Plane**: Gateway API resources (GatewayClass, Gateway, UDPRoute) + gateway operator that renders configs
- **Data Plane**: Fleet of `stunnerd` pods (TURN servers) that handle media traffic ingestion
- **Config Discovery Service (CDS)**: WebSocket-based service for dynamic dataplane configuration updates

## Build Commands

```bash
# Build all binaries
make build

# Build specific binaries (outputs to bin/)
make build-bin                    # All binaries
go build -o bin/stunnerd cmd/stunnerd/main.go
go build -o bin/turncat cmd/turncat/main.go
go build -o bin/stunnerctl cmd/stunnerctl/*.go
go build -o bin/icetester cmd/icetester/main.go

# Code generation (required before build)
make generate                     # OpenAPI codegen for pkg/config/...

# Format and vet
make fmt                          # Run go fmt
make vet                          # Run go vet
```

## Testing Commands

```bash
# Run all tests (includes generate, fmt, vet)
make test

# Run tests directly
go test ./... -v

# Run tests with coverage
go test -v -covermode=count -coverprofile=coverage.out

# Run specific package tests
go test ./pkg/config/client -v
go test ./internal/object -v

# Run single test
go test -run TestSpecificFunction ./path/to/package -v
```

## Key Components

### Core Packages

- **`github.com/l7mp/stunner`** (root): Main stunner daemon logic
  - `stunner.go`: Core `Stunner` struct and lifecycle (NewStunner, Reconcile, Shutdown)
  - `config.go`: Configuration loading from various origins (file, CDS, URI)
  - `reconcile.go`: Config reconciliation logic for listeners/clusters
  - `relay.go`: `RelayGen` for TURN relay address allocation with port range filtering
  - `server.go`: TURN server initialization and management
  - `handlers.go`: Authentication, permission, quota, and event handlers

- **`pkg/apis/v1`**: STUNner API v1 types
  - `StunnerConfig`: Top-level config structure
  - `AdminConfig`, `AuthConfig`, `ListenerConfig`, `ClusterConfig`
  - All configs implement the `Config` interface with `Validate()`, `DeepEqual()`, etc.

- **`pkg/config/client`**: Config client implementations
  - `CDSClient`: Config Discovery Service client (WebSocket-based)
  - `ConfigFileClient`: File-based config watcher using fsnotify
  - `New()`: Factory that returns appropriate client based on origin URI scheme
  - Supports `Load()` for one-shot loads and `Watch()` for continuous monitoring

- **`internal/object`**: Internal object managers
  - `Admin`, `Auth`, `Listener`, `Cluster`: STUNner internal objects
  - Each object has its own lifecycle and reconciliation logic

- **`internal/manager`**: Object lifecycle manager abstraction
  - Common interface for managing STUNner objects (Get, Upsert, Keys, etc.)

- **`internal/resolver`**: DNS resolver for STRICT_DNS cluster type

- **`internal/telemetry`**: Metrics and telemetry subsystem

### Command-Line Tools

- **`cmd/stunnerd`**: Main TURN gateway daemon
  - Can run standalone via URI (e.g., `turn://user:pass@127.0.0.1:3478`)
  - Production mode: config from file or CDS with watch mode (-w flag)
  - Supports UDP listener CPU scaling via `--udp-thread-num=N` flag

- **`cmd/turncat`**: STUN/TURN client tunnel tool
  - Opens connections through TURN server to arbitrary endpoints
  - Supports `k8s://` URIs to auto-discover STUNner configs from cluster
  - Usage: `turncat <local-addr> <turn-uri> <peer-addr>`

- **`cmd/stunnerctl`**: STUNner control utility for querying running instances

- **`cmd/icetester`**: ICE connectivity testing tool

## Configuration Details

### Config Origins

STUNner supports multiple config origins (see `config.go`):
- **File**: `file:///path/to/config.yaml` - watched via fsnotify
- **CDS**: `ws://host:port/path` or `http://host:port` - CDS server endpoint
- **K8s discovery**: `k8s://namespace/gateway:listener` - auto-discovery from k8s

Environment variable substitution is performed during config parsing (see `pkg/config/client/config.go`).

### Listener Types

Supported protocols (see `pkg/apis/v1/util.go`):
- `TURN-UDP` (or just `UDP`)
- `TURN-TCP` (or just `TCP`)
- `TURN-TLS` (or just `TLS`) - TCP+TLS
- `TURN-DTLS` (or just `DTLS`) - UDP+DTLS

### Cluster Types

- **STATIC**: Explicit endpoint IP addresses
- **STRICT_DNS**: DNS-based resolution (works well with Kubernetes headless services)

### Authentication Modes

- **static** (formerly "plaintext"): Fixed username/password
- **ephemeral** (formerly "longterm"): Time-scoped credentials with shared secret

## Important Implementation Details

### Reconciliation Model

When `Reconcile()` is called with a new config:
1. Diffs new config against running config
2. Updates objects (Admin, Auth, Listeners, Clusters) via their managers
3. Restarts objects if necessary (e.g., listener protocol/address changes)
4. Returns `ErrRestartRequired` if any restarts occurred (safe to ignore)
5. On error, rolls back to last working config (unless `SuppressRollback` is set)

### TURN Server Lifecycle

Each `Listener` object manages a separate TURN server instance. Listeners are restarted when:
- Protocol changes (UDP ↔ TCP ↔ TLS ↔ DTLS)
- Address or port changes
- TLS certificate/key changes

### Port Range Filtering

The `RelayGen` (relay.go) generates relay addresses and enforces cluster-level port range restrictions:
- Filters peer addresses against cluster endpoint definitions
- Caches cluster lookups for performance (LRU cache)
- Uses `PortRangePacketConn` wrapper to enforce filtering on read/write

### Config Discovery Service (CDS)

CDS servers expose WebSocket endpoints for streaming config updates:
- REST endpoint: `GET /api/v1/configs/{namespace}/{name}`
- WebSocket endpoint: Same path with `Upgrade: websocket`
- Clients use `Watch()` to maintain persistent connections with automatic retry

## Testing Patterns

Tests follow Go conventions:
- Unit tests alongside source files (`*_test.go`)
- Use `github.com/stretchr/testify/assert` for assertions
- Mock external dependencies (DNS resolver, network transport via pion VNet)
- Integration tests in `pkg/config/cds_test.go` use temporary file watchers

### Test Utilities

- `pkg/apis/v1`: Configs have `DeepEqual()` for comparison in tests
- `internal/object`: Objects implement `String()` for debugging
- Test configs often use `ZeroConfig()` or `EmptyConfig()` as base

## gopls MCP Integration

**ALWAYS prefer gopls MCP tools over manual file operations:**

```bash
# Package structure
go_workspace                          # Understand workspace layout
go_package_api                        # View package public API

# Navigation
go_search                             # Fuzzy search for symbols
go_symbol_references                  # Find all references to symbol
go_file_context                       # Understand file's package deps

# Code quality
go_diagnostics                        # Check for errors before/after edits
```

### Workflow Example

```bash
# 1. Understand structure
go_workspace
go_package_api(["github.com/l7mp/stunner/pkg/apis/v1"])

# 2. Find symbols
go_search({"query": "reconcile"})

# 3. Check dependencies
go_file_context({"file": "/path/to/file.go"})

# 4. Find usages before editing
go_symbol_references({"file": "/path/to/file.go", "symbol": "Reconcile"})

# 5. Edit code, then verify
go_diagnostics({"files": ["/path/to/edited.go"]})
```

## Common Development Workflows

### Adding a New Config Field

1. Add field to appropriate `*Config` struct in `pkg/apis/v1/`
2. Update `Validate()` method to handle defaults/validation
3. Update `DeepEqual()` if needed for comparison
4. Update `String()` for debugging output
5. Handle the field in reconciliation logic (reconcile.go or object managers)
6. Add tests in corresponding `*_test.go`

### Modifying TURN Server Behavior

1. Locate handler in `handlers.go` (auth, permission, quota, events)
2. Modify handler implementation
3. Check if listener restart needed (see `internal/object/listener.go`)
4. Run `go_diagnostics` after changes
5. Test with `turncat` against modified `stunnerd`

### Working with CDS

1. API definitions in `pkg/config/api/` (OpenAPI generated)
2. Client implementation in `pkg/config/client/cds_*.go`
3. To regenerate API: `make generate`
4. Test changes with `pkg/config/cds_test.go`

## Documentation

- `/docs`: Comprehensive documentation
  - `CONCEPTS.md`: Architecture overview
  - `GATEWAY.md`: Gateway API reference
  - `AUTH.md`: Authentication guide
  - `DEPLOYMENT.md`: Deployment models
  - `examples/`: Tutorials for various WebRTC servers

## Important Notes

- **Comment style**: Follow Go doc comment conventions; end sentences with periods.
- **Import management**: Use `goimports` (located at `/usr/bin/goimports`) with short timeout for auto-fixing imports.
- **License**: MIT License - see AUTHORS and LICENSE files.
- **API version**: Current API version is v1 (see `pkg/apis/v1/default.go`).
- **Default ports**: TURN 3478, metrics 8080, health-check 8086, CDS 13478.
