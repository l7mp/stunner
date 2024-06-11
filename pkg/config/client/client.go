package client

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/logging"
)

var errFileTruncated = errors.New("Zero-length config file")

var (
	// Send pings to the CDS server with this period. Must be less than PongWait.
	PingPeriod = 5 * time.Second

	// Time allowed to read the next pong message from the CDS server.
	PongWait = 8 * time.Second

	// Time allowed to write a message to the CDS server.
	WriteWait = 2 * time.Second

	// Period for retrying failed CDS connections.
	RetryPeriod = 1 * time.Second
)

// Client represents a generic config client. Currently supported config providers: http, ws, or
// file. Configuration obtained through the client are not validated, make sure to validate on the
// receiver side.
type Client interface {
	// Load grabs a new configuration from the config client.
	Load() (*stnrv1.StunnerConfig, error)
	// Watch grabs new configs from a config origin (config file or CDS server) and returns
	// them on the channel. The context cancels the watcher. If the origin is not available
	// watch will retry.
	Watch(ctx context.Context, ch chan<- *stnrv1.StunnerConfig) error
	// Poll creates a one-shot config watcher without the retry mechanincs of Watch.
	Poll(ctx context.Context, ch chan<- *stnrv1.StunnerConfig) error
	fmt.Stringer
}

// New creates a generic config client. Origin is either a network address in the form
// "<IP>:<port>" or a proper HTTP/WS URI, in which case a CDS client is returned, or a proper file
// URL "file://<path>/<filename>" in which case a config file watcher is returned.
func New(origin string, id string, logger logging.LoggerFactory) (Client, error) {
	u, err := getURI(origin)
	if err != nil {
		return nil, fmt.Errorf("could not parse config origin URI %q: %w", origin, err)
	}

	switch strings.ToLower(u.Scheme) {
	case "http", "ws", "https", "wss":
		client, err := NewCDSClient(u.String(), id, logger.NewLogger("cds-client"))
		if err != nil {
			return nil, err
		}
		return client, nil
	default:
		client, err := NewConfigFileClient(origin, id, logger.NewLogger("config-file-client"))
		if err != nil {
			return nil, err
		}
		return client, nil
	}
}
