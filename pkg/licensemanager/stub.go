package licensemanager

import (
	"fmt"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	"github.com/pion/logging"
)

var _ Manager = &Stub{}

// Stub is the default license manager that implements the free/open-source tier.
type Stub struct{ baseManager }

func NewStub(log logging.LeveledLogger) Manager {
	s := &Stub{baseManager: newBaseManager(log)}
	return s
}

func (s *Stub) Update(config *stnrv1.LicenseConfig) {
	s.log.Tracef("Licensing status update triggered using config %q", stnrv1.LicensingStatus(config))
	s.baseManager.Update(config)
}

func (s *Stub) GetTier() string { return "free" }

func (s *Stub) Status() string {
	return fmt.Sprintf("{tier=%q,conf=%s}", s.GetTier(), stnrv1.LicensingStatus(s.GetConfig()))
}
