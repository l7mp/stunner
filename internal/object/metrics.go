package object

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"

	"github.com/pion/logging"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Metrics is the Object that owns the Prometheus metrics HTTP server. Carving it out of Admin
// makes it independently restartable.
type Metrics struct {
	endpoint string
	server   *http.Server
	servAddr net.Addr
	dryRun   bool
	reg      Registry
	log      logging.LeveledLogger
}

// MetricsConfig is the typed subconfig consumed by Metrics. An empty endpoint disables the server.
type MetricsConfig struct {
	Endpoint string `json:"endpoint,omitempty"`
}

func (c *MetricsConfig) Validate() error    { return nil }
func (c *MetricsConfig) ConfigName() string { return DefaultMetricsName }
func (c *MetricsConfig) DeepEqual(other stnrv1.Config) bool {
	o, ok := other.(*MetricsConfig)
	if !ok {
		return false
	}
	return c.Endpoint == o.Endpoint
}
func (c *MetricsConfig) DeepCopyInto(dst stnrv1.Config) {
	d, ok := dst.(*MetricsConfig)
	if !ok {
		return
	}
	*d = *c
}
func (c *MetricsConfig) String() string {
	return fmt.Sprintf("MetricsConfig{endpoint=%q}", c.Endpoint)
}

// NewMetrics creates a Metrics object.
func NewMetrics(conf stnrv1.Config, reg Registry, rt *Runtime) (Object, error) {
	m := &Metrics{
		dryRun: rt.DryRun,
		reg:    reg,
		log:    rt.Logger.NewLogger("metrics"),
	}
	if conf == nil {
		return m, nil
	}
	req, ok := conf.(*MetricsConfig)
	if !ok {
		return nil, stnrv1.ErrInvalidConf
	}
	if err := m.Reconcile(req); err != nil {
		return nil, err
	}
	return m, nil
}

func (m *Metrics) ObjectName() string { return DefaultMetricsName }
func (m *Metrics) ObjectType() string { return TypeMetrics }

func (m *Metrics) Extract(c *stnrv1.StunnerConfig) (stnrv1.Config, error) {
	return &MetricsConfig{Endpoint: c.Admin.MetricsEndpoint}, nil
}

func (m *Metrics) GetConfig() stnrv1.Config { return &MetricsConfig{Endpoint: m.endpoint} }
func (m *Metrics) Status() stnrv1.Status    { return m.GetConfig() }

func (m *Metrics) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (Action, error) {
	req, ok := new.(*MetricsConfig)
	if !ok {
		return ActionNone, stnrv1.ErrInvalidConf
	}
	cur := old.(*MetricsConfig)
	if reflect.DeepEqual(req, cur) {
		if (req.Endpoint != "" && m.server == nil) || (req.Endpoint == "" && m.server != nil) {
			return ActionRestart, nil
		}
		return ActionNone, nil
	}
	return ActionRestart, nil
}

func (m *Metrics) Reconcile(conf stnrv1.Config) error {
	req, ok := conf.(*MetricsConfig)
	if !ok {
		return stnrv1.ErrInvalidConf
	}
	m.endpoint = req.Endpoint
	return nil
}

func (m *Metrics) Start() error {
	if m.dryRun {
		return nil
	}
	if m.endpoint == "" {
		return nil
	}
	addr, path := getAddrFromURL(m.endpoint, stnrv1.DefaultMetricsPort)
	if m.servAddr != nil && m.servAddr.String() == addr {
		return nil
	}

	m.log.Tracef("starting metrics server at %s", m.endpoint)
	mux := http.NewServeMux()
	mux.Handle(path, promhttp.Handler())
	m.server = &http.Server{Addr: addr, Handler: mux}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot start metrics server at http://%s%s: %w", addr, path, err)
	}
	m.servAddr = ln.Addr()

	go func() {
		if err := m.server.Serve(ln); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				m.log.Tracef("metrics server: normal shutdown")
			} else {
				m.log.Warnf("metrics server error at http://%s%s: %s",
					addr, path, err.Error())
				m.server = nil
			}
		}
	}()
	return nil
}

func (m *Metrics) Close(_ bool) error {
	if m.server == nil {
		return nil
	}
	if err := m.server.Close(); err != nil {
		m.log.Debugf("error closing metrics server: %s", err.Error())
	}
	m.server = nil
	m.servAddr = nil
	return nil
}
