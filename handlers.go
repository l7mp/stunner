package stunner

import (
	"errors"

	"github.com/l7mp/stunner/internal/object"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// NewReadinessHandler creates a helper function for checking the readiness of STUNner.
func (s *Stunner) NewReadinessHandler() object.ReadinessHandler {
	return func() error {
		if s.forceReady || s.IsReady() {
			return nil
		} else {
			return errors.New("stunnerd not ready")
		}
	}
}

// NewStatusHandler creates a helper function for printing the status of STUNner.
func (s *Stunner) NewStatusHandler() object.StatusHandler {
	return func() stnrv1.Status { return s.Status() }
}
