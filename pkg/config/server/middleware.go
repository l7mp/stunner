package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/gorilla/websocket"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/config/server/api"
)

func (s *Server) WSUpgradeMiddleware(next api.StrictHandlerFunc, operationID string) api.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		var ch chan *Config
		watch := false

		switch operationID {
		case "GetV1LicenseStatus":
			return next(ctx, w, r, request)

		case "GetV1ConfigNamespaceName":
			param, ok := request.(api.GetV1ConfigNamespaceNameRequestObject)
			if !ok {
				return nil, fmt.Errorf("unexpected parameters in API operation %q",
					operationID)
			}

			watch = param.Params.Watch != nil && *param.Params.Watch
			if !watch {
				return next(ctx, w, r, request)
			}

			var patcher PatchFunc
			var filter ClientFilter[string]
			if s.patcher != nil && param.Params.Node != nil {
				filter = func(expectedNode string) bool {
					return *param.Params.Node == expectedNode
				}
				patcher = func(conf *stnrv1.StunnerConfig) *stnrv1.StunnerConfig {
					return s.patcher(conf, *param.Params.Node)
				}
			}

			ch = s.configs.SubscribeConfig(param.Namespace, param.Name, filter, patcher)

		case "ListV1ConfigsNamespace":
			param, ok := request.(api.ListV1ConfigsNamespaceRequestObject)
			if !ok {
				return nil, fmt.Errorf("unexpected parameters in API operation %q",
					operationID)
			}

			watch = param.Params.Watch != nil && *param.Params.Watch
			if !watch {
				return next(ctx, w, r, request)
			}

			ch = s.configs.SubscribeNamespace(param.Namespace, nil, nil)

		case "ListV1Configs":
			param, ok := request.(api.ListV1ConfigsRequestObject)
			if !ok {
				return nil, fmt.Errorf("unexpected parameters in API operation %q",
					operationID)
			}

			watch = param.Params.Watch != nil && *param.Params.Watch
			if !watch {
				return next(ctx, w, r, request)
			}

			ch = s.configs.SubscribeAll(nil, nil)

		default:
			return nil, fmt.Errorf("invalid API operation %q", operationID)
		}

		s.log.V(4).Info("WS upgrade middleware: upgrading connection", "client", r.RemoteAddr)

		// upgrade to webSocket
		upgrader := websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return nil, err
		}

		s.handleConn(ctx, conn, operationID, ch)

		return nil, nil
	}
}

func (s *Server) handleConn(reqCtx context.Context, wsConn *websocket.Conn, operationID string, ch chan *Config) {
	// since wsConn is hijacked, reqCtx is unreliable in that it may not be canceled when the
	// connection is closed, so we create our own connection context that we can cancel
	// explicitly
	ctx, cancel := context.WithCancel(reqCtx)
	conn := NewConn(wsConn, ch, cancel)
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

	s.log.V(2).Info("New config stream connection", "api", operationID, "client", conn.Id())

	for {
		select {
		// wait for a new config from our subscription
		case config, ok := <-ch:
			if !ok {
				continue // avoid race condition
			}
			s.log.V(4).Info("Sending client connection", "api", operationID, "client", conn.Id(),
				"config", config.String())
			s.writeConfig(conn, config.Config)

		// or wait until client closes the connection or the server is cancelled
		case <-ctx.Done():
			s.log.V(1).Info("Client connection closed", "api", operationID, "client", conn.Id())
			s.closeConn(conn)
			return
		}
	}
}

func (s *Server) writeConfig(conn *Conn, c *stnrv1.StunnerConfig) {
	json, err := json.Marshal(c)
	if err != nil {
		s.log.Error(err, "Cannot JSON serialize config", "config", c.String())
		return
	}

	s.log.V(2).Info("Sending configuration to client", "client", conn.Id())

	if err := conn.WriteMessage(websocket.TextMessage, json); err != nil {
		s.log.Error(err, "Error sending config update", "client", conn.Id())
		s.closeConn(conn)
	}
}

func (s *Server) closeConn(conn *Conn) {
	if conn.closed {
		return
	}
	conn.closed = true

	s.log.V(1).Info("Closing client connection", "client", conn.Id())

	conn.WriteMessage(websocket.CloseMessage, []byte{}) //nolint:errcheck

	if conn.cancel != nil {
		conn.cancel()
		conn.cancel = nil
	}

	s.conns.Delete(conn)
	conn.Close()

	s.configs.Unsubscribe(conn.ch)
}
