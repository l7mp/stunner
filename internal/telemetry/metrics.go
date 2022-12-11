package telemetry

import (
	// "github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	stunnerNamespace = "stunner"
)

var (
	promConnLabels           = []string{"name"}
	promCounterLabels        = []string{"name", "direction"}
	allocActiveGauge         *prometheus.GaugeFunc
	promListenerPacketsTotal *prometheus.CounterVec
	promListenerBytesTotal   *prometheus.CounterVec
	promListenerConnsTotal   *prometheus.CounterVec
	promListenerConnsActive  *prometheus.GaugeVec
	promClusterPacketsTotal  *prometheus.CounterVec
	promClusterBytesTotal    *prometheus.CounterVec
	promClusterConnsTotal    *prometheus.CounterVec
	promClusterConnsActive   *prometheus.GaugeVec
)

func Init() {
	// listener stats
	promListenerConnsActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: stunnerNamespace,
		Subsystem: "listener",
		Name:      "connections",
		Help:      "Number of active downstream connections at a listener.",
	}, promConnLabels)
	promListenerConnsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "listener",
		Name:      "connections_total",
		Help:      "Number of downstream connections at a listener.",
	}, promConnLabels)
	promListenerPacketsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "listener",
		Name:      "packets_total",
		Help:      "Number of datagrams sent or received at a listener.",
	}, promCounterLabels)
	promListenerBytesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "listener",
		Name:      "bytes_total",
		Help:      "Number of bytes sent or received at a listener.",
	}, promCounterLabels)

	prometheus.MustRegister(promListenerPacketsTotal)
	prometheus.MustRegister(promListenerBytesTotal)
	prometheus.MustRegister(promListenerConnsTotal)
	prometheus.MustRegister(promListenerConnsActive)

	// cluster stats
	promClusterConnsActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: stunnerNamespace,
		Subsystem: "cluster",
		Name:      "connections",
		Help:      "Number of active upstream connections on behalf of a listener",
	}, promConnLabels)
	promClusterConnsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "cluster",
		Name:      "connections_total",
		Help:      "Number of upstream connections on behalf of a listener.",
	}, promConnLabels)
	promClusterPacketsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "cluster",
		Name:      "packets_total",
		Help:      "Number of datagrams sent to backends or received from backends on behalf of a listener",
	}, promCounterLabels)
	promClusterBytesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "cluster",
		Name:      "bytes_total",
		Help:      "Number of bytes sent to backends or received from backends on behalf of a listener.",
	}, promCounterLabels)

	prometheus.MustRegister(promClusterPacketsTotal)
	prometheus.MustRegister(promClusterBytesTotal)
	prometheus.MustRegister(promClusterConnsTotal)
	prometheus.MustRegister(promClusterConnsActive)
}

func Close() {
	_ = prometheus.Unregister(promListenerPacketsTotal)
	_ = prometheus.Unregister(promListenerBytesTotal)
	_ = prometheus.Unregister(promListenerConnsTotal)
	_ = prometheus.Unregister(promListenerConnsActive)
	_ = prometheus.Unregister(promClusterPacketsTotal)
	_ = prometheus.Unregister(promClusterBytesTotal)
	_ = prometheus.Unregister(promClusterConnsTotal)
	_ = prometheus.Unregister(promClusterConnsActive)
}

func IncrementPackets(n string, c ConnType, d Direction, count uint64) {
	switch c {
	case ListenerType:
		promListenerPacketsTotal.WithLabelValues(n, d.String()).Add(float64(count))
	case ClusterType:
		promClusterPacketsTotal.WithLabelValues(n, d.String()).Add(float64(count))
	}
}

func IncrementBytes(n string, c ConnType, d Direction, count uint64) {
	switch c {
	case ListenerType:
		promListenerBytesTotal.WithLabelValues(n, d.String()).Add(float64(count))
	case ClusterType:
		promClusterBytesTotal.WithLabelValues(n, d.String()).Add(float64(count))
	}
}

func AddConnection(n string, c ConnType) {
	switch c {
	case ListenerType:
		promListenerConnsActive.WithLabelValues(n).Add(1)
		promListenerConnsTotal.WithLabelValues(n).Add(1)
	case ClusterType:
		promClusterConnsActive.WithLabelValues(n).Add(1)
		promClusterConnsTotal.WithLabelValues(n).Add(1)
	}
}

func SubConnection(n string, c ConnType) {
	switch c {
	case ListenerType:
		promListenerConnsActive.WithLabelValues(n).Sub(1)
	case ClusterType:
		promClusterConnsActive.WithLabelValues(n).Sub(1)
	}
}

// func RegisterMetrics(log logging.LeveledLogger, GetAllocationCount func() float64) {
// 	AllocActiveGauge = prometheus.NewGaugeFunc(
// 		prometheus.GaugeOpts{
// 			Name: "stunner_allocations_active",
// 			Help: "Number of active allocations.",
// 		},
// 		GetAllocationCount,
// 	)
// 	if err := prometheus.Register(AllocActiveGauge); err == nil {
// 		log.Debug("GaugeFunc 'stunner_allocations_active' registered.")
// 	} else {
// 		log.Warn("GaugeFunc 'stunner_allocations_active' cannot be registered.")
// 	}
// }

// func UnregisterMetrics(log logging.LeveledLogger) {
// 	if AllocActiveGauge != nil {
// 		if success := prometheus.Unregister(AllocActiveGauge); success {
// 			log.Debug("GaugeFunc 'stunner_allocations_active' unregistered.")
// 			return
// 		}
// 	}
// 	log.Warn("GaugeFunc 'stunner_allocations_active' cannot be unregistered.")
// }
