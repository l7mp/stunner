package stunner

import (
	"github.com/l7mp/stunner/internal/reconciler"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
	cdsclient "github.com/l7mp/stunner/pkg/config/client"
)

// Reconcile handles updates to the STUNner configuration. The actual walk is delegated to the
// reconciler engine; this method only manages the readiness bit.
//
// Returns nil if nothing changed in a way that required a restart, stnrv1.ErrRestarted listing
// any objects that were bounced (safe to ignore), or a non-nil error if the config was rejected,
// in which case the previous configuration is rolled back unless SuppressRollback is set.
func (s *Stunner) Reconcile(req *stnrv1.StunnerConfig) error {
	err := s.reconciler.Reconcile(req, reconciler.Policy{
		SuppressRollback: s.suppressRollback,
		DryRun:           s.dryRun,
	})

	// Update loglevel after admin reconcile may have changed it.
	if a := s.GetAdmin(); a != nil && a.LogLevel != "" {
		s.logger.SetLevel(a.LogLevel)
	}

	// Become ready unless we are shutting down, already ready, in rollback, or bootstrapping
	// with a zero-config.
	if err == nil && !s.rt.IsShutdown() && !s.rt.IsReady() && !cdsclient.IsZeroConfig(req) {
		s.rt.SetReady(true)
	}

	return err
}
