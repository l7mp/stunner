//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=cfg.yaml ../api/stunner_openapi.yaml
package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/go-logr/logr"
	"github.com/gorilla/mux"
	"github.com/l7mp/stunner/pkg/config/client"
	"github.com/l7mp/stunner/pkg/config/server/api"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var (
	// SuppressConfigDeletion allows the server to suppress config deletions all together. Used
	// mostly for testing.
	SuppressConfigDeletion = false
)

// ConfigNodePatcher is a callback to patch config updates per node name.
type ConfigNodePatcher func(conf *stnrv1.StunnerConfig, node string) *stnrv1.StunnerConfig

// Server is a generic config discovery server implementation.
type Server struct {
	*http.Server
	router       *mux.Router
	addr         string
	conns        *ConnTrack
	configs      *ConfigStore[string]
	patcher      ConfigNodePatcher
	licenseStore *LicenseStore
	log          logr.Logger
}

// New creates a new config discovery server instance for the specified address.
func New(addr string, patch ConfigNodePatcher, logger logr.Logger) *Server {
	if addr == "" {
		addr = stnrv1.DefaultConfigDiscoveryAddress
	}

	return &Server{
		router:       mux.NewRouter(),
		conns:        NewConnTrack(),
		configs:      NewConfigStore[string](),
		licenseStore: NewLicenseStore(),
		addr:         addr,
		patcher:      patch,
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
		s.log.Info("Starting CDS server", "address", s.addr, "config-patcher-enabled", s.patcher != nil)

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
		<-ctx.Done()
		s.Close()
	}()

	return nil
}

// Close closes the server and drops all active connections.
func (s *Server) Close() {
	// close the underlying HTTP server so that we do not get any new connnections
	s.Server.Close() //nolint:errcheck
	// kill all active connections
	for _, conn := range s.conns.Snapshot() {
		s.closeConn(conn)
	}
	// remove all subscriptions
	s.configs.UnsubscribeAll()
}

// GetConfigStore returns the dataplane configs stores in the server.
func (s *Server) GetConfigStore() *ConfigStore[string] {
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

// PusNodeConfig updates the config at each known client that is subscribed for updates on a
// given node. This is useful for force pushing a new config when some node address changes.
func (s *Server) PushNodeConfig(node string) {
	s.log.V(4).Info("Pusing configs for node", "node", node)
	s.configs.Push(node)
}

func (s *Server) UpsertConfig(id string, c *stnrv1.StunnerConfig) {
	s.log.V(4).Info("Upserting config", "config-id", id, "config", c.String())
	if namespace, name, ok := NamespacedName(id); ok {
		s.configs.Upsert(namespace, name, c)
	}
}

// DeleteConfig removes a config from clients by sending a zero-config. Clients may decide to
// ignore the delete operation by (1) using client.IsConfigDeleted() to identify whether a config
// is being deleted and (2) selectively ignoring config delete updates based on the result. This is
// needed, e.g., in stunnerd, in order to avoid that a client being removed and entering the
// graceful shutdown cycle receive a zeroconfig and abruprly kill all listeners with all active
// connections allocated to them.
func (s *Server) DeleteConfig(id string) {
	s.log.V(4).Info("Deleting config", "config-id", id)

	namespace, name, ok := NamespacedName(id)
	if !ok {
		return
	}

	config := client.ZeroConfig(id)
	if SuppressConfigDeletion {
		s.log.Info("Suppressing config update for deleted config", "config-id", id)
		config = nil
	}

	s.configs.Delete(namespace, name, config)
}

// UpdateConfig receives a set of ids and newConfigs that represent the state-of-the-world at a
// particular instance of time and generates an update per each change.
func (s *Server) UpdateConfig(newConfigs []Config) error {
	s.log.V(4).Info("Processing config updates", "num-configs", len(newConfigs))
	oldConfigs := s.configs.Snapshot()

	for _, oldC := range oldConfigs {
		found := false
		for _, newC := range newConfigs {
			if oldC.Id() == newC.Id() {
				if !oldC.Config.DeepEqual(newC.Config) {
					s.log.V(2).Info("Updating config", "config-id", newC.Id(), "config",
						newC.Config.String())
					s.UpsertConfig(newC.Id(), newC.Config)
				} else {
					s.log.V(2).Info("Config unchanged", "config-id", newC.Id(),
						"old-config", oldC.Config.String(),
						"new-config", newC.Config.String())
				}
				found = true
				break
			}
		}

		if !found {
			s.log.V(2).Info("Removing config", "config-id", oldC.Id())
			s.DeleteConfig(oldC.Id())
		}
	}

	for _, newC := range newConfigs {
		found := false
		for _, oldC := range oldConfigs {
			if oldC.Id() == newC.Id() {
				found = true
				break
			}
		}

		if !found {
			s.log.V(2).Info("Adding config", "config-id", newC.Id(), "config", newC.Config)
			s.UpsertConfig(newC.Id(), newC.Config)
		}
	}

	return nil
}
