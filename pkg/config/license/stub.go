package license

import (
	"github.com/pion/logging"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

var _ ConfigManager = &Stub{}

// Stub is the default license manager that implements the free/open-source tier.
type Stub struct{ BaseManager }

func NewStub(log logging.LeveledLogger) ConfigManager {
	s := &Stub{BaseManager: newBaseManager(log)}
	return s
}

func (s *Stub) Reconcile(config *stnrv2.LicenseConfig) {
	s.log.Tracef("licensing status update triggered using config %q", stnrv2.LicensingStatus(config))
	s.BaseManager.Reconcile(config)
}
