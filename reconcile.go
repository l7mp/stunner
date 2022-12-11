package stunner

import (
	"fmt"
	"strings"

	// "github.com/pion/logging"
	// "github.com/pion/transport/vnet"

	"github.com/l7mp/stunner/internal/object"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Reconcile handles updates to the STUNner configuration. Some updates are destructive: in this
// case the returned error contains the names of the objects (usually, listeners) that were
// restarted during reconciliation (see the documentation of the corresponding STUNner objects for
// when STUNner may restart after a reconciliation). Reconcile returns nil no objects were
// restarted, v1alpha1.ErrRestarted to indicate that a shutdown-restart cycle was performed for at
// least one internal object (usually, a listener) for the new config (unless DryRun is enabled),
// and an error if an error has occurred during reconciliation, in which case it will rollback the
// last working configuration (unless SuppressRollback is on).
func (s *Stunner) Reconcile(req v1alpha1.StunnerConfig) error {
	s.log.Debugf("reconciling STUNner for config: %s ", req.String())

	rollback := s.GetConfig()
	restarted := []string{}

	// admin
	adminState, err := s.adminManager.PrepareReconciliation([]v1alpha1.Config{&req.Admin})
	if err != nil {
		if err == object.ErrRestartRequired {
			// singleton
			if len(adminState.Restarted) != 1 {
				return fmt.Errorf("internal error: cannot find restarted object for %s",
					req.Admin.String())
			}
			restarted = append(restarted, adminState.Restarted[0].ObjectName())
		} else {
			return fmt.Errorf("error preparing reconciliation for admin config: %s",
				err.Error())
		}
	}

	// auth
	authState, err := s.authManager.PrepareReconciliation([]v1alpha1.Config{&req.Auth})
	if err != nil {
		if err == object.ErrRestartRequired {
			// singleton
			if len(authState.Restarted) != 1 {
				return fmt.Errorf("internal error: cannot find restarte object for %s",
					req.Auth.String())
			}
			restarted = append(restarted, authState.Restarted[0].ObjectName())
		} else {
			return fmt.Errorf("error preparing reconciliation for auth config: %s",
				err.Error())
		}
	}

	// listener
	lconf := make([]v1alpha1.Config, len(req.Listeners))
	for i := range req.Listeners {
		lconf[i] = &(req.Listeners[i])
	}
	listenerState, err := s.listenerManager.PrepareReconciliation(lconf)
	if err != nil {
		if err == object.ErrRestartRequired {
			for _, o := range listenerState.Restarted {
				restarted = append(restarted, fmt.Sprintf("listener: %s",
					o.ObjectName()))
			}
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
		if err == object.ErrRestartRequired {
			for _, o := range clusterState.Restarted {
				restarted = append(restarted, fmt.Sprintf("cluster: %s",
					o.ObjectName()))
			}
		} else {
			return fmt.Errorf("error preparing reconciliation for cluster config: %s", err.Error())
		}
	}

	s.log.Tracef("reconciliation preparation ready, restart required: %s",
		restartStatus(restarted))

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

	// we are "ready" unless we are being shut down
	if !s.shutdown && !s.ready {
		s.ready = true
	}

	s.log.Infof("reconciliation ready: new objects: %d, changed objects: %d, "+
		"deleted objects: %d, restarted objcts: %d",
		new, changed, deleted, len(restarted))

	s.log.Info(s.Status())

	if len(restarted) > 0 {
		return v1alpha1.ErrRestarted{Objects: restarted}
	}

	return nil

rollback:
	if !s.suppressRollback {
		s.log.Infof("rolling back to previous configuration: %s", rollback.String())
		return s.Reconcile(*rollback)
	}

	return errFinal
}

func restartStatus(rs []string) string {
	restart := "NONE"
	if len(rs) > 0 {
		s := []string{}
		for _, o := range rs {
			s = append(s, fmt.Sprintf("[%s]", o))
		}
		restart = strings.Join(s, ", ")
	}
	return restart
}
