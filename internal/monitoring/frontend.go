package monitoring

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

type Frontend interface {
	// Reload frontend based on the configuration change
	Reconcile(endpoint string) error
	// Start the monitoring frontend
	Start()
	// Stop the monitoring frontend
	Stop()
	// Get current endpoint
	GetEndpoint() string
}

type frontendImpl struct {
	httpServer *http.Server
	Endpoint   string
	dryRun     bool
	log        logging.LeveledLogger
}

func NewFrontend(endpoint string, dryRun bool, logger logging.LoggerFactory) Frontend {
	log := logger.NewLogger("prometheus-frontend")
	log.Tracef("NewFrontend")

	return &frontendImpl{
		httpServer: nil,
		Endpoint:   endpoint,
		dryRun:     dryRun,
		log:        log,
	}
}

// only configuration errors are critical, Prom client HTTP frontend start/stop errors are
// suppredded: we don't want the stunner to rollback the old config when the HTTP server cannot
// start for some reason (e.g., EBUSY)
func (b *frontendImpl) Reconcile(endpoint string) error {
	b.log.Tracef("Reconcile: %s", endpoint)

	// stop if endpoint is unset
	if endpoint == "" {
		b.Stop()
		return nil
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return err
	}

	addr := u.Hostname()
	if addr == "" {
		return nil
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

	// Handle dry run
	if b.dryRun {
		b.Endpoint = endpoint
		return nil
	}

	if b.Endpoint == endpoint {
		// nothing to change
		return nil
	}

	mux := http.NewServeMux()
	mux.Handle(path, promhttp.Handler())

	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	b.Stop()
	b.Endpoint = endpoint
	b.httpServer = server
	b.Start()

	return nil
}

func (b *frontendImpl) Start() {
	b.log.Tracef("Starting prometheus client frontend at endpoint %s", b.Endpoint)
	if b.httpServer == nil {
		return
	}
	// serve Prometheus metrics over HTTP
	go func() {
		err := b.httpServer.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			b.log.Tracef("Prometheus client frontend: normal shutdown")
		} else if err != nil {
			b.log.Warnf("Cannot start Prometheus client frontend: %s", err.Error())
			b.httpServer = nil
		}
	}()
}

func (b *frontendImpl) Stop() {
	b.log.Tracef("Stopping prometheus client frontend %s", b.Endpoint)
	if b.httpServer == nil {
		return
	}
	// ignore errors
	if err := b.httpServer.Shutdown(context.Background()); err != nil {
		b.log.Warnf("Error in stopping prometheus client frontend %s", b.Endpoint)
	}
}

func (b *frontendImpl) GetEndpoint() string {
	if b == nil {
		return ""
	}
	return b.Endpoint
}
