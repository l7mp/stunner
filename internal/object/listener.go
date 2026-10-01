package object

import (
	"encoding/base64"
	"fmt"
	"sync/atomic"

	"github.com/pion/logging"

	"github.com/l7mp/stunner/v2/internal/api"
	"github.com/l7mp/stunner/v2/internal/listener"
	"github.com/l7mp/stunner/v2/internal/runtime"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// Listener is a listener config and the listener it runs, feeding the chain of servers the config
// names.
type Listener struct {
	name string

	conf atomic.Pointer[stnrv2.ListenerConfig]

	listener api.Listener

	rt  *runtime.Runtime
	log logging.LeveledLogger
}

// NewListener creates a listener object.
func NewListener(conf stnrv2.Config, rt *runtime.Runtime) (runtime.Object, error) {
	req, ok := conf.(*stnrv2.ListenerConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	l := &Listener{
		name: req.Name,
		rt:   rt,
		log:  rt.Logger.NewLogger(fmt.Sprintf("listener-%s", req.Name)),
	}
	if err := l.Reconcile(req); err != nil {
		return nil, err
	}
	return l, nil
}

func (l *Listener) Name() string             { return l.name }
func (l *Listener) Type() runtime.ObjectType { return runtime.TypeListener }

// Inspect restarts on a socket change; servers and public addresses reconcile in place.
func (l *Listener) Inspect(old, new stnrv2.Config, _ *stnrv2.StunnerConfig) (runtime.Action, error) {
	req, ok := new.(*stnrv2.ListenerConfig)
	if !ok {
		return runtime.ActionNone, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return runtime.ActionNone, err
	}
	cur := old.(*stnrv2.ListenerConfig)
	if cur.DeepEqual(req) {
		return runtime.ActionNone, nil
	}
	if cur.Protocol != req.Protocol || cur.Addr != req.Addr || cur.Port != req.Port ||
		cur.Cert != req.Cert || cur.Key != req.Key || cur.PQCMode != req.PQCMode {
		return runtime.ActionRestart, nil
	}
	return runtime.ActionReconcile, nil
}

func (l *Listener) Reconcile(conf stnrv2.Config) error {
	req, ok := conf.(*stnrv2.ListenerConfig)
	if !ok {
		return stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return err
	}
	if l.name != req.Name {
		return fmt.Errorf("cannot rename listener %q to %q", l.name, req.Name)
	}
	cp := &stnrv2.ListenerConfig{}
	req.DeepCopyInto(cp)
	l.conf.Store(cp)
	return nil
}

func (l *Listener) GetConfig() stnrv2.Config {
	cp := &stnrv2.ListenerConfig{Name: l.name}
	if c := l.conf.Load(); c != nil {
		c.DeepCopyInto(cp)
	}
	return cp
}

// Start binds the listener.
func (l *Listener) Start() error {
	conf := l.conf.Load()
	// the listener takes its TLS cert and key in PEM
	decoded := &stnrv2.ListenerConfig{}
	conf.DeepCopyInto(decoded)
	cert, err := base64.StdEncoding.DecodeString(conf.Cert)
	if err != nil {
		return fmt.Errorf("invalid TLS certificate: base64-decode error: %w", err)
	}
	key, err := base64.StdEncoding.DecodeString(conf.Key)
	if err != nil {
		return fmt.Errorf("invalid TLS key: base64-decode error: %w", err)
	}
	decoded.Cert, decoded.Key = string(cert), string(key)

	ln, err := listener.New(decoded, l.rt, l.serve)
	if err != nil {
		return err
	}
	if err := ln.Start(); err != nil {
		return fmt.Errorf("failed to start listener %s: %w", l.name, err)
	}
	l.listener = ln
	return nil
}

// Close closes the listener. The conns it emitted stay up.
func (l *Listener) Close(_ bool) error {
	if l.listener == nil {
		return nil
	}
	err := l.listener.Close()
	l.listener = nil
	return err
}

// serve hands a conn down the server chain, closing it unless a server consumes it.
func (l *Listener) serve(c api.Conn) {
	servers := l.conf.Load().Servers
	for _, name := range servers {
		srv, ok := l.rt.Server(name)
		if !ok {
			l.log.Debugf("listener %s: no server %q for client %s", l.name, name,
				c.RemoteAddr().String())
			_ = c.Close()
			return
		}
		next, verdict, err := srv.Serve(c)
		if err != nil {
			l.log.Debugf("listener %s: server %q refused client %s: %s", l.name, name,
				c.RemoteAddr().String(), err.Error())
			_ = c.Close()
			return
		}
		if verdict == api.Consumed {
			return
		}
		c = next
	}
	if len(servers) > 0 {
		l.log.Warnf("listener %s: the last server %q passed client %s on: closing it", l.name,
			servers[len(servers)-1], c.RemoteAddr().String())
	}
	_ = c.Close()
}

func (l *Listener) Status() stnrv2.Status {
	status := &stnrv2.ListenerStatus{ListenerConfig: l.GetConfig().(*stnrv2.ListenerConfig)}
	if offloadStatus, ok := l.rt.GetStatus(runtime.TypeOffload, "").(*stnrv2.OffloadStatus); ok {
		status.Stats = offloadStatus.Listeners[l.name]
	}
	return status
}
