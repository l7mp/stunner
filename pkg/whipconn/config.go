package whipconn

import (
	"fmt"
	"hash/fnv"
	"net/url"

	"github.com/pion/webrtc/v4"
)

const (
	messageSize  = 2048
	whipEndpoint = "/whip"
)

type Config struct {
	ICEServers         []webrtc.ICEServer
	ICETransportPolicy webrtc.ICETransportPolicy
	Token, Endpoint    string
}

func makeURL(addr, endpoint string) *url.URL {
	return &url.URL{
		Scheme: "http",
		Host:   addr,
		Path:   endpoint,
	}
}

func resourceHash(s string) string {
	h := fnv.New32a()
	h.Write([]byte(s))
	return fmt.Sprintf("/%d", h.Sum32())
}

func makeResourceURL(endpoint, id string) string {
	return endpoint + id
}
