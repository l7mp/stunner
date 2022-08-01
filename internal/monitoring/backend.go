package monitoring

import (
	"context"
	"net/http"
	"net/url"

	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

type Backend interface {
	// Reload backend based on the configuration change
	Reload(endpoint string, log logging.LeveledLogger) Backend
	// Start monitoring backend
	Start()
	// Stop the monitoring backend
	Stop()
}

type backendImpl struct {
	httpServer *http.Server
	Endpoint   string
}

func NewBackend(endpoint string) Backend {
	u, err := url.Parse(endpoint)
	if err != nil {
		return nil
	}

	addr := u.Hostname()
	if addr == "" {
		// omitted value means no monitoring, in this case we
		// return a dummy backendImpl
		b := &backendImpl{
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
	b := &backendImpl{
		httpServer: server,
		Endpoint:   endpoint,
	}
	return b
}

func (b *backendImpl) Reload(endpoint string, log logging.LeveledLogger) Backend {
	// stop if endpoint is unset
	if endpoint == "" {
		b.Stop()
		return b
	} else {
		// otherwise reinit at new address
		if b.Endpoint != endpoint {
			// new endpoint, restart monitoring server
			b.Stop()
			m := NewBackend(endpoint)
			b = m.(*backendImpl)
			b.Start()
		}
	}
	return b
}

func (b *backendImpl) Start() {
	if b.httpServer == nil {
		return
	}
	// serve Prometheus metrics over HTTP
	go func() {
		b.httpServer.ListenAndServe()
	}()
}

func (b *backendImpl) Stop() {
	if b.httpServer == nil {
		return
	}
	b.httpServer.Shutdown(context.Background())
}
