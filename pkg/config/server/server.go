//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen --config=cfg.yaml ../api/stunner_openapi.yaml
package server

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/l7mp/stunner/pkg/config/server/api"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var (
	// ConfigDeletionUpdateDelay is the delay between deleting a config from the server and
	// sending the corresponing zero-config to the client. Set this to zero to suppress sending
	// the zero-config all together.
	ConfigDeletionUpdateDelay = 5 * time.Second
)

// Server is a generic config discovery server implementation.
type Server struct {
	*http.Server
	router   *mux.Router
	addr     string
	conns    *ConnTrack
	configs  *ConfigStore
	configCh chan Config
	deleteCh chan Config
	patch    ConfigPatcher
	log      logr.Logger
}

// New creates a new config discovery server instance for the specified address.
func New(addr string, patch ConfigPatcher, logger logr.Logger) *Server {
	if addr == "" {
		addr = stnrv1.DefaultConfigDiscoveryAddress
	}

	return &Server{
		router:   mux.NewRouter(),
		conns:    NewConnTrack(),
		configs:  NewConfigStore(),
		configCh: make(chan Config, 8),
		deleteCh: make(chan Config, 8),
		addr:     addr,
		patch:    patch,
		log:      logger,
	}
}

// Start let the config discovery server listen to new client connections.
func (s *Server) Start(ctx context.Context) error {
	handler := api.NewStrictHandler(s, []api.StrictMiddlewareFunc{s.WSUpgradeMiddleware})
	api.HandlerFromMux(handler, s.router)
	s.Server = &http.Server{Addr: s.addr, Handler: s.router}

	go func() {
		s.log.Info("starting CDS server", "address", s.addr, "patch", s.patch != nil)

		err := s.ListenAndServe()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || errors.Is(err, http.ErrServerClosed) {
				s.log.Info("closing config discovery server")
			} else {
				s.log.Error(err, "error closing config discovery server", "error", err.Error())
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
				s.broadcastConfig(c)

			case c := <-s.deleteCh:
				// delayed config deletion
				go func() {
					select {
					case <-ctx.Done():
						return
					case <-time.After(ConfigDeletionUpdateDelay):
						s.configCh <- Config{Id: c.Id, Config: c.Config}
						return
					}
				}()

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
	if c := s.conns.Get(id); c != nil {
		s.log.V(1).Info("forcefully removing client connection", "client", id)
		s.closeConn(c)
	}
}

func (s *Server) handleConn(ctx context.Context, wsConn *websocket.Conn, operationID string, filter ConfigFilter, patch ClientConfigPatcher) {
	conn := NewConn(wsConn, filter, patch)
	s.conns.Upsert(conn)

	// a dummy reader that drops everything it receives: this must be there for the
	// WebSocket server to call our pong-handler: conn.Close() will kill this goroutine
	go func() {
		for {
			// drop anything we receive
			_, _, err := conn.ReadMessage()
			if err != nil {
				return
			}
		}
	}()

	conn.SetPingHandler(func(string) error {
		return conn.WriteMessage(websocket.PongMessage, []byte("keepalive"))
	})

	s.log.V(1).Info("new config stream connection", "api", operationID, "client", conn.Id())

	// send initial config(list)
	for _, conf := range s.configs.Snapshot() {
		if filter(conf.Id) {
			s.sendConfig(conn, conf.Config)
		}
	}

	// wait until client closes the connection or the server is cancelled (which will kill all
	// the running connections)
	<-ctx.Done()

	s.log.V(1).Info("client connection closed", "api", operationID, "client", conn.Id())

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
			s.log.Error(err, "cannot patch config", "event", e.String())
			return
		}
		c = newC
	}

	json, err := json.Marshal(c)
	if err != nil {
		s.log.Error(err, "cannor JSON serialize config", "event", e.String())
		return
	}

	s.sendJSONConfig(conn, json)
}

func (s *Server) sendJSONConfig(conn *Conn, json []byte) {
	s.log.V(2).Info("sending configuration to client", "client", conn.Id(),
		"config", string(json))

	if err := conn.WriteMessage(websocket.TextMessage, json); err != nil {
		s.log.Error(err, "error sending config update", "client", conn.Id())
		s.closeConn(conn)
	}
}

func (s *Server) closeConn(conn *Conn) {
	s.log.V(1).Info("closing client connection", "client", conn.Id())

	conn.WriteMessage(websocket.CloseMessage, []byte{}) //nolint:errcheck
	s.conns.Delete(conn)
	conn.Close()
}
