package telemetry

import (
	"github.com/prometheus/client_golang/prometheus"
)

const (
	stunnerNamespace = "stunner"
)

var (
	ConnLabels           = []string{"name"}
	CounterLabels        = []string{"name", "direction"}
	ListenerPacketsTotal *prometheus.CounterVec
	ListenerBytesTotal   *prometheus.CounterVec
	ListenerConnsTotal   *prometheus.CounterVec
	ListenerConnsActive  *prometheus.GaugeVec
	ClusterPacketsTotal  *prometheus.CounterVec
	ClusterBytesTotal    *prometheus.CounterVec
	// promClusterConnsTotal    *prometheus.CounterVec
	// promClusterConnsActive   *prometheus.GaugeVec
)

func Init() {
	// listener stats
	ListenerConnsActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: stunnerNamespace,
		Subsystem: "listener",
		Name:      "connections",
		Help:      "Number of active downstream connections at a listener.",
	}, ConnLabels)
	ListenerConnsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "listener",
		Name:      "connections_total",
		Help:      "Number of downstream connections at a listener.",
	}, ConnLabels)
	ListenerPacketsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "listener",
		Name:      "packets_total",
		Help:      "Number of datagrams sent or received at a listener.",
	}, CounterLabels)
	ListenerBytesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "listener",
		Name:      "bytes_total",
		Help:      "Number of bytes sent or received at a listener.",
	}, CounterLabels)

	prometheus.MustRegister(ListenerPacketsTotal)
	prometheus.MustRegister(ListenerBytesTotal)
	prometheus.MustRegister(ListenerConnsTotal)
	prometheus.MustRegister(ListenerConnsActive)

	// cluster stats
	// promClusterConnsActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
	// 	Namespace: stunnerNamespace,
	// 	Subsystem: "cluster",
	// 	Name:      "connections",
	// 	Help:      "Number of active upstream connections on behalf of a listener",
	// }, promConnLabels)
	// promClusterConnsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
	// 	Namespace: stunnerNamespace,
	// 	Subsystem: "cluster",
	// 	Name:      "connections_total",
	// 	Help:      "Number of upstream connections on behalf of a listener.",
	// }, promConnLabels)
	ClusterPacketsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "cluster",
		Name:      "packets_total",
		Help:      "Number of datagrams sent to backends or received from backends on behalf of a listener",
	}, CounterLabels)
	ClusterBytesTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: stunnerNamespace,
		Subsystem: "cluster",
		Name:      "bytes_total",
		Help:      "Number of bytes sent to backends or received from backends on behalf of a listener.",
	}, CounterLabels)

	prometheus.MustRegister(ClusterPacketsTotal)
	prometheus.MustRegister(ClusterBytesTotal)
	// prometheus.MustRegister(promClusterConnsTotal)
	// prometheus.MustRegister(promClusterConnsActive)
}

func Close() {
	_ = prometheus.Unregister(ListenerPacketsTotal)
	_ = prometheus.Unregister(ListenerBytesTotal)
	_ = prometheus.Unregister(ListenerConnsTotal)
	_ = prometheus.Unregister(ListenerConnsActive)
	_ = prometheus.Unregister(ClusterPacketsTotal)
	_ = prometheus.Unregister(ClusterBytesTotal)
	// _ = prometheus.Unregister(promClusterConnsTotal)
	// _ = prometheus.Unregister(promClusterConnsActive)
}

func IncrementPackets(n string, c ConnType, d Direction, count uint64) {
	switch c {
	case ListenerType:
		ListenerPacketsTotal.WithLabelValues(n, d.String()).Add(float64(count))
	case ClusterType:
		ClusterPacketsTotal.WithLabelValues(n, d.String()).Add(float64(count))
	}
}

func IncrementBytes(n string, c ConnType, d Direction, count uint64) {
	switch c {
	case ListenerType:
		ListenerBytesTotal.WithLabelValues(n, d.String()).Add(float64(count))
	case ClusterType:
		ClusterBytesTotal.WithLabelValues(n, d.String()).Add(float64(count))
	}
}

func AddConnection(n string, c ConnType) {
	switch c {
	case ListenerType:
		ListenerConnsActive.WithLabelValues(n).Add(1)
		ListenerConnsTotal.WithLabelValues(n).Add(1)
	case ClusterType:
		// promClusterConnsActive.WithLabelValues(n).Add(1)
		// promClusterConnsTotal.WithLabelValues(n).Add(1)
	}
}

func SubConnection(n string, c ConnType) {
	switch c {
	case ListenerType:
		ListenerConnsActive.WithLabelValues(n).Sub(1)
	case ClusterType:
		// promClusterConnsActive.WithLabelValues(n).Sub(1)
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

// func GetListenerPacketsTotal(ch chan prometheus.Metric) {
// 	go func() {
// 		defer close(ch)
// 		promListenerPacketsTotal.Collect(ch)
// 	}()
// }
