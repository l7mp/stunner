package object

import (
	"fmt"
	"strings"

	objectturn "github.com/l7mp/stunner/internal/object/turn"
	"github.com/l7mp/stunner/internal/runtime"
	"github.com/l7mp/stunner/internal/util"
)

// ListenerServer is the lifecycle-only child node that owns the TURN server of a Listener.
type ListenerServer struct {
	name     string
	listener *Listener
	rt       *runtime.Runtime
	server   *objectturn.Server
}

// NewListenerServer creates a lifecycle-only listener server child.
func NewListenerServer(listener *Listener, rt *runtime.Runtime) *ListenerServer {
	return &ListenerServer{
		name:     listener.Name(),
		listener: listener,
		rt:       rt,
	}
}

func (s *ListenerServer) Name() string { return s.name }

func (s *ListenerServer) Type() runtime.ObjectType { return runtime.TypeListenerServer }

func (s *ListenerServer) Start() error {
	s.listener.log.Infof("listener %s (re)starting", s.listener.String())
	t, err := objectturn.NewServer(s.name, s.rt, s.listener.getOffload())
	if err != nil {
		return fmt.Errorf("failed to start TURN server for listener %s: %w", s.name, err)
	}
	s.server = t
	s.listener.log.Infof("listener %s: listener running", s.name)
	return nil
}

func (s *ListenerServer) Close(_ bool) error {
	if s.server == nil {
		return nil
	}
	if err := s.server.Close(); err != nil && !util.IsClosedErr(err) && !strings.Contains(err.Error(), "already closed") {
		return err
	}
	s.server = nil
	return nil
}

// AllocationCount returns the active allocation count from the TURN server.
func (s *ListenerServer) AllocationCount() int {
	if s.server == nil {
		return 0
	}
	return s.server.AllocationCount()
}
