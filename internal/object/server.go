package object

import (
	"fmt"

	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server/l4"
	"github.com/l7mp/stunner/v2/internal/server/rtp"
	"github.com/l7mp/stunner/v2/internal/server/sip"
	"github.com/l7mp/stunner/v2/internal/server/srt"
	"github.com/l7mp/stunner/v2/internal/server/turn"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// NewServer creates the server object of the type a config names.
func NewServer(conf stnrv2.Config, rt *runtime.Runtime) (runtime.Object, error) {
	req, ok := conf.(*stnrv2.ServerConfig)
	if !ok {
		return nil, stnrv2.ErrInvalidConf
	}
	if err := req.Validate(); err != nil {
		return nil, err
	}
	t, _ := stnrv2.NewServerType(req.Type)
	switch t {
	case stnrv2.ServerTypeTURN:
		s, err := turn.New(req, rt)
		if err != nil {
			return nil, err
		}
		return s, nil
	case stnrv2.ServerTypeL4:
		s, err := l4.New(req, rt)
		if err != nil {
			return nil, err
		}
		return s, nil
	case stnrv2.ServerTypeSIP:
		return sip.New(req, rt)
	case stnrv2.ServerTypeRTP:
		return rtp.New(req, rt)
	case stnrv2.ServerTypeSRT:
		return srt.New(req, rt)
	}
	return nil, fmt.Errorf("unsupported server type %q", req.Type)
}
