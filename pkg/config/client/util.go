package client

import (
	"encoding/json"
	"net/url"
	"slices"
	"strings"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// decodeConfig decodes a config served by the CDS server, of either version.
func decodeConfig(r []byte) ([]*stnrv2.StunnerConfig, error) {
	c, err := ParseConfig(r)
	if err != nil {
		return nil, err
	}
	return []*stnrv2.StunnerConfig{c}, nil
}

// decodeConfigList decodes a config list served by the CDS server, each item of either version.
func decodeConfigList(r []byte) ([]*stnrv2.StunnerConfig, error) {
	l := ConfigList{}
	if err := json.Unmarshal(r, &l); err != nil {
		return nil, err
	}
	ret := make([]*stnrv2.StunnerConfig, 0, len(l.Items))
	for _, item := range l.Items {
		c, err := ParseConfig(item)
		if err != nil {
			return nil, err
		}
		ret = append(ret, c)
	}
	return ret, nil
}

// getURI parses a config origin into a URL. The origin is one of: (1) a bare network address
// "host:port", optionally followed by a "/path", which defaults to the http scheme, (2) a full URL
// carrying an explicit scheme (http, https, ws, wss or file), or (3) a file path. IPv6 hosts must be
// given in RFC 3986 bracketed form, e.g. "[::1]:port" or "http://[::1]:port"; url.Parse rejects an
// unbracketed IPv6 host.
func getURI(addr string) (*url.URL, error) {
	if !strings.Contains(addr, "://") {
		// no scheme: treat addr as a bare network address and default to http
		addr = "http://" + addr
	}
	return url.Parse(addr)
}

// wsURI converts a config origin into a websocket URL for the given API
// endpoint. The scheme is mapped to its websocket equivalent: https and wss
// become wss, everything else becomes ws.
func wsURI(addr, endpoint string, labels map[string]string) (string, error) {
	uri, err := getURI(addr)
	if err != nil {
		return "", err
	}

	switch uri.Scheme {
	case "https", "wss":
		uri.Scheme = "wss"
	default:
		uri.Scheme = "ws"
	}
	uri.Path = endpoint
	v := url.Values{}
	v.Set("watch", "true")
	for _, l := range labelParams(labels) {
		v.Add("label", l)
	}
	uri.RawQuery = v.Encode()

	return uri.String(), nil
}

// labelParams renders client labels as the label query parameters, key=value, sorted.
func labelParams(labels map[string]string) []string {
	ret := make([]string, 0, len(labels))
	for k, v := range labels {
		ret = append(ret, k+"="+v)
	}
	slices.Sort(ret)
	return ret
}
