package object

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"

	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	licensecfg "github.com/l7mp/stunner/pkg/config/license"
)

const DefaultAdminObjectName = "DefaultAdmin"

// Admin holds the bits of STUNner administration that aren't carved out into Health/Metrics/
// Offload. After the refactor Admin owns only Name/LogLevel/UserQuota/License — everything that
// used to drag the whole struct through close+start now lives in the sibling Objects.
type Admin struct {
	Name, LogLevel string
	quota          int
	LicenseManager licensecfg.ConfigManager
	licenseConfig  *stnrv1.LicenseConfig
	reg            Registry
	log            logging.LeveledLogger
}

// NewAdmin creates an Admin object.
func NewAdmin(conf stnrv1.Config, reg Registry, rt *Runtime) (Object, error) {
	a := &Admin{
		LicenseManager: licensecfg.New(rt.Logger.NewLogger("license")),
		reg:            reg,
		log:            rt.Logger.NewLogger("admin"),
	}
	if conf == nil {
		return a, nil
	}
	req, ok := conf.(*stnrv1.AdminConfig)
	if !ok {
		return nil, stnrv1.ErrInvalidConf
	}
	if err := a.Reconcile(req); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Admin) ObjectName() string { return stnrv1.DefaultAdminName }
func (a *Admin) ObjectType() string { return TypeAdmin }

// Extract returns the AdminConfig piece of the full StunnerConfig as-is. Sub-objects
// (Health/Metrics/Offload) Extract their own slices independently.
func (a *Admin) Extract(c *stnrv1.StunnerConfig) (stnrv1.Config, error) {
	cp := c.Admin
	return &cp, nil
}

// GetConfig returns the full AdminConfig, pulling Health/Metrics/Offload pieces from the live
// children stored in the Registry.
func (a *Admin) GetConfig() stnrv1.Config {
	a.log.Tracef("getConfig")

	healthEndpoint := ""
	metricsEndpoint := ""
	offloadEngine := stnrv1.OffloadEngineNone.String()
	offloadInterfaces := []string{}

	if a.reg != nil {
		if h, ok := a.reg.LookupOne(TypeHealth); ok {
			healthEndpoint = h.GetConfig().(*HealthConfig).Endpoint
		}
		if m, ok := a.reg.LookupOne(TypeMetrics); ok {
			metricsEndpoint = m.GetConfig().(*MetricsConfig).Endpoint
		}
		if o, ok := a.reg.LookupOne(TypeOffload); ok {
			off := o.GetConfig().(*OffloadConfig)
			offloadEngine = off.Engine
			offloadInterfaces = make([]string, len(off.Interfaces))
			copy(offloadInterfaces, off.Interfaces)
		}
	}

	return &stnrv1.AdminConfig{
		Name:                a.Name,
		LogLevel:            a.LogLevel,
		MetricsEndpoint:     metricsEndpoint,
		HealthCheckEndpoint: &healthEndpoint,
		UserQuota:           a.quota,
		OffloadEngine:       offloadEngine,
		OffloadInterfaces:   offloadInterfaces,
		LicenseConfig:       a.licenseConfig,
	}
}

func (a *Admin) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (Action, error) {
	req, ok := new.(*stnrv1.AdminConfig)
	if !ok {
		return ActionNone, stnrv1.ErrInvalidConf
	}
	cur := old.(*stnrv1.AdminConfig)
	// Only compare own-state fields. Sub-fields (Health/Metrics/Offload) are inspected by
	// their owning Objects.
	changed := req.Name != cur.Name ||
		req.LogLevel != cur.LogLevel ||
		req.UserQuota != cur.UserQuota ||
		!reflect.DeepEqual(req.LicenseConfig, cur.LicenseConfig)
	// Admin owns no restartable resources of its own: name/loglevel/quota/license can be
	// updated in place.
	if changed {
		return ActionReconcile, nil
	}
	return ActionNone, nil
}

func (a *Admin) Reconcile(conf stnrv1.Config) error {
	req, ok := conf.(*stnrv1.AdminConfig)
	if !ok {
		return stnrv1.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	a.log.Tracef("reconcile: %s", req.String())

	a.Name = req.Name
	a.LogLevel = req.LogLevel
	a.quota = req.UserQuota
	a.LicenseManager.Reconcile(req.LicenseConfig)
	a.licenseConfig = req.LicenseConfig
	return nil
}

func (a *Admin) Start() error       { return nil }
func (a *Admin) Close(_ bool) error { return nil }

func (a *Admin) Status() stnrv1.Status {
	conf := a.GetConfig().(*stnrv1.AdminConfig)
	healthEndpoint := ""
	if conf.HealthCheckEndpoint != nil {
		healthEndpoint = *conf.HealthCheckEndpoint
	}
	intfs := "all"
	if conf.OffloadEngine != stnrv1.OffloadEngineNone.String() && len(conf.OffloadInterfaces) > 0 {
		intfs = strings.Join(conf.OffloadInterfaces, ",")
	}
	return &stnrv1.AdminStatus{
		Name:                a.Name,
		LogLevel:            a.LogLevel,
		MetricsEndpoint:     conf.MetricsEndpoint,
		HealthCheckEndpoint: healthEndpoint,
		UserQuota:           strconv.Itoa(a.quota),
		OffloadStatus:       fmt.Sprintf("%s[%s]", conf.OffloadEngine, intfs),
		LicensingInfo:       a.LicenseManager.Status(),
	}
}

// UserQuota returns the configured per-user TURN allocation quota.
func (a *Admin) UserQuota() int { return a.quota }

// getAddrFromURL is reused by Health and Metrics for parsing URI-style endpoints. It is exported
// here (lowercase, package-internal) so all admin-adjacent Objects share one implementation.
func getAddrFromURL(e string, defaultPort int) (string, string) {
	if e == "" {
		return "", ""
	}
	u, err := url.Parse(e)
	if err != nil {
		return "", ""
	}
	addr := u.Hostname()
	if addr == "" {
		addr = "0.0.0.0"
	}
	port := u.Port()
	if port == "" {
		port = strconv.Itoa(defaultPort)
	}
	addr = addr + ":" + port

	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return addr, path
}
