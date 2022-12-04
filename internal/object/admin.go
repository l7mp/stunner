package object

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	// "time"

	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	health "github.com/heptiolabs/healthcheck"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

const DefaultAdminObjectName = "DefaultAdmin"

// Admin is the main object holding STUNner administration info
type Admin struct {
	Name, LogLevel                       string
	DryRun                               bool
	MetricsEndpoint, HealthCheckEndpoint string
	metricsServer, healthCheckServer     *http.Server
	health                               health.Handler
	log                                  logging.LeveledLogger
}

// NewAdmin creates a new Admin object. Requires a server restart (returns
// v1alpha1.ErrRestartRequired)
func NewAdmin(conf v1alpha1.Config, dryRun bool, rc health.Check, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*v1alpha1.AdminConfig)
	if !ok {
		return nil, v1alpha1.ErrInvalidConf
	}

	admin := Admin{
		DryRun: dryRun,
		health: health.NewHandler(),
		log:    logger.NewLogger("stunner-admin"),
	}
	admin.log.Tracef("NewAdmin: %#v", req)

	// health checker
	// liveness probe always succeeds once we got here
	admin.health.AddLivenessCheck("server-alive", func() error { return nil })
	admin.health.AddReadinessCheck("server-ready", rc)

	if err := admin.Reconcile(req); err != nil && err != v1alpha1.ErrRestartRequired {
		return nil, err
	}

	return &admin, nil
}

// Inspect examines whether a configuration change on the object would require a restart. An empty
// new-config means it is about to be deleted, an empty old-config means it is to be deleted,
// otherwise it will be reconciled from the old configuration to the new one
func (a *Admin) Inspect(old, new v1alpha1.Config) bool {
	return false
}

// Reconcile updates the authenticator for a new configuration. Requires a valid reconciliation
// request
func (a *Admin) Reconcile(conf v1alpha1.Config) error {
	req, ok := conf.(*v1alpha1.AdminConfig)
	if !ok {
		return v1alpha1.ErrInvalidConf
	}

	if err := req.Validate(); err != nil {
		return err
	}

	a.log.Tracef("Reconcile: %#v", req)

	a.Name = req.Name
	a.LogLevel = req.LogLevel

	// health-check server reconciliation errors are FATAL (may break Kubernetes
	// liveness/readiness checks): return any error encountered
	if err := a.reconcileHealthCheck(req); err != nil {
		return err
	}

	// metrics server reconciliation errors are NOT FATAL: just warn if something goes wrong
	// but otherwise go on with reconciliation
	if err := a.reconcileMetrics(req); err != nil {
		a.log.Warnf("error reconciling metrics server:", err.Error())
	}

	return nil
}

// Name returns the name of the object
func (a *Admin) ObjectName() string {
	// singleton!
	return v1alpha1.DefaultAdminName
}

// GetConfig returns the configuration of the running object
func (a *Admin) GetConfig() v1alpha1.Config {
	a.log.Tracef("GetConfig")
	return &v1alpha1.AdminConfig{
		Name:                a.Name,
		LogLevel:            a.LogLevel,
		MetricsEndpoint:     a.MetricsEndpoint,
		HealthCheckEndpoint: a.HealthCheckEndpoint,
	}
}

// Close closes the Admin object
func (a *Admin) Close() error {
	a.log.Tracef("Close")
	if a.healthCheckServer != nil {
		if err := a.healthCheckServer.Close(); err != nil {
			hcAddr := getHealthAddr(a.HealthCheckEndpoint)
			a.log.Debugf("error closing healthcheck server http://%s: %s",
				hcAddr, err.Error())
		}
	}
	if a.metricsServer != nil {
		if err := a.metricsServer.Close(); err != nil {
			mAddr, mPath := getMetricsAddr(a.MetricsEndpoint)
			a.log.Debugf("error closing metrics server http://%s/%s: %s",
				mAddr, mPath, a.MetricsEndpoint, err.Error())
		}
	}
	return nil
}

