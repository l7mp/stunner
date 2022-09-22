package monitoring

import (
	"github.com/pion/logging"
)

type MockFrontendImpl struct {
	Endpoint string
}

func NewMockFrontend() Frontend {
	b := &MockFrontendImpl{Endpoint: "mock!"}
	return b
}

func (b *MockFrontendImpl) Reload(endpoint string, log logging.LeveledLogger) Frontend { return b }

func (b *MockFrontendImpl) Start(log logging.LeveledLogger) {}

func (b *MockFrontendImpl) Stop(log logging.LeveledLogger) {}
