//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen --config=cfg.yaml ../api/stunner_openapi.yaml
package client

import (
	"context"
	"fmt"
	"strings"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/logging"
)

// CDSClient is a client for the config discovery service that knows how to poll configs for a
// specific gateway. Use the CDSAPI to access the general CDS client set.
type CDSClient struct {
	CdsApi
	addr, id string
}

// NewCDSClient creates a config discovery service client that can be used to load or watch STUNner
// configurations from a CDS remote server.
func NewCDSClient(addr, id string, logger logging.LeveledLogger) (Client, error) {
	ps := strings.Split(id, "/")
	if len(ps) != 2 {
		return nil, fmt.Errorf("invalid id: %q", id)
	}

	client, err := NewConfigNamespaceNameAPI(addr, ps[0], ps[1], logger)
	if err != nil {
		return nil, err
	}

	return &CDSClient{CdsApi: client, addr: addr, id: id}, nil
}

// String outputs the status of the client.
func (p *CDSClient) String() string {
	return fmt.Sprintf("config discovery client %q: using server %s", p.id, p.addr)
}

// Load grabs a new configuration from the config doscovery server.
func (p *CDSClient) Load() (*stnrv1.StunnerConfig, error) {
	configs, err := p.CdsApi.Get(context.Background())
	if err != nil {
		return nil, err
	}
	if len(configs) != 1 {
		return nil, fmt.Errorf("expected exactly one config, got %d", len(configs))
	}

	c := configs[0]
	if err := c.Validate(); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	return c, nil
}
