package client

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/logging"
)

// configDiscoveryClient is the the implementation of the config discovery service.
type configDiscoveryClient struct {
	// serverAddress is the URL of the config discovery server.
	serverAddress string
	// Id is the name of the stunnerd instance that is used to bootstrap the connection
	// poller. Set to namespace/name of the pod when using the stunner gateway operator for
	// config discovery.
	id string
	// Log is a leveled logger used to report progress.
	log logging.LeveledLogger
}

func (p *configDiscoveryClient) String() string {
	return fmt.Sprintf("config discovery service using server %q", p.serverAddress)
}

func (p *configDiscoveryClient) Load() (*stnrv1.StunnerConfig, error) {
	location, _, err := getConfigDiscoveryLocation(p.serverAddress, p.id, false)
	if err != nil {
		return nil, err
	}

	resp, err := http.Get(location)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("invalid HTTP response status: %s", resp.Status)
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if len(body) == 0 {
		return nil, errFileTruncated
	}

	// fmt.Println("++++++++++++++++++++")
	// fmt.Println(string(body))

	return ParseConfig(body)
}

// Watch polls a config discovery server for a configuration file by sending a config request and
// then waits for the server to push a valid `StunnerConfig`. Use the `context` to cancel the
// watcher.
func (p *configDiscoveryClient) Watch(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	_, _, err := getConfigDiscoveryLocation(p.serverAddress, p.id, true)
	if err != nil {
		return err
	}

	// Note: we do not emit an initial config but rather wait for the CDS server to send one,
	// so that pod will not be able to bootstrap the healthchecker and keep on restarting until
	// it finds the CDS server

	go func() {
		for {
			// try to watch
			if err := p.configPoller(ctx, ch); err != nil {
				p.log.Errorf("config file discovery service: %s", err.Error())
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

func (p *configDiscoveryClient) configPoller(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	p.log.Tracef("configPoller: trying to open connection to config discovery server at %q", p.serverAddress)

	location, origin, _ := getConfigDiscoveryLocation(p.serverAddress, p.id, true)
	header := http.Header{}
	header.Set("origin", origin)
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, location, header)
	if err != nil {
		return err
	}

	p.log.Infof("connection sucessfully opened to config discovery server at %q", location)

	// this will close the poller goroutine
	defer conn.Close()

	// pinger
	resCh := make(chan stnrv1.StunnerConfig, 16)
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
				p.log.Tracef("closing ping handler to config discovery server at %q at client %q",
					location, p.id)
				return
			}
		}
	}()

	// poller
	go func() {
		defer close(resCh)
		defer close(errCh)

		// the next pong must arrive within the PongWait period
		conn.SetReadDeadline(time.Now().Add(PongWait)) //nolint:errcheck
		// reinit the deadline when we get a pong
		conn.SetPongHandler(func(string) error {
			// p.log.Tracef("++++ PONG ++++ from CDS server %q at client %q", location, p.id)
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
				p.log.Warn("ignoring zero-length config config fil")
				continue
			}

			// fmt.Println("++++++++++++++++++++")
			// fmt.Println(string(msg))
			// fmt.Println("++++++++++++++++++++")

			c, err := ParseConfig(msg)
			if err != nil {
				// assume it is a YAML/JSON syntax error: report and ignore
				p.log.Warnf("could not parse config: %s", err.Error())
				continue
			}

			confCopy := stnrv1.StunnerConfig{}
			c.DeepCopyInto(&confCopy)

			p.log.Debugf("new config received from %q: %s", p.serverAddress, confCopy.String())

			resCh <- confCopy
		}
	}()

	// wait fo cancel
	for {
		select {
		case <-ctx.Done():
			// cancel: normal return
			closePinger <- struct{}{}

			return nil
		case err := <-errCh:
			// error: return it
			closePinger <- struct{}{}

			return err
		case conf := <-resCh:
			// new config: pass it along and move on
			p.log.Debugf("new config available: %s", conf.String())
			ch <- conf

			continue
		}
	}
}

// getConfigLocation returns a valid URL from config server address, either for a single HTTP GET
// to query the config discovery server for a single config file or a websocket URL and client for
// polling config file updates
func getConfigDiscoveryLocation(addr, id string, ws bool) (string, string, error) {
	u, err := url.Parse(addr)
	if err != nil {
		return "", "", fmt.Errorf("invalid config discovery server URL %q: %w", addr, err)
	}

	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return "", "", fmt.Errorf("invalid config discovery query server URL %q: %w", addr, err)
	}

	// add our id as a query parameter
	q.Set("id", id)
	u.RawQuery = q.Encode()

	// TODO: share between server and client
	u.Path = "/api/v1/config"
	if ws {
		u.Scheme = "ws"
		u.Path = u.Path + "/watch"
	} else {
		u.Scheme = "http"
	}

	// target URL
	location := u.String()

	// client
	u.Scheme = "http"
	u.RawQuery = ""
	client := u.String()

	return location, client, nil
}
