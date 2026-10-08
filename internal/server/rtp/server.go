// Package rtp implements the RTP server, a media proxy relaying the media of the calls a SIP server
// sets up.
// The open-source build has no implementation; a premium build replaces constructor.
package rtp

import (
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// constructor builds the RTP server of a validated config. The open-source default serves no conn
// and warns on start.
var constructor = func(conf *stnrv2.ServerConfig, rt *runtime.Runtime) (runtime.Object, error) {
	return server.NewUnimplemented(conf, rt)
}

// New creates the RTP server of a validated config.
func New(conf *stnrv2.ServerConfig, rt *runtime.Runtime) (runtime.Object, error) {
	return constructor(conf, rt)
}