func (a *Admin) reconcileHealthCheck(req *v1alpha1.AdminConfig) error {
	if req.HealthCheckEndpoint != a.HealthCheckEndpoint && !a.DryRun {

		hcAddr := getHealthAddr(a.HealthCheckEndpoint)
		hcEndpoint := fmt.Sprintf("http://%s", hcAddr)

		// close old server if exists
		if a.healthCheckServer != nil {
			a.log.Tracef("closing healthcheck server at %s", hcEndpoint)
			if err := a.healthCheckServer.Close(); err != nil {
				return fmt.Errorf("error stopping healthcheck server at %s: %w",
					hcEndpoint, err)
			}
			a.healthCheckServer = nil
		}

		hcAddr = getHealthAddr(req.HealthCheckEndpoint)
		hcEndpoint = fmt.Sprintf("http://%s", hcAddr)

		// only start new server if necessary
		if hcAddr == "" {
			return nil
		}

		a.log.Tracef("starting healthcheck server at %s", hcEndpoint)
		a.healthCheckServer = &http.Server{
			Addr:    hcAddr,
			Handler: a.health,
		}

		// we separate Listen() and Serve(), so that we can return errors from the listener
		ln, err := net.Listen("tcp", hcAddr)
		if err != nil {
			return fmt.Errorf("cannot start healthcheck server at %s: %w",
				hcEndpoint, err)
		}

		go func() {
			if err := a.healthCheckServer.Serve(ln); err != nil {
				if errors.Is(err, http.ErrServerClosed) {
					a.log.Tracef("healthcheck server: normal shutdown")
				} else {
					a.log.Warnf("healthcheck server error at %s: %s",
						hcEndpoint, err.Error())
					a.healthCheckServer = nil
				}
			}
		}()
	}
	a.HealthCheckEndpoint = req.HealthCheckEndpoint

	return nil
}

func (a *Admin) reconcileMetrics(req *v1alpha1.AdminConfig) error {
	if a.MetricsEndpoint != req.MetricsEndpoint && !a.DryRun {

		mAddr, mPath := getMetricsAddr(a.MetricsEndpoint)
		mEndpoint := fmt.Sprintf("http://%s/%s", mAddr, mPath)

		// close old server if exists
		if a.metricsServer != nil {
			a.log.Tracef("closing metrics server at %s", mEndpoint)
			if err := a.metricsServer.Shutdown(context.Background()); err != nil {
				return fmt.Errorf("error stopping metrics server at %s: %w",
					mEndpoint, err)
			}
			a.metricsServer = nil
		}

		// only start new server if necessary
		if req.MetricsEndpoint == "" {
			return nil
		}

		a.log.Tracef("starting metrics server at %s", req.MetricsEndpoint)

		mAddr, mPath = getMetricsAddr(req.MetricsEndpoint)
		mEndpoint = fmt.Sprintf("http://%s/%s", mAddr, mPath)

		mux := http.NewServeMux()
		mux.Handle(mPath, promhttp.Handler())
		a.metricsServer = &http.Server{
			Addr:    mAddr,
			Handler: mux,
		}

		// we separate Listen() and Serve(), so that we can return errors from the listener
		ln, err := net.Listen("tcp", mAddr)
		if err != nil {
			return fmt.Errorf("cannot start metrics server at %s: %w",
				mEndpoint, err)
		}

		go func() {
			if err := a.metricsServer.Serve(ln); err != nil {
				if errors.Is(err, http.ErrServerClosed) {
					a.log.Tracef("metrics server: normal shutdown")
				} else {
					a.log.Warnf("metrics server error at %s: %s",
						mEndpoint, err.Error())
					a.metricsServer = nil
				}
			}
		}()
	}
	a.MetricsEndpoint = req.MetricsEndpoint

	return nil
}

// AdminFactory can create now Admin objects
type AdminFactory struct {
	dry    bool
	rc     health.Check
	logger logging.LoggerFactory
}

// NewAdminFactory creates a new factory for Admin objects
func NewAdminFactory(dryRun bool, rc health.Check, logger logging.LoggerFactory) Factory {
	return &AdminFactory{dry: dryRun, rc: rc, logger: logger}
}

// New can produce a new Admin object from the given configuration. A nil config will create an
// empty admin object (useful for creating throwaway objects for, e.g., calling Inpect)
func (f *AdminFactory) New(conf v1alpha1.Config) (Object, error) {
	if conf == nil {
		return &Admin{}, nil
	}

	return NewAdmin(conf, f.dry, f.rc, f.logger)
}

func getHealthAddr(e string) string {
	// health-check disabled
	if e == "" {
		return ""
	}

	u, err := url.Parse(e)

	// this should never happen: endpoint is validated
	if err != nil {
		return ""
	}

	addr := u.Hostname()
	if addr == "" {
		addr = "0.0.0.0"
	}

	port := u.Port()
	if port == "" {
		port = fmt.Sprintf("%d", v1alpha1.DefaultHealthCheckPort)
	}

	return addr + ":" + port
}

func getMetricsAddr(e string) (string, string) {
	// metric scraping disabled
	if e == "" {
		return "", ""
	}

	u, err := url.Parse(e)

	// this should never happen: endpoint is validated
	if err != nil {
		return "", ""
	}

	addr := u.Hostname()
	if addr == "" {
		addr = "0.0.0.0"
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

	return addr, path
}
