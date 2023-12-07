package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/l7mp/stunner/pkg/config/server/api"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// make sure the server satisfies the generate OpenAPI server interface
var _ api.ServerInterface = &Server{}

// ResponseGen is a callback to generate a response to a request (also used for sending the initial config dump for watches).
type ResponseGen func() ([]byte, *Error)

// FilterConfig is a callback to filter config updates for a client.
type FilterConfig func(confId string) bool

// (GET /api/v1/configs)
func (s *Server) ListV1Configs(w http.ResponseWriter, r *http.Request, params api.ListV1ConfigsParams) {
	endpoint := "/api/v1/configs"

	responder := func() ([]byte, *Error) {
		configs := s.configs.Snapshot()
		response := ConfigList{Version: "v1", Items: []*stnrv1.StunnerConfig{}}
		for _, c := range configs {
			response.Items = append(response.Items, c.Config)
		}

		json, err := json.Marshal(response)
		if err != nil {
			return []byte{}, &Error{
				Code:    http.StatusInternalServerError,
				Message: "Could not JSON marshal config list",
			}
		}

		return json, nil
	}

	filter := func(confId string) bool {
		return true
	}

	if params.Watch != nil && *params.Watch {
		s.handleConn(w, r, endpoint, responder, filter)
	} else {
		s.handleReq(w, r, endpoint, responder)
	}
}

// (GET /api/v1/configs/{namespace})
func (s *Server) ListV1ConfigsNamespace(w http.ResponseWriter, r *http.Request, namespace string, params api.ListV1ConfigsNamespaceParams) {
	endpoint := fmt.Sprintf("/api/v1/configs/%s", namespace)

	responder := func() ([]byte, *Error) {
		configs := s.configs.Snapshot()
		response := ConfigList{Version: "v1", Items: []*stnrv1.StunnerConfig{}}
		for _, c := range configs {
			ps := strings.Split(c.Id, "/")
			if len(ps) == 2 && ps[0] == namespace {
				response.Items = append(response.Items, c.Config)
			}
		}

		json, err := json.Marshal(response)
		if err != nil {
			return []byte{}, &Error{
				Code:    http.StatusInternalServerError,
				Message: "Could not JSON marshal config list",
			}
		}

		return json, nil
	}
	filter := func(confId string) bool {
		ps := strings.Split(confId, "/")
		return len(ps) == 2 && ps[0] == namespace
	}

	if params.Watch != nil && *params.Watch {
		s.handleConn(w, r, endpoint, responder, filter)
	} else {
		s.handleReq(w, r, endpoint, responder)
	}
}

// (GET /api/v1/configs/{namespace}/{name})
func (s *Server) GetV1ConfigNamespaceName(w http.ResponseWriter, r *http.Request, namespace string, name string, params api.GetV1ConfigNamespaceNameParams) {
	id := fmt.Sprintf("%s/%s", namespace, name)
	endpoint := fmt.Sprintf("/api/v1/configs/%s/%s", namespace, name)

	responder := func() ([]byte, *Error) {
		c := s.configs.Get(id)
		if c == nil {
			return []byte{}, &Error{
				Code:    http.StatusBadRequest,
				Message: "Config not found",
			}
		}

		json, err := json.Marshal(c)
		if err != nil {
			return []byte{}, &Error{
				Code:    http.StatusInternalServerError,
				Message: "Config not found",
			}
		}

		return json, nil
	}
	filter := func(confId string) bool { return confId == id }

	if params.Watch != nil && *params.Watch {
		s.handleConn(w, r, endpoint, responder, filter)
	} else {
		s.handleReq(w, r, endpoint, responder)
	}
}
