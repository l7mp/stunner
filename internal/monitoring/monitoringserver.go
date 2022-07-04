package monitoring

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"

)

// Monitoring is an instance of STUNner monitoring
type MonitoringServer struct {
	httpServer *http.Server
	Endpoint   string
	Group      string
	log        logging.LeveledLogger
}

// NewMonitoring initiates the monitoring subsystem
func NewMonitoringServer(endpoint string, group string, logger logging.LoggerFactory) (*MonitoringServer, error) {
	addr := strings.Split(strings.Replace(endpoint, "http://", "", 1), "/")[0]

	if addr == "" {
		return nil, errors.New(fmt.Sprintf("no host:port info found in %s", endpoint))
	}

	server := &http.Server{
		Addr:    addr,
		Handler: promhttp.Handler(),
	}

	m := &MonitoringServer{
		httpServer: server,
		Endpoint:   endpoint,
		Group:      group,
		log:        logger.NewLogger("stunner-monitoring"),
	}

	return m, nil
}

func (m *MonitoringServer) Init(fp func() float64) {
	RegisterMetrics(m.log, fp)
}

func (m *MonitoringServer) Start() {  // specify config, create new server; move init here?
	// serve Prometheus metrics over HTTP
	go func() {
		m.httpServer.ListenAndServe()
	}()
}

func (m *MonitoringServer) Stop() {
	m.httpServer.Shutdown(context.Background())
}

//TODO: add reconcile <- admin can do it
// receives a config, if diff: close old, start new, else: do nothing

// metrics.go: add metrics that are relevant: create an array

// global monitoring.Metrics
