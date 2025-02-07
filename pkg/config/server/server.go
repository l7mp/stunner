//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=cfg.yaml ../api/stunner_openapi.yaml
package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/l7mp/stunner/pkg/config/server/api"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var (
	// SuppressConfigDeletion allows the server to suppress config deletions all together. Used
	// mostly for testing.
	SuppressConfigDeletion = false
)

// Server is a generic config discovery server implementation.
type Server struct {
	*http.Server
	router       *mux.Router
	addr         string
	conns        *ConnTrack
	configs      *ConfigStore
	configCh     chan Config
	deleteCh     chan Config
	patch        ConfigPatcher
	licenseStore *LicenseStore
	log          logr.Logger
}

// New creates a new config discovery server instance for the specified address.
func New(addr string, patch ConfigPatcher, logger logr.Logger) *Server {
	if addr == "" {
		addr = stnrv1.DefaultConfigDiscoveryAddress
	}

	return &Server{
		router:       mux.NewRouter(),
		conns:        NewConnTrack(),
		configs:      NewConfigStore(),
		configCh:     make(chan Config, 8),
		deleteCh:     make(chan Config, 8),
		addr:         addr,
		patch:        patch,
		licenseStore: NewLicenseStore(),
		log:          logger,
	}
}

// Start let the config discovery server listen to new client connections.
func (s *Server) Start(ctx context.Context) error {
	handler := api.NewStrictHandler(s, []api.StrictMiddlewareFunc{s.WSUpgradeMiddleware})
	api.HandlerFromMux(handler, s.router)
	s.Server = &http.Server{Addr: s.addr, Handler: s.router}
	l, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("CDS server failed to listen: %w", err)
	}

	go func() {
		s.log.Info("Starting CDS server", "address", s.addr, "config-patcher-enabled", s.patch != nil)

		err := s.Serve(l)
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed) {
				s.log.Info("Closing config discovery server")
			} else {
				s.log.Error(err, "Error closing config discovery server", "error", err.Error())
			}
			return
		}
	}()

	go func() {
		defer close(s.configCh)
		defer close(s.deleteCh)
		defer s.Close()

		for {
			select {
			case c := <-s.configCh:
				s.log.V(2).Info("Sending config update event", "config-id", c.Id)
				s.broadcastConfig(c)
			case c := <-s.deleteCh:
				s.log.V(2).Info("Sending config delete event", "config-id", c.Id)
				s.broadcastConfig(c)
			case <-ctx.Done():
				return
			}
		}
	}()

	return nil
}

// Close closes the server and drops all active connections.
func (s *Server) Close() {
	// first close the underlying HTTP server so that we do not get any new connnections
	s.Server.Close()
	// then kill all active connections
	for _, conn := range s.conns.Snapshot() {
		s.closeConn(conn)
	}
}

// GetConfigChannel returns the channel that can be used to add configs to the server's config
// store. Use Update to specify more configs at once.
func (s *Server) GetConfigChannel() chan Config {
	return s.configCh
}

// GetConfigStore returns the dataplane configs stores in the server.
func (s *Server) GetConfigStore() *ConfigStore {
	return s.configs
}

// GetConnTrack returns the client connection tracking table of the server.
func (s *Server) GetConnTrack() *ConnTrack {
	return s.conns
}

// RemoveClient forcefully closes a client connection. This is used mainly for testing.
func (s *Server) RemoveClient(id string) {
	if conn := s.conns.Get(id); conn != nil {
		s.log.V(1).Info("Forcefully removing client connection", "config-id", id,
			"client", conn.RemoteAddr().String())
		s.closeConn(conn)
	}
}

func (s *Server) handleConn(reqCtx context.Context, wsConn *websocket.Conn, operationID string, filter ConfigFilter, patch ClientConfigPatcher) {
	// since wsConn is hijacked, reqCtx is unreliable in that it may not be canceled when the
	// connection is closed, so we create our own connection context that we can cancel
	// explicitly
	ctx, cancel := context.WithCancel(reqCtx)
	conn := NewConn(wsConn, filter, patch, cancel)
	s.conns.Upsert(conn)

	// a dummy reader that drops everything it receives: this must be there for the
	// WebSocket server to call our pong-handler: conn.Close() will kill this goroutine
	go func() {
		for {
			// drop anything we receive
			_, _, err := conn.ReadMessage()
			if err != nil {
				s.closeConn(conn)
				return
			}
		}
	}()

	conn.SetPingHandler(func(string) error {
		return conn.WriteMessage(websocket.PongMessage, []byte("keepalive"))
	})

	s.log.V(1).Info("New config stream connection", "api", operationID, "client", conn.Id())

	// send initial config(list)
	for _, conf := range s.configs.Snapshot() {
		if filter(conf.Id) {
			s.sendConfig(conn, conf.Config)
		}
	}

	// wait until client closes the connection or the server is cancelled (which will kill all
	// the running connections)
	<-ctx.Done()

	s.log.V(1).Info("Client connection closed", "api", operationID, "client", conn.Id())

	conn.Close()
}

// iterate through all connections and send response if needed
func (s *Server) broadcastConfig(e Config) {
	for _, conn := range s.conns.Snapshot() {
		if conn.Filter(e.Id) {
			s.sendConfig(conn, e.Config)
		}
	}
}

func (s *Server) sendConfig(conn *Conn, e *stnrv1.StunnerConfig) {
	c := &stnrv1.StunnerConfig{}
	e.DeepCopyInto(c)

	if conn.patch != nil {
		newC, err := conn.patch(c)
		if err != nil {
			s.log.Error(err, "Cannot patch config", "event", e.String())
			return
		}
		c = newC
	}

	json, err := json.Marshal(c)
	if err != nil {
		s.log.Error(err, "Cannot JSON serialize config", "event", e.String())
		return
	}

	s.log.V(2).Info("Sending configuration to client", "client", conn.Id())

	if err := conn.WriteMessage(websocket.TextMessage, json); err != nil {
		s.log.Error(err, "Error sending config update", "client", conn.Id())
		s.closeConn(conn)
	}
}

func (s *Server) closeConn(conn *Conn) {
	s.log.V(1).Info("Closing client connection", "client", conn.Id())

	conn.WriteMessage(websocket.CloseMessage, []byte{}) //nolint:errcheck

	if conn.cancel != nil {
		conn.cancel()
		conn.cancel = nil // make sure we can cancel multiple times
	}

	s.conns.Delete(conn)
	conn.Close()
}
