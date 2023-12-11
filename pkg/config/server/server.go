//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen --config=cfg.yaml ../api/stunner_openapi.yaml
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

// Server is a generic config discovery server implementation.
type Server struct {
	*http.Server
	addr     string
	conns    *ConnTrack
	configs  *ConfigStore
	configCh chan Config
	log      logr.Logger
}

// New creates a new config discovery server instance for the specified address.
func New(addr string, logger logr.Logger) *Server {
	if addr == "" {
		addr = stnrv1.DefaultConfigDiscoveryAddress
	}

	cds := &Server{
		conns:    NewConnTrack(),
		configs:  NewConfigStore(),
		configCh: make(chan Config, 8),
		addr:     addr,
		log:      logger,
	}
	return cds
}

// Start let the config discovery server listen to new client connections.
func (s *Server) Start(ctx context.Context) error {
	r := mux.NewRouter()
	api.HandlerFromMux(s, r)
	s.Server = &http.Server{Addr: s.addr, Handler: r}

	go func() {
		s.log.Info("Starting CDS server", "address", s.addr)

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
		defer s.Close()

		for {
			select {
			case e := <-s.configCh:
				s.broadcastConfig(e)
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

func (s *Server) handleReq(w http.ResponseWriter, r *http.Request, endpoint string, responder ResponseGen) {
	s.log.V(1).Info("received new request", "api", endpoint, "client", r.RemoteAddr)

	response, err := responder()
	if err != nil {
		s.log.Info("client config not found", "api", endpoint, "client", r.RemoteAddr,
			"code", err.Code, "message", err.Message)
		sendServerErrorRaw(w, err)
	}

	s.log.V(2).Info("sending response to client", "api", endpoint, "client", r.RemoteAddr,
		"response", string(response))

	if _, err := w.Write(response); err != nil {
		s.log.Error(err, "could not write config", "api", endpoint, "client", r.RemoteAddr)
		http.Error(w, "Could not write config", http.StatusInternalServerError)
		return
	}
}

func (s *Server) handleConn(w http.ResponseWriter, r *http.Request, endpoint string, responder ResponseGen, filter FilterConfig) {
	// upgrade to webSocket
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	wsConn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		msg := fmt.Sprintf("could not upgrade HTTP connection for client %s: %s",
			r.RemoteAddr, err.Error())
		sendServerError(w, msg, http.StatusInternalServerError)
		return
	}
	conn := NewConn(wsConn, filter)
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

	s.log.V(1).Info("new config stream connection", "api", endpoint, "client", conn.Id())

	// send initial config(list)
	for _, conf := range s.configs.Snapshot() {
		if filter(conf.Id) {
			s.sendConfig(conn, conf)
		}
	}

	// wait until client closes the connection or the server is cancelled (which will kill all
	// the running connections)
	<-r.Context().Done()

	s.log.V(1).Info("client connection closed", "api", endpoint,
		"client", r.RemoteAddr)

	conn.Close()
}

// iterate through all connections and send response if needed
func (s *Server) broadcastConfig(e Config) {
	json, err := json.Marshal(e.Config)
	if err != nil {
		s.log.Error(err, "error JSON marshaling config", "event", e.String())
		return
	}

	for _, conn := range s.conns.Snapshot() {
		if conn.Filter(e.Id) {
			s.sendJSONConfig(conn, json)
		}
	}
}

func (s *Server) sendConfig(conn *Conn, e Config) {
	json, err := json.Marshal(e.Config)
	if err != nil {
		s.log.Error(err, "error JSON marshaling config", "event", e.String())
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
