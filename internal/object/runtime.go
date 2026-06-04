package object

import (
	"github.com/pion/transport/v4"

	"github.com/l7mp/stunner/internal/quota"
	"github.com/l7mp/stunner/internal/resolver"
	"github.com/l7mp/stunner/internal/telemetry"
	"github.com/l7mp/stunner/pkg/logger"
)

// Runtime carries per-STUNner process dependencies used by object constructors.
type Runtime struct {
	Logger           logger.LoggerFactory
	DryRun           bool
	Resolver         resolver.DnsResolver
	Telemetry        *telemetry.Telemetry
	QuotaStore       quota.Store
	UdpThreadNum     int
	Net              transport.Net
	ReadinessHandler ReadinessHandler
	StatusHandler    StatusHandler
	OffloadHandler   OffloadHandlerCtor
}
