package object

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/runtime"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Health is the Object that owns the /live, /ready, /status HTTP server.
type Health struct {
	endpoint string
	server   *http.Server
	servAddr net.Addr
	mux      *http.ServeMux
	dryRun   bool

	// conf is the atomic snapshot read via Admin.GetConfig on the allocation path.
	conf atomic.Pointer[HealthConfig]

	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// HealthConfig is the typed subconfig consumed by the Health object.
type HealthConfig struct {
	// Endpoint is the URI of the form `http://address:port` exposed for external HTTP
	// health-checking. Empty endpoint == use defaults; nil pointer in AdminConfig is mapped to
	// the default endpoint at extract time.
	Endpoint string `json:"endpoint,omitempty"`
}

func (c *HealthConfig) Validate() error    { return nil }
func (c *HealthConfig) ConfigName() string { return stnrv1.DefaultHealthName }
func (c *HealthConfig) DeepEqual(other stnrv1.Config) bool {
	o, ok := other.(*HealthConfig)
	if !ok {
		return false
	}
	return c.Endpoint == o.Endpoint
}
func (c *HealthConfig) DeepCopyInto(dst stnrv1.Config) {
	d, ok := dst.(*HealthConfig)
	if !ok {
		return
	}
	*d = *c
}
func (c *HealthConfig) String() string {
	return fmt.Sprintf("HealthConfig{endpoint=%q}", c.Endpoint)
}

// NewHealth creates a Health object.
func NewHealth(conf stnrv1.Config, rt *runtime.Runtime) (runtime.Object, error) {
	h := &Health{
		dryRun: rt.DryRun,
		rt:     rt,
		log:    rt.Logger.NewLogger("health"),
	}
	h.mux = h.buildMux()
	if conf == nil {
		return h, nil
	}
	req, ok := conf.(*HealthConfig)
	if !ok {
		return nil, stnrv1.ErrInvalidConf
	}
	if err := h.Reconcile(req); err != nil {
		return nil, err
	}
	return h, nil
}

func (h *Health) buildMux() *http.ServeMux {
	mux := http.NewServeMux()
	// Liveness probe: always OK once we are here.
	mux.HandleFunc("/live", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}\n")) //nolint:errcheck
	})
	mux.HandleFunc("/ready", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if h.rt == nil {
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, "{\"status\":%d,\"message\":\"%s\"}\n", //nolint:errcheck
				http.StatusOK, "READY")
			return
		}
		if !h.rt.ReadyForProbes() {
			w.WriteHeader(http.StatusServiceUnavailable)
			fmt.Fprintf(w, "{\"status\":%d,\"message\":\"%s\"}\n", //nolint:errcheck
				http.StatusServiceUnavailable, "stunnerd not ready")
			return
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "{\"status\":%d,\"message\":\"%s\"}\n", //nolint:errcheck
			http.StatusOK, "READY")
	})
	mux.HandleFunc("/status", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		var status stnrv1.Status
		if h.rt != nil {
			status = h.rt.GetStatus(runtime.TypeStunner, "")
		}
		if status == nil {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("{}\n")) //nolint:errcheck
			return
		}
		js, err := json.Marshal(status)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprintf(w, "{\"status\":%d,\"message\":\"%s\"}\n", //nolint:errcheck
				http.StatusInternalServerError, err.Error())
			return
		}
		w.WriteHeader(http.StatusOK)
		w.Write(js) //nolint:errcheck
	})
	return mux
}

func (h *Health) Name() string             { return stnrv1.DefaultHealthName }
func (h *Health) Type() runtime.ObjectType { return runtime.TypeHealth }

// GetConfig returns a copy of the live health config. Safe for concurrent use.
func (h *Health) GetConfig() stnrv1.Config {
	if snap := h.conf.Load(); snap != nil {
		cp := *snap
		return &cp
	}
	return &HealthConfig{}
}

func (h *Health) Status() stnrv1.Status { return h.GetConfig() }

func (h *Health) Inspect(old, new stnrv1.Config, _ *stnrv1.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*HealthConfig)
	if !ok {
		return runtime.ActionNone, stnrv1.ErrInvalidConf
	}
	cur := old.(*HealthConfig)
	if reflect.DeepEqual(req, cur) {
		return runtime.ActionNone, nil
	}
	return runtime.ActionRestart, nil
}

func (h *Health) Reconcile(conf stnrv1.Config) error {
	req, ok := conf.(*HealthConfig)
	if !ok {
		return stnrv1.ErrInvalidConf
	}
	h.endpoint = req.Endpoint
	h.conf.Store(&HealthConfig{Endpoint: req.Endpoint})
	if h.mux == nil {
		h.mux = h.buildMux()
	}
	return nil
}

func (h *Health) Start() error {
	if h.dryRun {
		return nil
	}
	// Empty endpoint disables health checking entirely.
	if h.endpoint == "" {
		return nil
	}

	addr, _ := getAddrFromURL(h.endpoint, stnrv1.DefaultHealthCheckPort)
	if h.servAddr != nil && h.servAddr.String() == addr {
		// Server is already up at the desired address.
		return nil
	}

	h.log.Tracef("starting healthcheck server at http://%s", addr)
	h.server = &http.Server{Addr: addr, Handler: h.mux}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("cannot start healthcheck server at http://%s: %w", addr, err)
	}
	h.servAddr = ln.Addr()

	go func() {
		if err := h.server.Serve(ln); err != nil {
			if errors.Is(err, http.ErrServerClosed) {
				h.log.Tracef("healthcheck server: normal shutdown")
			} else {
				h.log.Warnf("healthcheck server error at http://%s: %s",
					addr, err.Error())
				h.server = nil
			}
		}
	}()
	return nil
}

func (h *Health) Close(_ bool) error {
	if h.server == nil {
		return nil
	}
	if err := h.server.Close(); err != nil {
		h.log.Debugf("error closing healthcheck server: %s", err.Error())
	}
	h.server = nil
	h.servAddr = nil
	return nil
}

// defaultHealthEndpoint mirrors the historical Admin behaviour: a nil HealthCheckEndpoint pointer
// (the user not setting the field) maps to the default `http://:8086` endpoint.
func defaultHealthEndpoint() string {
	var b strings.Builder
	fmt.Fprintf(&b, "http://:%d", stnrv1.DefaultHealthCheckPort)
	return b.String()
}
