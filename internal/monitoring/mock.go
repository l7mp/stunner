package monitoring

import (
	"github.com/pion/logging"
)

type MockBackendImpl struct {
	Endpoint string
}

func NewMockBackend() Backend {
	b := &MockBackendImpl{Endpoint: "mock!"}
	return b
}

func (b *MockBackendImpl) Reload(endpoint string, log logging.LeveledLogger) Backend { return b }

func (b *MockBackendImpl) Start() {}

func (b *MockBackendImpl) Stop() {}
