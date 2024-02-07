package client

import (
	"encoding/json"
	"net/url"
	"strings"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

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
	// make sure we have a working HTTP scheme
	if !strings.HasPrefix(addr, "http://") && !strings.HasPrefix(addr, "https://") &&
		!strings.HasPrefix(addr, "ws://") && !strings.HasPrefix(addr, "wss://") &&
		!strings.HasPrefix(addr, "file://") {
		addr = "http://" + addr
	}

	url, err := url.Parse(addr)
	if err != nil {
		return nil, err
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
