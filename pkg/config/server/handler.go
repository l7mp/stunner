package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/l7mp/stunner/pkg/config/server/api"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

type ConfigList = api.V1ConfigList

// make sure the server satisfies the generate OpenAPI server interface
var _ api.StrictServerInterface = (*Server)(nil)

// (GET /api/v1/license)
func (s *Server) GetV1LicenseStatus(ctx context.Context, request api.GetV1LicenseStatusRequestObject) (api.GetV1LicenseStatusResponseObject, error) {
	s.log.V(1).Info("Handling GetV1LicenseStatus API call")
	return api.GetV1LicenseStatus200JSONResponse(s.licenseStore.Get()), nil
}

// (GET /api/v1/configs)
func (s *Server) ListV1Configs(ctx context.Context, request api.ListV1ConfigsRequestObject) (api.ListV1ConfigsResponseObject, error) {
	s.log.V(1).Info("Handling ListV1Configs API call")

	configs := s.configs.Snapshot() // deepcopies
	response := ConfigList{Version: "v1", Items: []stnrv1.StunnerConfig{}}
	for _, c := range configs {
		response.Items = append(response.Items, *c.Config)
	}

	s.log.V(3).Info("ListV1Configs API handler: ready", "configlist-len", len(configs))

	return api.ListV1Configs200JSONResponse(response), nil
}

// (GET /api/v1/configs/{namespace})
func (s *Server) ListV1ConfigsNamespace(ctx context.Context, request api.ListV1ConfigsNamespaceRequestObject) (api.ListV1ConfigsNamespaceResponseObject, error) {
	s.log.V(1).Info("Handling ListV1ConfigsNamespace API call", "namespace", request.Namespace)

	configs := s.configs.Snapshot() // deepcopies
	response := ConfigList{Version: "v1", Items: []stnrv1.StunnerConfig{}}
	for _, c := range configs {
		if c.Namespace == request.Namespace {
			response.Items = append(response.Items, *c.Config)
		}
	}

	s.log.V(3).Info("ListV1ConfigsNamespace API handler: ready", "configlist-len", len(configs))

	return api.ListV1ConfigsNamespace200JSONResponse(response), nil
}

// (GET /api/v1/configs/{namespace}/{name})
func (s *Server) GetV1ConfigNamespaceName(ctx context.Context, request api.GetV1ConfigNamespaceNameRequestObject) (api.GetV1ConfigNamespaceNameResponseObject, error) {
	namespace, name := request.Namespace, request.Name
	s.log.V(1).Info("Handling GetV1ConfigNamespaceName API call", "namespace", namespace,
		"name", name)

	c, ok := s.configs.Get(namespace, name)
	if !ok {
		s.log.V(1).Info("GetV1ConfigNamespaceName: Config not found", "namespace", namespace, "name", name)
		return api.GetV1ConfigNamespaceName404JSONResponse{
			Code:    http.StatusNotFound,
			Message: fmt.Sprintf("Config not found for ID %s/%s", namespace, name),
		}, nil
	}

	if s.patcher != nil && request.Params.Node != nil {
		c.Config = s.patcher(c.Config, *request.Params.Node)
		s.log.V(4).Info("GetV1ConfigNamespaceName: patch config", "config", c.String())
	}

	s.log.V(3).Info("GetV1ConfigNamespaceName API handler: ready", "config", c.String())

	return api.GetV1ConfigNamespaceName200JSONResponse(*c.Config), nil
}
