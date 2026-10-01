package server

import (
	"sync"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

// UpdateLicenseStatus updates the licensing status that is served by the server.
func (s *Server) UpdateLicenseStatus(status stnrv2.LicenseStatus) {
	s.log.V(4).Info("processing license status update", "status", status.String())
	s.licenseStore.Upsert(status)
}

type LicenseStore struct {
	status stnrv2.LicenseStatus
	lock   sync.RWMutex
}

func NewLicenseStore() *LicenseStore {
	return &LicenseStore{status: stnrv2.NewEmptyLicenseStatus()}
}

func (t *LicenseStore) Get() stnrv2.LicenseStatus {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.status
}

func (t *LicenseStore) Upsert(s stnrv2.LicenseStatus) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.status = s
}
