package v1

const ApiVersion string = "v1"
const DefaultStunnerName = "default-stunnerd"
const DefaultProtocol = "turn-udp"
const DefaultClusterProtocol = "udp"
const DefaultPort int = 3478
const DefaultLogLevel = "all:INFO"
const DefaultRealm = "stunner.l7mp.io"
const DefaultAuthType = "static"
const DefaultMinRelayPort int = 1
const DefaultMaxRelayPort int = 1<<16 - 1
const DefaultClusterType = "STATIC"

const DefaultAdminName = "default-admin-config"
const DefaultAuthName = "default-auth-config"

const DefaultMetricsPort int = 8080
const DefaultHealthCheckPort int = 8086

// DefaultConfigDiscoveryAddress is the default URI at which config discovery requests are served.
const DefaultConfigDiscoveryAddress = "0.0.0.0:13478"
