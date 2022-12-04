package telemetry

import (
	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus"
)

var AllocActiveGauge prometheus.GaugeFunc

//TODO: add connection metrics

func RegisterMetrics(log logging.LeveledLogger, GetAllocationCount func() float64) {
	AllocActiveGauge = prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "stunner_allocations_active",
			Help: "Number of active allocations.",
		},
		GetAllocationCount,
	)
	if err := prometheus.Register(AllocActiveGauge); err == nil {
		log.Debug("GaugeFunc 'stunner_allocations_active' registered.")
	} else {
		log.Warn("GaugeFunc 'stunner_allocations_active' cannot be registered.")
	}
}

func UnregisterMetrics(log logging.LeveledLogger) {
	if AllocActiveGauge != nil {
		if success := prometheus.Unregister(AllocActiveGauge); success {
			log.Debug("GaugeFunc 'stunner_allocations_active' unregistered.")
			return
		}
	}
	log.Warn("GaugeFunc 'stunner_allocations_active' cannot be unregistered.")
}
