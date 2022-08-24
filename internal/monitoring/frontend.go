package monitoring

import (
	"context"
	"net/http"
	"net/url"

	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Frontend interface {
	// Reload frontend based on the configuration change
	Reload(endpoint string, log logging.LeveledLogger) Frontend
	// Start monitoring frontend
	Start()
	// Stop the monitoring frontend
	Stop()
}

type frontendImpl struct {
	httpServer *http.Server
	Endpoint   string
}

func NewFrontend(endpoint string) Frontend {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil
	}

	addr := u.Hostname()
	if addr == "" {
		// omitted value means no monitoring, in this case we
		// return a dummy frontendImpl
		b := &frontendImpl{
			httpServer: nil,
			Endpoint:   endpoint,
		}
		return b
	}

	port := u.Port()
	if port != "" {
		addr = addr + ":" + port
	}

	path := u.EscapedPath()
	if path == "" {
		path = "/metrics"
	}

	mux := http.NewServeMux()
	mux.Handle(path, promhttp.Handler())

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	b := &frontendImpl{
		httpServer: server,
		Endpoint:   endpoint,
	}
	return b
}

func (b *frontendImpl) Reload(endpoint string, log logging.LeveledLogger) Frontend {
	// stop if endpoint is unset
	if endpoint == "" {
		b.Stop()
		return b
	} else {
		// otherwise reinit at new address
		if b.Endpoint != endpoint {
			// new endpoint, restart monitoring server
			b.Stop()
			m := NewFrontend(endpoint)
			b = m.(*frontendImpl)
			b.Start()
		}
	}
	return b
}

func (b *frontendImpl) Start() {
	if b.httpServer == nil {
		return
	}
	// serve Prometheus metrics over HTTP
	go func() {
		b.httpServer.ListenAndServe()
	}()
}

func (b *frontendImpl) Stop() {
	if b.httpServer == nil {
		return
	}
	b.httpServer.Shutdown(context.Background())
}
