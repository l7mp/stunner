// Package sip implements the SIP server, a stateful SIP proxy handing the SDP of every call to a
// media proxy.
// The open-source build has no implementation; a premium build replaces constructor.
package sip

import (
	"github.com/l7mp/stunner/v2/internal/runtime"
	"github.com/l7mp/stunner/v2/internal/server"
	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// constructor builds the SIP server of a validated config. The open-source default serves no conn
// and warns on start.
var constructor = func(conf *stnrv2.ServerConfig, rt *runtime.Runtime) (runtime.Object, error) {
	return server.NewUnimplemented(conf, rt)
}

// New creates the SIP server of a validated config.
func New(conf *stnrv2.ServerConfig, rt *runtime.Runtime) (runtime.Object, error) {
	return constructor(conf, rt)
}
