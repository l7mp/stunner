//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen --config=cfg.yaml ../api/stunner_openapi.yaml
package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/logging"
)

// CDSClient is a client for the config discovery service that knows how to poll configs for a
// specific gateway. Use the CDSAPI to access the general CDS client set.
type CDSClient struct {
	CDSAPI
	addr, id string
}

func NewCDSClient(addr, id string, logger logging.LeveledLogger) (Client, error) {
	ps := strings.Split(id, "/")
	if len(ps) != 2 {
		return nil, fmt.Errorf("invalid id: %q", id)
	}

	client, err := NewConfigNamespaceNameAPI(addr, ps[0], ps[1], logger)
	if err != nil {
		return nil, err
	}

	return &CDSClient{CDSAPI: client, addr: addr, id: id}, nil
}

func (p *CDSClient) String() string {
	return fmt.Sprintf("config discovery client %q: using server %s", p.id, p.addr)
}

func (p *CDSClient) Load() (*stnrv1.StunnerConfig, error) {
	configs, err := p.CDSAPI.Get(context.Background())
	if err != nil {
		return nil, err
	}
	if len(configs) != 1 {
		return nil, fmt.Errorf("expected exactly one config, got %d", len(configs))
	}

	return configs[0], nil
}

func watch(ctx context.Context, a CDSAPI, ch chan<- stnrv1.StunnerConfig) error {
	go func() {
		for {
			// try to watch
			if err := poll(ctx, a, ch); err != nil {
				_, wsuri := a.Endpoint()
				a.Errorf("failed to init CDS watcher (url: %s): %s", wsuri, err.Error())
			} else {
				// context got cancelled
				return
			}

			// wait between each attempt
			time.Sleep(RetryPeriod)
		}
	}()

	return nil
}

func poll(ctx context.Context, a CDSAPI, ch chan<- stnrv1.StunnerConfig) error {
	_, url := a.Endpoint()
	a.Tracef("poll: trying to open connection to CDS server at %s", url)

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, url, makeHeader(url))
	if err != nil {
		return err
	}
	defer conn.Close() // this will close the poller goroutine

	a.Infof("connection successfully opened to config discovery server at %s", url)

	errCh := make(chan error, 1)
	pingTicker := time.NewTicker(PingPeriod)
	closePinger := make(chan any)
	defer close(closePinger)

	go func() {
		defer pingTicker.Stop()
		for {
			select {
			case <-pingTicker.C:
				// p.log.Tracef("++++ PING ++++ for CDS server %q at client %q", location, p.id)
				conn.SetWriteDeadline(time.Now().Add(WriteWait)) //nolint:errcheck
				if err := conn.WriteMessage(websocket.PingMessage, []byte("keepalive")); err != nil {
					errCh <- fmt.Errorf("could not ping CDS server at %q: %w",
						conn.RemoteAddr(), err)
					return
				}
			case <-closePinger:
				a.Tracef("closing ping handler to config discovery server at %q", url)
				return
			}
		}
	}()

	// poller
	go func() {
		defer close(errCh)

		// the next pong must arrive within the PongWait period
		conn.SetReadDeadline(time.Now().Add(PongWait)) //nolint:errcheck
		// reinit the deadline when we get a pong
		conn.SetPongHandler(func(string) error {
			// a.Tracef("Got PONG from server %q", url)
			conn.SetReadDeadline(time.Now().Add(PongWait)) //nolint:errcheck
			return nil
		})

		for {
			// ping-pong deadline misses will end up being caught here as a read beyond
			// the deadline
			msgType, msg, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}

			if msgType != websocket.TextMessage {
				errCh <- fmt.Errorf("unexpected message type (code: %d) from client %q",
					msgType, conn.RemoteAddr().String())
				return
			}

			if len(msg) == 0 {
				a.Warn("ignoring zero-length config")
				continue
			}

			// fmt.Println("++++++++++++++++++++")
			// fmt.Println(string(msg))
			// fmt.Println("++++++++++++++++++++")

			c, err := ParseConfig(msg)
			if err != nil {
				// assume it is a YAML/JSON syntax error: report and ignore
				a.Warnf("could not parse config: %s", err.Error())
				continue
			}

			confCopy := stnrv1.StunnerConfig{}
			c.DeepCopyInto(&confCopy)

			a.Debugf("new config received from %q: %q", url, confCopy.String())

			ch <- confCopy
		}
	}()

	// wait fo cancel
	for {
		defer func() {
			a.Infof("closing connection for client %s", conn.RemoteAddr().String())
			conn.WriteMessage(websocket.CloseMessage, []byte{}) //nolint:errcheck
			conn.Close()
			closePinger <- struct{}{}
		}()

		select {
		case <-ctx.Done():
			// cancel: normal return
			return nil
		case err := <-errCh:
			// error: return it
			return err
		}
	}
}

// creates an origin header
func makeHeader(uri string) http.Header {
	header := http.Header{}
	url, _ := getURI(uri) //nolint:errcheck
	origin := *url
	origin.Scheme = "http"
	origin.Path = ""
	header.Set("origin", origin.String())
	return header
}
