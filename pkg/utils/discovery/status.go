package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	stnrv1 "github.com/l7mp/stunner/v2/pkg/apis/v1"
)

// statusTimeout bounds one status query to a stunnerd instance.
const statusTimeout = 5 * time.Second

// GetStunnerdStatus fetches the runtime status of the stunnerd instance whose health-check
// endpoint is served at addr (host:port), typically the local end of a port-forward opened by
// DiscoverK8sStunnerdPods.
func GetStunnerdStatus(ctx context.Context, addr string) (*stnrv1.StunnerStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("http://%s/status", addr), nil)
	if err != nil {
		return nil, err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("error querying status at %s: %w", addr, err)
	}
	defer res.Body.Close() //nolint:errcheck
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status query at %s failed with HTTP error code %s", addr, res.Status)
	}
	s := &stnrv1.StunnerStatus{}
	if err := json.NewDecoder(res.Body).Decode(s); err != nil {
		return nil, fmt.Errorf("could not decode status response from %s: %w", addr, err)
	}
	return s, nil
}
