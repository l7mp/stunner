//go:generate go run github.com/deepmap/oapi-codegen/v2/cmd/oapi-codegen --config=cfg.yaml ../api/stunner_openapi.yaml
package client

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/config/client/api"
	"github.com/pion/logging"
)

const (
	ConfigNamespaceNameAPIEndpoint = "/api/v1/configs/%s/%s"
	ConfigsNamespaceAPIEndpoint    = "/api/v1/configs/%s"
	AllConfigsAPIEndpoint          = "/api/v1/configs"
)

type ConfigList struct {
	Version string                  `json:"version"`
	Items   []*stnrv1.StunnerConfig `json:"items"`
}

type CDSAPI interface {
	// Endpoint returns the address of the server plus the WebSocket API endpoint.
	Endpoint() (string, string)
	// Get loads the config(s) from the API endpoint.
	Get(ctx context.Context) ([]*stnrv1.StunnerConfig, error)
	// Watch watches config(s) from the API endpoint of a CDS server. If the server is not
	// available watch will retry, and if the connection goes away it will create a new one.
	Watch(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error
	// Poll creates a one-shot config watcher without the retry mechanincs of Watch.
	Poll(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error
	logging.LeveledLogger
}

// AllConfigsAPI is the API for listing all configs in a namespace.
type AllConfigsAPI struct {
	addr, httpURI, wsURI string
	client               *api.ClientWithResponses
	logging.LeveledLogger
}

func NewAllConfigsAPI(addr string, logger logging.LeveledLogger) (CDSAPI, error) {
	httpuri, err := getURI(addr)
	if err != nil {
		return nil, err
	}

	wsuri, err := wsURI(addr, AllConfigsAPIEndpoint)
	if err != nil {
		return nil, err
	}

	client, err := api.NewClientWithResponses(httpuri.String())
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
		return []*stnrv1.StunnerConfig{}, fmt.Errorf("HTTP error: %s", r.HTTPResponse.Status)
	}

	return decodeConfigList(r.Body)
}

func (a *AllConfigsAPI) Watch(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	a.Debugf("WATCH: watching all configs from CDS server %s", a.wsURI)
	return watch(ctx, a, ch)
}

func (a *AllConfigsAPI) Poll(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	a.Debugf("POLL: polling all configs from CDS server %s", a.wsURI)
	return poll(ctx, a, ch)
}

// ConfigsNamespaceAPI is the API for listing all configs in a namespace.
type ConfigsNamespaceAPI struct {
	addr, namespace, httpURI, wsURI string
	client                          *api.ClientWithResponses
	logging.LeveledLogger
}

func NewConfigsNamespaceAPI(addr, namespace string, logger logging.LeveledLogger) (CDSAPI, error) {
	httpuri, err := getURI(addr)
	if err != nil {
		return nil, err
	}

	wsuri, err := wsURI(addr, fmt.Sprintf(ConfigsNamespaceAPIEndpoint, namespace))
	if err != nil {
		return nil, err
	}

	client, err := api.NewClientWithResponses(httpuri.String())
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
		return []*stnrv1.StunnerConfig{}, fmt.Errorf("HTTP error: %s",
			r.HTTPResponse.Status)
	}

	return decodeConfigList(r.Body)
}

func (a *ConfigsNamespaceAPI) Watch(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	a.Debugf("WATCH: watching all configs in namespace %s from CDS server %s",
		a.namespace, a.wsURI)
	return watch(ctx, a, ch)
}

func (a *ConfigsNamespaceAPI) Poll(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	a.Debugf("POLL: polling all configs in namespace %s from CDS server %s",
		a.namespace, a.wsURI)
	return poll(ctx, a, ch)
}

type ConfigNamespaceNameAPI struct {
	addr, namespace, name, httpURI, wsURI string
	client                                *api.ClientWithResponses
	logging.LeveledLogger
}

func NewConfigNamespaceNameAPI(addr, namespace, name string, logger logging.LeveledLogger) (CDSAPI, error) {
	httpuri, err := getURI(addr)
	if err != nil {
		return nil, err
	}

	wsuri, err := wsURI(addr, fmt.Sprintf(ConfigNamespaceNameAPIEndpoint, namespace, name))
	if err != nil {
		return nil, err
	}

	client, err := api.NewClientWithResponses(httpuri.String())
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

	r, err := a.client.GetV1ConfigNamespaceNameWithResponse(ctx, a.namespace, a.name, nil)
	if err != nil {
		return []*stnrv1.StunnerConfig{}, err
	}

	if r.HTTPResponse.StatusCode != http.StatusOK {
		return []*stnrv1.StunnerConfig{}, fmt.Errorf("HTTP error: %s",
			r.HTTPResponse.Status)
	}

	return decodeConfig(r.Body)
}

func (a *ConfigNamespaceNameAPI) Watch(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	a.Debugf("WATCH: watching config for gateway %s/%s from CDS server %s",
		a.namespace, a.name, a.wsURI)
	return watch(ctx, a, ch)
}

func (a *ConfigNamespaceNameAPI) Poll(ctx context.Context, ch chan<- stnrv1.StunnerConfig) error {
	a.Debugf("POLL: polling config for gateway %s/%s from CDS server %s",
		a.namespace, a.name, a.wsURI)
	return poll(ctx, a, ch)
}

func decodeConfig(r []byte) ([]*stnrv1.StunnerConfig, error) {
	c := stnrv1.StunnerConfig{}
	if err := json.Unmarshal(r, &c); err != nil {
		return nil, err
	}

	// copy

	return []*stnrv1.StunnerConfig{&c}, nil
}

func decodeConfigList(r []byte) ([]*stnrv1.StunnerConfig, error) {
	l := ConfigList{}
	if err := json.Unmarshal(r, &l); err != nil {
		return nil, err
	}
	return l.Items, nil
}

// getURI tries to parse an address or an URL or a file name into an URL.
func getURI(addr string) (*url.URL, error) {
	url, err := url.Parse(addr)
	if err != nil {
		// try to parse with a http scheme as a last resort
		u, err2 := url.Parse("http://" + addr)
		if err2 != nil {
			return nil, err
		}
		url = u
	}
	return url, nil
}

// wsURI returns a websocket url from a HTTP URI.
func wsURI(addr, endpoint string) (string, error) {
	uri, err := getURI(addr)
	if err != nil {
		return "", err
	}

	uri.Scheme = "ws"
	uri.Path = endpoint
	v := url.Values{}
	v.Set("watch", "true")
	uri.RawQuery = v.Encode()

	return uri.String(), nil
}
