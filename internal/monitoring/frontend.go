package monitoring

import (
	"context"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

type Frontend interface {
	// Reload frontend based on the configuration change
	Reload(endpoint string, log logging.LeveledLogger) Frontend
	// Start monitoring frontend
	Start(log logging.LeveledLogger)
	// Stop the monitoring frontend
	Stop(log logging.LeveledLogger)
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
	if port == "" {
		port = strconv.Itoa(v1alpha1.DefaultMetricsPort)
	}
	addr = addr + ":" + port

	path := u.EscapedPath()
	if path == "" {
		path = "/"
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
		b.Stop(log)
		return b
	} else {
		// otherwise reinit at new address
		if b.Endpoint != endpoint {
			// new endpoint, restart monitoring server
			b.Stop(log)
			m := NewFrontend(endpoint)
			b = m.(*frontendImpl)
			b.Start(log)
		}
	}
	return b
}

func (b *frontendImpl) Start(log logging.LeveledLogger) {
	if b.httpServer == nil {
		return
	}
	// serve Prometheus metrics over HTTP
	go func() {
		if err := b.httpServer.ListenAndServe(); err != nil {
			log.Warn("Error in metrics HTTP endpoint operation.")
		}
	}()
	log.Debug("Started metrics HTTP endpoint.")
}

func (b *frontendImpl) Stop(log logging.LeveledLogger) {
	if b.httpServer == nil {
		return
	}
	if err := b.httpServer.Shutdown(context.Background()); err != nil {
		log.Warn("Error in metrics HTTP endpoint shutdown.")
	}
	log.Debug("Succesful metrics HTTP endpoint shutdown.")
}
