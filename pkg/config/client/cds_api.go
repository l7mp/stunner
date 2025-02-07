//go:generate go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen --config=cfg.yaml ../api/stunner_openapi.yaml
package client

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/config/client/api"
	"github.com/l7mp/stunner/pkg/config/util"
	"github.com/pion/logging"
)

const (
	ConfigNamespaceNameAPIEndpoint = "/api/v1/configs/%s/%s"
	ConfigsNamespaceAPIEndpoint    = "/api/v1/configs/%s"
	AllConfigsAPIEndpoint          = "/api/v1/configs"
	LicenseStatusEndpoint          = "/api/v1/license"
)

type ConfigList struct {
	Version string                  `json:"version"`
	Items   []*stnrv1.StunnerConfig `json:"items"`
}

type ClientOption = api.ClientOption
type HttpRequestDoer = api.HttpRequestDoer

type CdsApi interface {
	// Endpoint returns the address of the server plus the WebSocket API endpoint.
	Endpoint() (string, string)
	// Get loads the config(s) from the API endpoint.
	Get(ctx context.Context) ([]*stnrv1.StunnerConfig, error)
	// Watch watches config(s) from the API endpoint of a CDS server. If the server is not
	// available watch will retry, and if the connection goes away it will create a new one. If
	// set, the suppressDelete instructs the API to ignore config delete updates from the
	// server.
	Watch(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error
	// Poll creates a one-shot config watcher without the retry mechanincs of Watch.
	Poll(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error
	logging.LeveledLogger
}

func WithHTTPClient(doer HttpRequestDoer) ClientOption { return api.WithHTTPClient(doer) }

// AllConfigsAPI is the API for listing all configs in a namespace.
type AllConfigsAPI struct {
	addr, httpURI, wsURI string
	client               *api.ClientWithResponses
	logging.LeveledLogger
}

func NewAllConfigsAPI(addr string, logger logging.LeveledLogger, opts ...ClientOption) (CdsApi, error) {
	httpuri, err := getURI(addr)
	if err != nil {
		return nil, err
	}

	wsuri, err := wsURI(addr, AllConfigsAPIEndpoint)
	if err != nil {
		return nil, err
	}

	client, err := api.NewClientWithResponses(httpuri.String(), opts...)
	if err != nil {
		return nil, err
	}

	return &AllConfigsAPI{
		addr:          addr,
		httpURI:       httpuri.String(),
		wsURI:         wsuri,
		client:        client,
		LeveledLogger: logger,
	}, nil
}

func (a *AllConfigsAPI) Endpoint() (string, string) {
	return a.addr, a.wsURI
}

func (a *AllConfigsAPI) Get(ctx context.Context) ([]*stnrv1.StunnerConfig, error) {
	a.Debugf("GET: loading all configs from CDS server %s", a.addr)

	r, err := a.client.ListV1ConfigsWithResponse(ctx, nil)
	if err != nil {
		return []*stnrv1.StunnerConfig{}, err
	}

	if r.HTTPResponse.StatusCode != http.StatusOK {
		body := strings.TrimSpace(string(r.Body))
		return []*stnrv1.StunnerConfig{}, fmt.Errorf("HTTP error (status: %s): %s",
			r.HTTPResponse.Status, body)
	}

	return decodeConfigList(r.Body)
}

func (a *AllConfigsAPI) Watch(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	a.Debugf("WATCH: watching all configs from CDS server %s", a.wsURI)
	return watch(ctx, a, ch, suppressDelete)
}

func (a *AllConfigsAPI) Poll(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	a.Debugf("POLL: polling all configs from CDS server %s", a.wsURI)
	return poll(ctx, a, ch, suppressDelete)
}

// ConfigsNamespaceAPI is the API for listing all configs in a namespace.
type ConfigsNamespaceAPI struct {
	addr, namespace, httpURI, wsURI string
	client                          *api.ClientWithResponses
	logging.LeveledLogger
}

func NewConfigsNamespaceAPI(addr, namespace string, logger logging.LeveledLogger, opts ...ClientOption) (CdsApi, error) {
	httpuri, err := getURI(addr)
	if err != nil {
		return nil, err
	}

	wsuri, err := wsURI(addr, fmt.Sprintf(ConfigsNamespaceAPIEndpoint, namespace))
	if err != nil {
		return nil, err
	}

	client, err := api.NewClientWithResponses(httpuri.String(), opts...)
	if err != nil {
		return nil, err
	}

	return &ConfigsNamespaceAPI{
		addr:          addr,
		namespace:     namespace,
		httpURI:       httpuri.String(),
		wsURI:         wsuri,
		client:        client,
		LeveledLogger: logger,
	}, nil
}

func (a *ConfigsNamespaceAPI) Endpoint() (string, string) {
	return a.addr, a.wsURI
}

func (a *ConfigsNamespaceAPI) Get(ctx context.Context) ([]*stnrv1.StunnerConfig, error) {
	a.Debugf("GET: loading all configs in namespace %s from CDS server %s",
		a.namespace, a.addr)

	r, err := a.client.ListV1ConfigsNamespaceWithResponse(ctx, a.namespace, nil)
	if err != nil {
		return []*stnrv1.StunnerConfig{}, err
	}

	if r.HTTPResponse.StatusCode != http.StatusOK {
		body := strings.TrimSpace(string(r.Body))
		return []*stnrv1.StunnerConfig{}, fmt.Errorf("HTTP error (status: %s): %s",
			r.HTTPResponse.Status, body)
	}

	return decodeConfigList(r.Body)
}

func (a *ConfigsNamespaceAPI) Watch(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	a.Debugf("WATCH: watching all configs in namespace %s from CDS server %s",
		a.namespace, a.wsURI)
	return watch(ctx, a, ch, suppressDelete)
}

func (a *ConfigsNamespaceAPI) Poll(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	a.Debugf("POLL: polling all configs in namespace %s from CDS server %s",
		a.namespace, a.wsURI)
	return poll(ctx, a, ch, suppressDelete)
}

type ConfigNamespaceNameAPI struct {
	addr, namespace, name, httpURI, wsURI string
	client                                *api.ClientWithResponses
	logging.LeveledLogger
}

func NewConfigNamespaceNameAPI(addr, namespace, name string, logger logging.LeveledLogger, opts ...ClientOption) (CdsApi, error) {
	httpuri, err := getURI(addr)
	if err != nil {
		return nil, err
	}

	wsuri, err := wsURI(addr, fmt.Sprintf(ConfigNamespaceNameAPIEndpoint, namespace, name))
	if err != nil {
		return nil, err
	}

	client, err := api.NewClientWithResponses(httpuri.String(), opts...)
	if err != nil {
		return nil, err
	}

	return &ConfigNamespaceNameAPI{
		addr:          addr,
		namespace:     namespace,
		name:          name,
		httpURI:       httpuri.String(),
		wsURI:         wsuri,
		client:        client,
		LeveledLogger: logger,
	}, nil
}

func (a *ConfigNamespaceNameAPI) Endpoint() (string, string) {
	return a.addr, a.wsURI
}

func (a *ConfigNamespaceNameAPI) Get(ctx context.Context) ([]*stnrv1.StunnerConfig, error) {
	a.Debugf("GET: loading config for gateway %s/%s from CDS server %s",
		a.namespace, a.name, a.addr)

	var params *api.GetV1ConfigNamespaceNameParams
	r, err := a.client.GetV1ConfigNamespaceNameWithResponse(ctx, a.namespace, a.name, params)
	if err != nil {
		return []*stnrv1.StunnerConfig{}, err
	}

	if r.HTTPResponse.StatusCode != http.StatusOK {
		body := strings.TrimSpace(string(r.Body))
		return []*stnrv1.StunnerConfig{}, fmt.Errorf("HTTP error (status: %s): %s",
			r.HTTPResponse.Status, body)
	}

	return decodeConfig(r.Body)
}

func (a *ConfigNamespaceNameAPI) Watch(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	a.Debugf("WATCH: watching config for gateway %s/%s from CDS server %s",
		a.namespace, a.name, a.wsURI)
	return watch(ctx, a, ch, suppressDelete)
}

func (a *ConfigNamespaceNameAPI) Poll(ctx context.Context, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	a.Debugf("POLL: polling config for gateway %s/%s from CDS server %s",
		a.namespace, a.name, a.wsURI)
	return poll(ctx, a, ch, suppressDelete)
}

func watch(ctx context.Context, a CdsApi, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	go func() {
		for {
			if err := poll(ctx, a, ch, suppressDelete); err != nil {
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

// ////////////
// API workers
// ////////////
func poll(ctx context.Context, a CdsApi, ch chan<- *stnrv1.StunnerConfig, suppressDelete bool) error {
	_, url := a.Endpoint()
	a.Tracef("poll: trying to open connection to CDS server at %s", url)

	wc, _, err := websocket.DefaultDialer.DialContext(ctx, url, makeHeader(url))
	if err != nil {
		return err
	}
	// wrap with a locker to prevent concurrent writes
	conn := util.NewConn(wc)
	defer conn.Close() // this will close the poller goroutine

	a.Infof("connection successfully opened to config discovery server at %s", url)

	pingTicker := time.NewTicker(PingPeriod)
	closePinger := make(chan any)
	defer close(closePinger)

	// wait until all threads are closed and we can remove the error channel
	errCh := make(chan error, 1)
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer pingTicker.Stop()
		defer wg.Done()

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
		defer wg.Done()

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

			c, err := ParseConfig(msg)
			if err != nil {
				// assume it is a YAML/JSON syntax error: report and ignore
				a.Warnf("could not parse config: %s", err.Error())
				continue
			}

			if err := c.Validate(); err != nil {
				a.Warnf("invalid config: %s", err.Error())
				continue
			}

			if suppressDelete && IsConfigDeleted(c) {
				a.Infof("Ignoring delete configuration update from %q", url)
				continue
			}

			a.Debugf("new config received from %q: %q", url, c.String())

			ch <- c
		}
	}()

	// wait fo cancel
	for {
		defer func() {
			a.Infof("closing connection to server %s", conn.RemoteAddr().String())
			conn.WriteMessage(websocket.CloseMessage, []byte{}) //nolint:errcheck
			conn.Close()
			closePinger <- struct{}{}
			wg.Wait()
			close(errCh)
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
