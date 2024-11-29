package licensemanager

import (
	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var constructor = NewStub
var _ Manager = &baseManager{}

type Manager interface {
	Update(config *stnrv1.LicenseConfig)
	Close()
	Validate(feature string) bool
	GetTier() string
	GetConfig() *stnrv1.LicenseConfig
	Status() string
}

func New(log logging.LeveledLogger) Manager {
	return constructor(log)
}

// baseManager implements the basic functionality so that all license manager implementations can embed it
type baseManager struct {
	config *stnrv1.LicenseConfig
	log    logging.LeveledLogger
}

func newBaseManager(log logging.LeveledLogger) baseManager {
	m := baseManager{log: log}
	return m
}

func (m *baseManager) Update(config *stnrv1.LicenseConfig) { m.config = config }
func (m *baseManager) Close()                              {}
func (m *baseManager) Validate(feature string) bool        { return false }
func (m *baseManager) GetTier() string                     { return "<N/A>" }
func (m *baseManager) GetConfig() *stnrv1.LicenseConfig    { return m.config }
func (m *baseManager) Status() string                      { return "<N/A>" }
