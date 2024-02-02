package v1

// stunnerd defaults
const (
	ApiVersion             string = "v1"
	DefaultStunnerName            = "default-stunnerd"
	DefaultProtocol               = "turn-udp"
	DefaultClusterProtocol        = "udp"
	DefaultPort            int    = 3478
	DefaultLogLevel               = "all:INFO"
	DefaultRealm                  = "stunner.l7mp.io"
	DefaultAuthType               = "static"
	DefaultMinRelayPort    int    = 1
	DefaultMaxRelayPort    int    = 1<<16 - 1
	DefaultClusterType            = "STATIC"
	DefaultAdminName              = "default-admin-config"
	DefaultAuthName               = "default-auth-config"
)

// health-check and metrics reporting defaults
const (
	DefaultMetricsPort     int = 8080
	DefaultHealthCheckPort int = 8086
)

// CDS defaults
const (
	DefaultConfigDiscoveryAddress = ":13478"
	DefaultEnvVarName             = "STUNNER_NAME"
	DefaultEnvVarNamespace        = "STUNNER_NAMESPACE"
	DefaultEnvVarNodeName         = "STUNNER_NODENAME"
	DefaultEnvVarConfigOrigin     = "STUNNER_CONFIG_ORIGIN"
	DefaultCDSServiceLabelKey     = "stunner.l7mp.io/config-discovery-service"
	DefaultCDSServiceLabelValue   = "enabled"
	DefaultCDSServerAddrEnv       = "CDS_SERVER_ADDR"
	DefaultCDSServerNamespaceEnv  = "CDS_SERVER_NAMESPACE"
	DefaultCDSServerPortEnv       = "CDS_SERVER_PORT"
)
