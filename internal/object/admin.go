package object

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"

	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	licensecfg "github.com/l7mp/stunner/pkg/config/license"
)

const DefaultAdminObjectName = "DefaultAdmin"

// Admin is the main object holding STUNner administration info.
type Admin struct {
	Name, LogLevel                       string
	DryRun                               bool
	MetricsEndpoint, HealthCheckEndpoint string
	metricsServer, healthCheckServer     *http.Server
	health                               *http.ServeMux
	quota                                int
	LicenseManager                       licensecfg.ConfigManager
	licenseConfig                        *stnrv1.LicenseConfig
	log                                  logging.LeveledLogger
}

// NewAdmin creates a new Admin object.
func NewAdmin(conf stnrv1.Config, dryRun bool, rc ReadinessHandler, status StatusHandler, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*stnrv1.AdminConfig)
	if !ok {
		return nil, stnrv1.ErrInvalidConf
	}

	admin := Admin{
		DryRun:         dryRun,
		health:         http.NewServeMux(),
		LicenseManager: licensecfg.New(logger.NewLogger("license")),
		log:            logger.NewLogger("admin"),
	}
	admin.log.Tracef("NewAdmin: %s", req.String())

	// health checker
	// liveness probe always succeeds once we got here
	admin.health.HandleFunc("/live", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}\n")) //nolint:errcheck
	})

	// readniness checker calls the checker from the factory
	admin.health.HandleFunc("/ready", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if err := rc(); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)

			w.Write([]byte(fmt.Sprintf("{\"status\":%d,\"message\":\"%s\"}\n", //nolint:errcheck
				http.StatusServiceUnavailable, err.Error())))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(fmt.Sprintf("{\"status\":%d,\"message\":\"%s\"}\n", //nolint:errcheck
				http.StatusOK, "READY")))
		}
	})

	// status handler returns the status
	admin.health.HandleFunc("/status", func(w http.ResponseWriter, req *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if js, err := json.Marshal(status()); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf("{\"status\":%d,\"message\":\"%s\"}\n", //nolint:errcheck
				http.StatusInternalServerError, err.Error())))
		} else {
			w.WriteHeader(http.StatusOK)
			w.Write(js) //nolint:errcheck
		}
	})

	if err := admin.Reconcile(req); err != nil && !errors.Is(err, ErrRestartRequired) {
		return nil, err
	}

	return &admin, nil
}

// Inspect examines whether a configuration change requires a reconciliation (returns true if it
// does) or restart (returns ErrRestartRequired).
func (a *Admin) Inspect(old, new, full stnrv1.Config) (bool, error) {
	return !old.DeepEqual(new), nil
}

// Reconcile updates the authenticator for a new configuration. Requires a valid reconciliation
// request.
func (a *Admin) Reconcile(conf stnrv1.Config) error {
	req, ok := conf.(*stnrv1.AdminConfig)
	if !ok {
		return stnrv1.ErrInvalidConf
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

	a.quota = req.UserQuota

	a.LicenseManager.Reconcile(req.LicenseConfig)
	a.licenseConfig = req.LicenseConfig

	return nil
}

// ObjectName returns the name of the object.
func (a *Admin) ObjectName() string {
	return stnrv1.DefaultAdminName
}

// ObjectType returns the type of the object.
func (a *Admin) ObjectType() string {
	return "admin"
}

// GetConfig returns the configuration of the running object.
func (a *Admin) GetConfig() stnrv1.Config {
	a.log.Tracef("GetConfig")

	// use a copy when taking the pointer: we don't want anyone downstream messing with our own
	// copies
	h := a.HealthCheckEndpoint

	return &stnrv1.AdminConfig{
		Name:                a.Name,
		LogLevel:            a.LogLevel,
		MetricsEndpoint:     a.MetricsEndpoint,
		HealthCheckEndpoint: &h,
		UserQuota:           a.quota,
		LicenseConfig:       a.licenseConfig,
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

// Status returns the status of the object.
func (a *Admin) Status() stnrv1.Status {
	s := stnrv1.AdminStatus{
		Name:                a.Name,
		LogLevel:            a.LogLevel,
		MetricsEndpoint:     a.MetricsEndpoint,
		HealthCheckEndpoint: a.HealthCheckEndpoint,
		UserQuota:           fmt.Sprintf("%d", a.quota),
		LicensingInfo:       a.LicenseManager.Status().String(),
	}
	return &s
}

func (a *Admin) reconcileMetrics(req *stnrv1.AdminConfig) error {
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
			return fmt.Errorf("failed to stop metrics server at %s: %w",
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
func (a *Admin) reconcileHealthCheck(req *stnrv1.AdminConfig) error {
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
	rc     ReadinessHandler
	status StatusHandler
	logger logging.LoggerFactory
}

// NewAdminFactory creates a new factory for Admin objects
func NewAdminFactory(dryRun bool, rc ReadinessHandler, status StatusHandler, logger logging.LoggerFactory) Factory {
	return &AdminFactory{dry: dryRun, rc: rc, status: status, logger: logger}
}

// New can produce a new Admin object from the given configuration. A nil config will create an
// empty admin object (useful for creating throwaway objects for, e.g., calling Inpect)
func (f *AdminFactory) New(conf stnrv1.Config) (Object, error) {
	if conf == nil {
		return &Admin{}, nil
	}

	return NewAdmin(conf, f.dry, f.rc, f.status, f.logger)
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
		port = fmt.Sprintf("%d", stnrv1.DefaultHealthCheckPort)
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
		port = strconv.Itoa(stnrv1.DefaultMetricsPort)
	}
	addr = addr + ":" + port

	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}

	return addr, path
}
