package monitoring

import (
	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus"
)

func RegisterMetrics(log logging.LeveledLogger, GetAllocationCount func() float64) {
	if err := prometheus.Register(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "stunner_allocations_active",
			Help: "Number of active allocations.",
		},
		GetAllocationCount,
	)); err == nil {
		log.Debug("GaugeFunc 'stunner_allocations_active' registered.")
	} else {
		log.Warn("GaugeFunc 'stunner_allocations_active' cannot be registered (already registered?).")
	}

	//TODO: add connection metrics
}
