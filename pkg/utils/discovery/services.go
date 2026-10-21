package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// serviceTimeout bounds one query to a stunnerd instance or the auth service.
const serviceTimeout = 5 * time.Second

// get queries a service and returns the body of its response, or an error for any response but
// 200 OK.
func get(ctx context.Context, u string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, serviceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error %s", res.Status)
	}
	return io.ReadAll(res.Body)
}

// GetStunnerdStatus fetches the runtime status of the stunnerd instance whose health-check
// endpoint is served at addr (host:port), typically the local end of a port-forward opened by
// DiscoverK8sStunnerdPods.
func GetStunnerdStatus(ctx context.Context, addr string) (*stnrv2.StunnerStatus, error) {
	b, err := get(ctx, fmt.Sprintf("http://%s/status", addr))
	if err != nil {
		return nil, fmt.Errorf("error querying status at %s: %w", addr, err)
	}
	s := &stnrv2.StunnerStatus{}
	if err := json.Unmarshal(b, s); err != nil {
		return nil, fmt.Errorf("could not decode status response from %s: %w", addr, err)
	}
	return s, nil
}

// AuthQuery is a request to the auth service: the ICE server configuration, or with TURNCredential
// a TURN credential in the format of the REST API for TURN, of the Gateways of a namespace or of
// one Gateway, optionally for a user. Empty fields are left out of the query.
type AuthQuery struct {
	Namespace, Gateway, Username string
	TURNCredential               bool
}

// GetAuth queries the auth service served at addr (host:port), typically the local end of a
// port-forward opened by DiscoverK8sAuthServer, and returns its response: a JSON ICE server
// configuration or TURN credential.
func GetAuth(ctx context.Context, addr string, q AuthQuery) ([]byte, error) {
	u := url.URL{Scheme: "http", Host: addr, Path: "/ice"}
	if q.TURNCredential {
		u.Path = ""
	}
	v := url.Values{"service": {"turn"}}
	for k, val := range map[string]string{"namespace": q.Namespace, "gateway": q.Gateway, "username": q.Username} {
		if val != "" {
			v.Set(k, val)
		}
	}
	u.RawQuery = v.Encode()
	b, err := get(ctx, u.String())
	if err != nil {
		return nil, fmt.Errorf("error querying auth service at %s: %w", addr, err)
	}
	return b, nil
}
