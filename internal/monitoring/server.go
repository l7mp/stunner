package monitoring

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Monitoring is an instance of STUNner monitoring
type MonitoringServer struct {
	httpServer *http.Server
	Endpoint   string
	Group      string
}

// NewMonitoring initiates the monitoring subsystem
func NewMonitoringServer(endpoint string, group string) (*MonitoringServer, error) {
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
	}

	return m, nil
}

func (m *MonitoringServer) Start() { // specify config, create new server; move init here?
	if m.Endpoint == "" {
		return
	}
	// serve Prometheus metrics over HTTP
	go func() {
		m.httpServer.ListenAndServe()
	}()
}

func (m *MonitoringServer) Stop() {
	if m.httpServer == nil {
		return
	}
	m.httpServer.Shutdown(context.Background())
}
