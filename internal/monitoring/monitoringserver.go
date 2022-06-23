package monitoring

import (
	"context"
	"fmt"
	"net/http"

	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/l7mp/stunner/internal/object"
)

// Monitoring is an instance of STUNner monitoring
type MonitoringServer struct {
	httpServer *http.Server
	serveMux   *http.ServeMux
	Port       int
	Url        string
	Group      string
	log        logging.LeveledLogger
}

// NewMonitoring initiates the monitoring subsystem
func NewMonitoringServer(o *object.Monitoring) (*MonitoringServer, error) {
	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", o.Port),
		Handler: promhttp.Handler(),
	}

	m := &MonitoringServer{
		httpServer: server,
		Port:       o.Port,
		Url:        o.Url,
		Group:      o.Group,
		//log:        o.log,
	}

	return m, nil
}

func (m *MonitoringServer) Init(fp func() float64) {
	if err := prometheus.Register(prometheus.NewGaugeFunc(
		prometheus.GaugeOpts{
			Name: "allocation_count",
			Help: "Number of active allocations.",
		},
		fp,
	)); err == nil {
		//FIXME m.log.Debug("GaugeFunc 'allocation' registered.")
		fmt.Println("GaugeFunc 'allocation' registered.")
	} else {
		//FIXME m.log.Warn("GaugeFunc 'allocation' cannot be registered (already registered?).")
		fmt.Println("GaugeFunc 'allocation' cannot be registered (already registered?).")
	}
}

func (m *MonitoringServer) Start() {
	// serve Prometheus mertics over HTTP
	//m.httpServer.Shutdown(context.Background())
	go func() {
		//http.Handle(m.Url, promhttp.Handler())
		m.httpServer.ListenAndServe()
	}()
}

func (m *MonitoringServer) Stop() {
	m.httpServer.Shutdown(context.Background())
}
