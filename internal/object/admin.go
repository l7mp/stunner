package object

import (
	"fmt"
	"net"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Admin holds the bits of STUNner administration that aren't carved out into
// Health/Metrics/Offload: Name/LogLevel/UserQuota/License.
type Admin struct {
	name, logLevel string
	quota          int
	licenseConfig  *stnrv2.LicenseConfig

	// conf is the atomic snapshot of the admin's own fields, read by the quota handler on
	// the allocation path via GetConfig.
	conf atomic.Pointer[stnrv2.AdminConfig]

	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// NewAdmin creates an Admin object.
func NewAdmin(conf stnrv2.Config, rt *runtime.Runtime) (runtime.Object, error) {
	a := &Admin{
		rt:  rt,
		log: rt.Logger.NewLogger("admin"),
	}
	if conf == nil {
		return a, nil
	}
	req, ok := conf.(*stnrv2.AdminConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	if err := a.Reconcile(req); err != nil {
		return nil, err
	}
	return a, nil
}

func (a *Admin) Name() string             { return stnrv2.DefaultAdminName }
func (a *Admin) Type() runtime.ObjectType { return runtime.TypeAdmin }

// GetConfig returns the full AdminConfig, combining the admin's own snapshot with the
// Health/Metrics/Offload pieces pulled from the live children. Safe for concurrent use.
func (a *Admin) GetConfig() stnrv2.Config {
	a.log.Tracef("getConfig")

	out := &stnrv2.AdminConfig{}
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

	out.OffloadEngine = stnrv2.OffloadEngineNone.String()
	out.OffloadInterfaces = []string{}
	if oc, ok := a.rt.GetConfig(runtime.TypeOffload, "").(*OffloadConfig); ok && oc != nil {
		out.OffloadEngine = oc.Engine
		out.OffloadInterfaces = append([]string{}, oc.Interfaces...)
	}

	return out
}

func (a *Admin) Inspect(old, new stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv2.AdminConfig)
	if !ok {
		return runtime.ActionNone, stnrv2.ErrInvalidConf
	}
	cur := old.(*stnrv2.AdminConfig)
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

func (a *Admin) Reconcile(conf stnrv2.Config) error {
	req, ok := conf.(*stnrv2.AdminConfig)
	if !ok {
		return stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	a.log.Tracef("reconcile: %s", req.String())

	a.name = req.Name
	a.logLevel = req.LogLevel
	a.quota = req.UserQuota
	a.rt.License.Reconcile(req.LicenseConfig)
	a.licenseConfig = req.LicenseConfig

	a.conf.Store(&stnrv2.AdminConfig{
		Name:          a.name,
		LogLevel:      a.logLevel,
		UserQuota:     a.quota,
		LicenseConfig: a.licenseConfig,
	})
	return nil
}

func (a *Admin) Start() error       { return nil }
func (a *Admin) Close(_ bool) error { return nil }

func (a *Admin) Status() stnrv2.Status {
	conf := a.GetConfig().(*stnrv2.AdminConfig)
	healthEndpoint := ""
	if conf.HealthCheckEndpoint != nil {
		healthEndpoint = *conf.HealthCheckEndpoint
	}
	intfs := "all"
	if conf.OffloadEngine != stnrv2.OffloadEngineNone.String() && len(conf.OffloadInterfaces) > 0 {
		intfs = strings.Join(conf.OffloadInterfaces, ",")
	}
	return &stnrv2.AdminStatus{
		Name:                conf.Name,
		LogLevel:            conf.LogLevel,
		MetricsEndpoint:     conf.MetricsEndpoint,
		HealthCheckEndpoint: healthEndpoint,
		UserQuota:           strconv.Itoa(conf.UserQuota),
		OffloadStatus:       fmt.Sprintf("%s[%s]", conf.OffloadEngine, intfs),
		LicensingInfo:       a.rt.License.Status(),
	}
}

// LogLevel returns the configured log level. Safe for concurrent use.
func (a *Admin) LogLevel() string {
	if own := a.conf.Load(); own != nil {
		return own.LogLevel
	}
	return ""
}

// getAddrFromURL is reused by Health and Metrics for parsing URI-style endpoints. A host-less
// endpoint (like the default "http://:8086") yields a host-less listen address (":8086").
func getAddrFromURL(e string, defaultPort int) (string, string) {
	if e == "" {
		return "", ""
	}
	u, err := url.Parse(e)
	if err != nil {
		return "", ""
	}
	port := u.Port()
	if port == "" {
		port = strconv.Itoa(defaultPort)
	}
	addr := net.JoinHostPort(u.Hostname(), port)

	path := u.EscapedPath()
	if path == "" {
		path = "/"
	}
	return addr, path
}
