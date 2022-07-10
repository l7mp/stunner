package monitoring

import (
	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus"
)

func RegisterMetrics(log logging.LeveledLogger, GetAllocationCount func() float64) {
	if err := prometheus.Register(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "allocation_count",
			Help: "Number of active allocations.",
		},
		GetAllocationCount,
	)); err == nil {
		log.Debug("GaugeFunc 'allocation' registered.")
	} else {
		log.Warn("GaugeFunc 'allocation' cannot be registered (already registered?).")
	}

	//TODO: add connection metrics
}
