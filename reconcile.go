package stunner

import (
	"fmt"

	// "github.com/pion/logging"
	// "github.com/pion/transport/vnet"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Reconcile handles the updates to the STUNner configuration. Some updates are destructive so the
// server must be closed and restarted with the new configuration manually (see the documentation
// of the corresponding STUNner objects for when STUNner may restart after a
// reconciliation). Reconcile returns nil if is server restart was not requred,
// v1alpha1.ErrRestartRequired to indicate that it performed a full shutdown-restart cycle to
// reconcile the new config (unless DryRun is on), and an error if an error happened during
// reconciliation, in which case it will rollback the last working configuration (unless
// SuppressRollback is on)
func (s *Stunner) Reconcile(req v1alpha1.StunnerConfig) error {
	s.log.Debugf("reconciling STUNner for config: %#v ", req)

	rollback := s.GetConfig()
	restart := false

	// admin
	adminState, err := s.adminManager.PrepareReconciliation([]v1alpha1.Config{&req.Admin})
	if err != nil {
		if err == v1alpha1.ErrRestartRequired {
			restart = true
		} else {
			return fmt.Errorf("error preparing reconciliation for admin config: %s", err.Error())
		}
	}

	// auth
	authState, err := s.authManager.PrepareReconciliation([]v1alpha1.Config{&req.Auth})
	if err != nil {
		if err == v1alpha1.ErrRestartRequired {
			restart = true
		} else {
			return fmt.Errorf("error preparing reconciliation for auth config: %s", err.Error())
		}
	}

	// listener
	lconf := make([]v1alpha1.Config, len(req.Listeners))
	for i := range req.Listeners {
		lconf[i] = &(req.Listeners[i])
	}
	listenerState, err := s.listenerManager.PrepareReconciliation(lconf)
	if err != nil {
		if err == v1alpha1.ErrRestartRequired {
			restart = true
		} else {
			return fmt.Errorf("error preparing reconciliation for listener config: %s", err.Error())
		}
	}

	// cluster
	cconf := make([]v1alpha1.Config, len(req.Clusters))
	for i := range req.Clusters {
		cconf[i] = &(req.Clusters[i])
	}
	clusterState, err := s.clusterManager.PrepareReconciliation(cconf)
	if err != nil {
		if err == v1alpha1.ErrRestartRequired {
			restart = true
		} else {
			return fmt.Errorf("error preparing reconciliation for cluster config: %s", err.Error())
		}
	}

	s.log.Debugf("reconciliation preparation ready, restart required: %t", restart)

	// a restart may be needed: close the running server while it still holds the actual
	// configuration (i.e., before actually calling Reconcile on all objects)
	if restart && !s.options.DryRun {
		s.log.Debug("closing running server")
		s.Stop()
	}

	// finish reconciliation
	// admin
	new, deleted, changed := 0, 0, 0
	var errFinal error

	err = s.adminManager.FinishReconciliation(adminState)
	if err != nil {
		s.log.Errorf("could not reconcile admin config: %s", err.Error())
		errFinal = err
		goto rollback
	}

	s.log.Infof("setting loglevel to %q", s.GetAdmin().LogLevel)
	s.logger.SetLevel(s.GetAdmin().LogLevel)

	new += len(adminState.NewJobQueue)
	changed += len(adminState.ChangedJobQueue)
	deleted += len(adminState.DeletedJobQueue)

	// auth
	err = s.authManager.FinishReconciliation(authState)
	if err != nil {
		s.log.Errorf("could not reconcile auth config: %s", err.Error())
		errFinal = err
		goto rollback
	}

	new += len(authState.NewJobQueue)
	changed += len(authState.ChangedJobQueue)
	deleted += len(authState.DeletedJobQueue)

	// listener
	err = s.listenerManager.FinishReconciliation(listenerState)
	if err != nil {
		s.log.Errorf("could not reconcile listener config: %s", err.Error())
		errFinal = err
		goto rollback
	}

	if len(s.listenerManager.Keys()) == 0 {
		s.log.Warn("running with no listeners")
	}

	new += len(listenerState.NewJobQueue)
	changed += len(listenerState.ChangedJobQueue)
	deleted += len(listenerState.DeletedJobQueue)

	// cluster
	err = s.clusterManager.FinishReconciliation(clusterState)
	if err != nil {
		s.log.Errorf("could not reconcile cluster config: %s", err.Error())
		errFinal = err
		goto rollback
	}

	if len(s.clusterManager.Keys()) == 0 {
		s.log.Warn("running with no clusters: all traffic will be dropped")
	}

	new += len(clusterState.NewJobQueue)
	changed += len(clusterState.ChangedJobQueue)
	deleted += len(clusterState.DeletedJobQueue)

	s.log.Infof("reconciliation ready: new objects: %d, changed objects: %d, deleted objects: %d",
		new, changed, deleted)

	if s.options.DryRun {
		if restart {
			return v1alpha1.ErrRestartRequired
		}
		return nil
	}

	// no dry-run
	if restart {
		s.log.Debugf("restarting for conf: %#v", req)

		if err := s.Start(); err != nil {
			s.log.Errorf("could not restart: %s", err.Error())
			if s.options.SuppressRollback {
				return err
			}

			errFinal = err
			goto rollback
		}

		return v1alpha1.ErrRestartRequired
	}

	return nil

rollback:
	if !s.options.SuppressRollback {
		s.log.Infof("rolling back to previous configuration: %#v", rollback)
		return s.Reconcile(*rollback)
	}

	return errFinal
}
