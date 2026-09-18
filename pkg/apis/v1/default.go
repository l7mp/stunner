package v1

import "time"

// stunnerd defaults
const (
	ApiVersion                    string = "v1"
	DefaultStunnerName                   = "default-stunnerd"
	DefaultProtocol                      = "turn-udp"
	DefaultClusterProtocol               = "udp"
	DefaultPort                   int    = 3478
	DefaultLogLevel                      = "all:INFO"
	DefaultRealm                         = "stunner.l7mp.io"
	DefaultAuthType                      = "static"
	DefaultCredentialLifetime            = time.Hour
	DefaultMinRelayPort           int    = 1
	DefaultMaxRelayPort           int    = 1<<16 - 1
	DefaultClusterType                   = "STATIC"
	DefaultAdminName                     = "default-admin-config"
	DefaultAuthName                      = "default-auth-config"
	DefaultListenerListName              = "default-listener-list"
	DefaultClusterListName               = "default-cluster-list"
	DefaultHealthName                    = "default-health"
	DefaultMetricsName                   = "default-metrics"
	DefaultOffloadName                   = "default-offload"
	DefaultNodeAddressPlaceholder        = "__node_address_placeholder" // guaranteed to not parse as a valid IP
)

// default ports
const (
	DefaultMetricsPort     int = 8080
	DefaultHealthCheckPort int = 8086
	DefaultAuthServicePort int = 8088
	DefaultICETesterPort   int = 8089
)

// Label/annotation defaults
const (
	DefaultCDSServiceLabelKey      = "stunner.l7mp.io/config-discovery-service"
	DefaultCDSServiceLabelValue    = "enabled"
	DefaultAppLabelKey             = "app"
	DefaultAppLabelValue           = "stunner"
	DefaultAuthAppLabelValue       = "stunner-auth"
	DefaultRelatedGatewayKey       = "stunner.l7mp.io/related-gateway-name"
	DefaultRelatedGatewayNamespace = "stunner.l7mp.io/related-gateway-namespace"
	DefaultOwnedByLabelKey         = "stunner.l7mp.io/owned-by"
	DefaultOwnedByLabelValue       = "stunner"
)

// Gateway operator defaults shared with the clients that need to find the operator.
const (
	// DefaultLeaderElectionID is the name of the Lease the operator replicas compete for, in
	// the operator's namespace.
	DefaultLeaderElectionID = "stunner-gateway-operator.l7mp.io"
)

// CDS defaults
const (
	DefaultConfigDiscoveryPort    = 13478
	DefaultConfigDiscoveryAddress = ":13478"
	DefaultEnvVarName             = "STUNNER_NAME"
	DefaultEnvVarNamespace        = "STUNNER_NAMESPACE"
	DefaultEnvVarAddr             = "STUNNER_ADDR"
	DefaultEnvVarAddrs            = "STUNNER_ADDRS"
	DefaultEnvVarNodeName         = "STUNNER_NODENAME"
	DefaultEnvVarConfigOrigin     = "STUNNER_CONFIG_ORIGIN"
	DefaultCDSServerAddrEnv       = "CDS_SERVER_ADDR"
	DefaultCDSServerNamespaceEnv  = "CDS_SERVER_NAMESPACE"
	DefaultCDSServerPortEnv       = "CDS_SERVER_PORT"
)
