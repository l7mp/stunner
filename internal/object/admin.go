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

// Admin is the main object holding STUNner administration info.
type Admin struct {
	Name, LogLevel                       string
	DryRun                               bool
	MetricsEndpoint, HealthCheckEndpoint string
	metricsServer, healthCheckServer     *http.Server
	health                               health.Handler
	log                                  logging.LeveledLogger
}

// NewAdmin creates a new Admin object.
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
	admin.log.Tracef("NewAdmin: %s", req.String())

	// health checker
	// liveness probe always succeeds once we got here
	admin.health.AddLivenessCheck("server-alive", func() error { return nil })
	admin.health.AddReadinessCheck("server-ready", rc)

	if err := admin.Reconcile(req); err != nil && !errors.Is(err, ErrRestartRequired) {
		return nil, err
	}

	return &admin, nil
}

// Inspect examines whether a configuration change requires a reconciliation (returns true if it
// does) or restart (returns ErrRestartRequired).
func (a *Admin) Inspect(old, new, full v1alpha1.Config) (bool, error) {
	return !old.DeepEqual(new), nil
}

// Reconcile updates the authenticator for a new configuration. Requires a valid reconciliation
// request.
func (a *Admin) Reconcile(conf v1alpha1.Config) error {
	req, ok := conf.(*v1alpha1.AdminConfig)
	if !ok {
		return v1alpha1.ErrInvalidConf
	}

	if err := req.Validate(); err != nil {
		return err
	}

	a.log.Tracef("Reconcile: %s", req.String())

	a.Name = req.Name
	a.LogLevel = req.LogLevel

	// metrics server reconciliation errors are NOT FATAL: just warn if something goes wrong
	// but otherwise go on with reconciliation
	if err := a.reconcileMetrics(req); err != nil {
		a.log.Warnf("error reconciling metrics server:", err.Error())
	}

	// health-check server reconciliation errors are FATAL (may break Kubernetes
	// liveness/readiness checks): return any error encountered
	if err := a.reconcileHealthCheck(req); err != nil {
		return err
	}

	return nil
}

// ObjectName returns the name of the object.
func (a *Admin) ObjectName() string {
	return v1alpha1.DefaultAdminName
}

// ObjectType returns the type of the object.
func (a *Admin) ObjectType() string {
	return "admin"
}

// GetConfig returns the configuration of the running object.
func (a *Admin) GetConfig() v1alpha1.Config {
	a.log.Tracef("GetConfig")

	// use a copy when taking the pointer: we don't want anyone downstream messing with our own
	// copies
	h := a.HealthCheckEndpoint

	return &v1alpha1.AdminConfig{
		Name:                a.Name,
		LogLevel:            a.LogLevel,
		MetricsEndpoint:     a.MetricsEndpoint,
		HealthCheckEndpoint: &h,
	}
}

// Close closes the Admin object.
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

func (a *Admin) reconcileMetrics(req *v1alpha1.AdminConfig) error {
	a.log.Trace("reconcileMetrics")

	if a.DryRun {
		goto end
	}

	// close if: running and new endpoint is empty or differs from the old one
	if a.metricsServer != nil && req.MetricsEndpoint != a.MetricsEndpoint {
		mAddr, mPath := getMetricsAddr(a.MetricsEndpoint)
		mEndpoint := fmt.Sprintf("http://%s/%s", mAddr, mPath)

		a.log.Tracef("closing metrics server at %s", mEndpoint)

		if err := a.metricsServer.Shutdown(context.Background()); err != nil {
			return fmt.Errorf("error stopping metrics server at %s: %w",
				mEndpoint, err)
		}
		a.metricsServer = nil
	}

	// start if: new endpoint differs from the old one
	if req.MetricsEndpoint != a.MetricsEndpoint && req.MetricsEndpoint != "" {
		a.log.Tracef("starting metrics server at %s", req.MetricsEndpoint)

		mAddr, mPath := getMetricsAddr(req.MetricsEndpoint)
		mEndpoint := fmt.Sprintf("http://%s/%s", mAddr, mPath)

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

end:
	a.MetricsEndpoint = req.MetricsEndpoint

	return nil
}

// req MUST be validated!
func (a *Admin) reconcileHealthCheck(req *v1alpha1.AdminConfig) error {
	a.log.Trace("reconcileHealthCheck")

	// if req is validated then either
	// (1) *req.HealthCheckEndpoint="", which means caller does not want healthchecking, or
	// (2) *req.HealthCheckEndpoint=default | what is specified by the caller
	if req.HealthCheckEndpoint == nil {
		return fmt.Errorf("internal error: processing unvalidated AdminConfig: %#v", req)
	}

	if a.DryRun {
		goto end
	}

	// close if: running and new endpoint is empty or differs from the old one
	if a.healthCheckServer != nil && *req.HealthCheckEndpoint != a.HealthCheckEndpoint {
		hcAddr := getHealthAddr(a.HealthCheckEndpoint)
		hcEndpoint := fmt.Sprintf("http://%s", hcAddr)

		a.log.Tracef("closing healthCheck server at %s", hcEndpoint)

		if err := a.healthCheckServer.Close(); err != nil {
			return fmt.Errorf("error stopping healthCheck server at %s: %w",
				hcEndpoint, err)
		}
		a.healthCheckServer = nil
	}

	// start if: new endpoint differs from the old one
	if *req.HealthCheckEndpoint != a.HealthCheckEndpoint && *req.HealthCheckEndpoint != "" {
		hcAddr := getHealthAddr(*req.HealthCheckEndpoint)
		hcEndpoint := fmt.Sprintf("http://%s", hcAddr)

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

end:
	a.HealthCheckEndpoint = ""
	if req.HealthCheckEndpoint != nil {
		a.HealthCheckEndpoint = *req.HealthCheckEndpoint
	}

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
