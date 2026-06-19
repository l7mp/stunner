package object

import (
	"fmt"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	licensecfg "github.com/l7mp/stunner/pkg/config/license"
)

// Admin holds the bits of STUNner administration that aren't carved out into Health/Metrics/
// Offload: Name/LogLevel/UserQuota/License. Everything that used to drag the whole struct
// through close+start lives in the sibling Objects.
type Admin struct {
	name, logLevel string
	quota          int
	licenseManager licensecfg.ConfigManager
	licenseConfig  *stnrv1.LicenseConfig

	// conf is the atomic snapshot of the admin's own fields, read by the quota handler on
	// the allocation path via GetConfig.
	conf atomic.Pointer[stnrv1.AdminConfig]

	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// NewAdmin creates an Admin object.
func NewAdmin(conf stnrv1.Config, rt *runtime.Runtime) (runtime.Object, error) {
	a := &Admin{
		licenseManager: licensecfg.New(rt.Logger.NewLogger("license")),
		rt:             rt,
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

func (a *Admin) Name() string             { return stnrv1.DefaultAdminName }
func (a *Admin) Type() runtime.ObjectType { return runtime.TypeAdmin }

// GetConfig returns the full AdminConfig, combining the admin's own snapshot with the
// Health/Metrics/Offload pieces pulled from the live children. Safe for concurrent use.
func (a *Admin) GetConfig() stnrv1.Config {
	a.log.Tracef("getConfig")

	out := &stnrv1.AdminConfig{}
	if own := a.conf.Load(); own != nil {
		*out = *own
	}

	healthEndpoint := ""
	if hc, ok := a.rt.GetConfig(runtime.TypeHealth, "").(*HealthConfig); ok && hc != nil {
		healthEndpoint = hc.Endpoint
	}
	out.HealthCheckEndpoint = &healthEndpoint

	if mc, ok := a.rt.GetConfig(runtime.TypeMetrics, "").(*MetricsConfig); ok && mc != nil {
		out.MetricsEndpoint = mc.Endpoint
	}

	out.OffloadEngine = stnrv1.OffloadEngineNone.String()
	out.OffloadInterfaces = []string{}
	if oc, ok := a.rt.GetConfig(runtime.TypeOffload, "").(*OffloadConfig); ok && oc != nil {
		out.OffloadEngine = oc.Engine
		out.OffloadInterfaces = append([]string{}, oc.Interfaces...)
	}

	return out
}

func (a *Admin) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv1.AdminConfig)
	if !ok {
		return runtime.ActionNone, stnrv1.ErrInvalidConf
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
		return runtime.ActionReconcile, nil
	}
	return runtime.ActionNone, nil
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

	a.name = req.Name
	a.logLevel = req.LogLevel
	a.quota = req.UserQuota
	a.licenseManager.Reconcile(req.LicenseConfig)
	a.licenseConfig = req.LicenseConfig

	a.conf.Store(&stnrv1.AdminConfig{
		Name:          a.name,
		LogLevel:      a.logLevel,
		UserQuota:     a.quota,
		LicenseConfig: a.licenseConfig,
	})
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
		Name:                conf.Name,
		LogLevel:            conf.LogLevel,
		MetricsEndpoint:     conf.MetricsEndpoint,
		HealthCheckEndpoint: healthEndpoint,
		UserQuota:           strconv.Itoa(conf.UserQuota),
		OffloadStatus:       fmt.Sprintf("%s[%s]", conf.OffloadEngine, intfs),
		LicensingInfo:       a.licenseManager.Status(),
	}
}

// LogLevel returns the configured log level. Safe for concurrent use.
func (a *Admin) LogLevel() string {
	if own := a.conf.Load(); own != nil {
		return own.LogLevel
	}
	return ""
}

// UserQuota returns the configured per-user TURN allocation quota. Safe for concurrent use.
func (a *Admin) UserQuota() int {
	if own := a.conf.Load(); own != nil {
		return own.UserQuota
	}
	return 0
}

// getAddrFromURL is reused by Health and Metrics for parsing URI-style endpoints.
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
