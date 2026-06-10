package turn

import (
	"net"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// OffloadHandler surfaces channel lifecycle events from TURN into offload engines.
type OffloadHandler interface {
	Start() error
	Close() error
	HandleChannelCreate(net.Addr, net.Addr, string, string, string, net.Addr, net.Addr, uint16, string, string)
	HandleChannelDelete(net.Addr, net.Addr, string, string, string, net.Addr, net.Addr, uint16)
	Status() *stnrv1.OffloadStatus
}

type offloadHandlerStub struct{}

// Start is a no-op for the stub offload handler.
func (o *offloadHandlerStub) Start() error { return nil }

// Close is a no-op for the stub offload handler.
func (o *offloadHandlerStub) Close() error { return nil }

// HandleChannelCreate ignores channel-create events.
func (o *offloadHandlerStub) HandleChannelCreate(_, _ net.Addr, _, _, _ string, _, _ net.Addr, _ uint16, _, _ string) {
}

// HandleChannelDelete ignores channel-delete events.
func (o *offloadHandlerStub) HandleChannelDelete(_, _ net.Addr, _, _, _ string, _, _ net.Addr, _ uint16) {
}

// Status returns an empty offload status snapshot.
func (o *offloadHandlerStub) Status() *stnrv1.OffloadStatus {
	return &stnrv1.OffloadStatus{
		Listeners: map[string]stnrv1.OffloadDirStat{},
		Clusters:  map[string]stnrv1.OffloadDirStat{},
	}
}

// NewOffloadHandlerStub returns the default no-op offload handler.
func NewOffloadHandlerStub() OffloadHandler {
	return &offloadHandlerStub{}
}
