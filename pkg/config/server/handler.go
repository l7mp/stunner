package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/l7mp/stunner/pkg/config/server/api"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// make sure the server satisfies the generate OpenAPI server interface
var _ api.StrictServerInterface = (*Server)(nil)

// ConfigFilter is a callback to filter config updates for a client.
type ConfigFilter func(confId string) bool

// ConfigPatcher is a callback to patch config updates for a client.
type ConfigPatcher func(conf *stnrv1.StunnerConfig, node string) (*stnrv1.StunnerConfig, error)

// (GET /api/v1/configs)
func (s *Server) ListV1Configs(ctx context.Context, request api.ListV1ConfigsRequestObject) (api.ListV1ConfigsResponseObject, error) {
	s.log.V(1).Info("handling ListV1Configs API call")

	configs := s.configs.Snapshot()
	response := ConfigList{Version: "v1", Items: []stnrv1.StunnerConfig{}}
	for _, c := range configs {
		cpy := stnrv1.StunnerConfig{}
		c.Config.DeepCopyInto(&cpy)
		response.Items = append(response.Items, cpy)
	}

	s.log.V(3).Info("ListV1Configs API handler: ready",
		"configlist-len", len(configs))

	return api.ListV1Configs200JSONResponse(response), nil
}

// (GET /api/v1/configs/{namespace})
func (s *Server) ListV1ConfigsNamespace(ctx context.Context, request api.ListV1ConfigsNamespaceRequestObject) (api.ListV1ConfigsNamespaceResponseObject, error) {
	s.log.V(1).Info("handling ListV1ConfigsNamespace API call",
		"namespace", request.Namespace)

	configs := s.configs.Snapshot()
	response := ConfigList{Version: "v1", Items: []stnrv1.StunnerConfig{}}
	for _, c := range configs {
		ps := strings.Split(c.Id, "/")
		if len(ps) == 2 && ps[0] == request.Namespace {
			cpy := stnrv1.StunnerConfig{}
			c.Config.DeepCopyInto(&cpy)
			response.Items = append(response.Items, cpy)
		}
	}

	s.log.V(3).Info("ListV1ConfigsNamespace API handler: ready",
		"configlist-len", len(configs))

	return api.ListV1ConfigsNamespace200JSONResponse(response), nil
}

// (GET /api/v1/configs/{namespace}/{name})
func (s *Server) GetV1ConfigNamespaceName(ctx context.Context, request api.GetV1ConfigNamespaceNameRequestObject) (api.GetV1ConfigNamespaceNameResponseObject, error) {
	namespace, name := request.Namespace, request.Name
	s.log.V(1).Info("handling GetV1ConfigNamespaceName API call", "namespace", namespace,
		"name", name)

	id := fmt.Sprintf("%s/%s", namespace, name)
	c := s.configs.Get(id)
	if c == nil {
		s.log.V(1).Info("GetV1ConfigNamespaceName: config not found", "client", id)
		return nil, fmt.Errorf("config not found for id %q", id)
	}

	ret := &stnrv1.StunnerConfig{}
	c.DeepCopyInto(ret)

	if s.patch != nil && request.Params.Node != nil {
		conf, err := s.patch(ret, *request.Params.Node)
		if err != nil {
			s.log.Error(err, "GetV1ConfigNamespaceName: patch config failed")
			return nil, err
		}
		ret = conf
	}

	s.log.V(3).Info("GetV1ConfigNamespaceName API handler: ready",
		"config", ret.String())

	return api.GetV1ConfigNamespaceName200JSONResponse(*ret), nil
}
