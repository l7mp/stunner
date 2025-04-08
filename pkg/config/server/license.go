package server

import (
	"sync"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// UpdateLicenseStatus updates the licensing status that is served by the server.
func (s *Server) UpdateLicenseStatus(status stnrv1.LicenseStatus) {
	s.log.V(4).Info("Processing license status update", "status", status.String())
	s.licenseStore.Upsert(status)
}

type LicenseStore struct {
	status stnrv1.LicenseStatus
	lock   sync.RWMutex
}

func NewLicenseStore() *LicenseStore {
	return &LicenseStore{status: stnrv1.NewEmptyLicenseStatus()}
}

func (t *LicenseStore) Get() stnrv1.LicenseStatus {
	t.lock.RLock()
	defer t.lock.RUnlock()
	return t.status
}

func (t *LicenseStore) Upsert(s stnrv1.LicenseStatus) {
	t.lock.Lock()
	defer t.lock.Unlock()
	t.status = s
}
