package v1alpha1

const ApiVersion string = "v1alpha1"
const DefaultStunnerName = "default-stunnerd"
const DefaultProtocol = "udp"
const DefaultPort int = 3478
const DefaultLogLevel = "all:INFO"
const DefaultRealm = "stunner.l7mp.io"
const DefaultAuthType = "plaintext"

// no more default user/pass pairs
// const DefaultUsername = "user1"
// const DefaultPassword = "passwd1"

const DefaultMinRelayPort int = 1 << 15
const DefaultMaxRelayPort int = 1<<16 - 1
const DefaultClusterType = "STATIC"

const DefaultAdminName = "default-admin-config"
const DefaultAuthName = "default-auth-config"

const DefaultMonitoringGroup = "STUNner"
