package server

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/l7mp/stunner/pkg/config/server/api"
)

func (s *Server) WSUpgradeMiddleware(next api.StrictHandlerFunc, operationID string) api.StrictHandlerFunc {
	return func(ctx context.Context, w http.ResponseWriter, r *http.Request, request interface{}) (interface{}, error) {
		var filter ConfigFilter
		var patcher ClientConfigPatcher
		watch := false

		switch operationID {
		case "GetV1LicenseStatus":
			return next(ctx, w, r, request)

		case "GetV1ConfigNamespaceName":
			param, ok := request.(api.GetV1ConfigNamespaceNameRequestObject)
			if !ok {
				return nil, fmt.Errorf("unexpected parameters in API operation %q",
					operationID)
			}

			filter = func(confID string) bool {
				id := fmt.Sprintf("%s/%s", param.Namespace, param.Name)
				return confID == id
			}

			watch = param.Params.Watch != nil && *param.Params.Watch

			if s.patch != nil && param.Params.Node != nil {
				patcher = func(conf *stnrv1.StunnerConfig) (*stnrv1.StunnerConfig, error) {
					return s.patch(conf, *param.Params.Node)
				}
			}

		case "ListV1ConfigsNamespace":
			param, ok := request.(api.ListV1ConfigsNamespaceRequestObject)
			if !ok {
				return nil, fmt.Errorf("unexpected parameters in API operation %q",
					operationID)
			}

			filter = func(confID string) bool {
				ps := strings.Split(confID, "/")
				return len(ps) == 2 && ps[0] == param.Namespace
			}

			watch = param.Params.Watch != nil && *param.Params.Watch

		case "ListV1Configs":
			param, ok := request.(api.ListV1ConfigsRequestObject)
			if !ok {
				return nil, fmt.Errorf("unexpected parameters in API operation %q",
					operationID)
			}

			filter = func(confID string) bool {
				return true
			}

			watch = param.Params.Watch != nil && *param.Params.Watch

		default:
			return nil, fmt.Errorf("invalid API operation %q",
				operationID)
		}

		if !watch {
			return next(ctx, w, r, request)
		}

		s.log.V(4).Info("WS upgrade middleware: upgrading connection", "client", r.RemoteAddr)

		// upgrade to webSocket
		upgrader := websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		}

		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return nil, err
		}

		s.handleConn(ctx, conn, operationID, filter, patcher)

		return nil, nil
	}
}
