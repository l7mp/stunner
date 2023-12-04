package client

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/logging"
)

var errFileTruncated = errors.New("zero-length config file")

var (
	// Time
	RetryPeriod = 1 * time.Second

	// Time allowed to write a message to the CDS server.
	WriteWait = 2 * time.Second

	// Time allowed to read the next pong message from the CDS server.
	PongWait = 8 * time.Second

	// Send pings to the CDS server with this period. Must be less than PongWait.
	PingPeriod = 5 * time.Second
)

// Client represents a generic config client. Currently supported config providers: http, ws, or
// file. Configuration obtained through the client are not validated, make sure to validate on the
// receiver side.
type Client interface {
	// Load grabs a new configuration from the config client.
	Load() (*stnrv1.StunnerConfig, error)
	// Watch listens to new configurations and returns them on the channel ch. The context ctx
	// cancels the watcher.
	Watch(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error
	fmt.Stringer
}

// GetConfig returns the configuration of the running STUNner daemon.
func NewClient(origin string, id string, logger logging.LoggerFactory) (Client, error) {
	u, err := url.Parse(origin)
	if err != nil {
		return nil, fmt.Errorf("could not parse config origin address %q: %w", origin, err)
	}

	var client Client
	switch strings.ToLower(u.Scheme) {
	case "http", "ws", "https", "wss":
		client = &configDiscoveryClient{
			serverAddress: origin,
			id:            id,
			log:           logger.NewLogger("config-poller"),
		}
	default:
		client = &configFileClient{
			configFile: origin,
			id:         id,
			log:        logger.NewLogger("config-watcher"),
		}
	}

	return client, nil
}
