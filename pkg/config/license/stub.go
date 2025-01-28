package license

import (
	"fmt"

	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var _ ConfigManager = &Stub{}

// Stub is the default license manager that implements the free/open-source tier.
type Stub struct{ baseManager }

func NewStub(log logging.LeveledLogger) ConfigManager {
	s := &Stub{baseManager: newBaseManager(log)}
	return s
}

func (s *Stub) Reconcile(config *stnrv1.LicenseConfig) {
	s.log.Tracef("Licensing status update triggered using config %q", stnrv1.LicensingStatus(config))
	s.baseManager.Reconcile(config)
}

func (s *Stub) Status() string {
	return fmt.Sprintf("{tier=%q}", s.SubscriptionType())
}
