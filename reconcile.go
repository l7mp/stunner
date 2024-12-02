package stunner

import (
	"fmt"

	"github.com/l7mp/stunner/internal/object"
	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

// Reconcile handles updates to the STUNner configuration. Some updates are destructive: in this
// case the returned error contains the names of the objects (usually, listeners) that were
// restarted during reconciliation (see the documentation of the corresponding STUNner objects for
// when STUNner may restart after a reconciliation). Reconcile returns nil no objects were
// restarted, v1.ErrRestarted to indicate that a shutdown-restart cycle was performed for at
// least one internal object (usually, a listener) for the new config (unless DryRun is enabled),
// and an error if an error has occurred during reconciliation, in which case it will rollback the
// last working configuration (unless SuppressRollback is on).
func (s *Stunner) Reconcile(req *stnrv1.StunnerConfig) error {
	return s.reconcileWithRollback(req, false)
}

func (s *Stunner) reconcileWithRollback(req *stnrv1.StunnerConfig, inRollback bool) error {
	var errFinal error
	new, deleted, changed := 0, 0, 0

	if err := req.Validate(); err != nil {
		return err
	}

	s.log.Debugf("Reconciling STUNner for config: %s ", req.String())

	rollback := s.GetConfig()
	toBeStarted, toBeRestarted := []object.Object{}, []object.Object{}

	// admin
	adminState, err := s.adminManager.PrepareReconciliation([]stnrv1.Config{&req.Admin}, req)
	if err != nil {
		return fmt.Errorf("error preparing reconciliation for admin config: %s",
			err.Error())
	}
	toBeRestarted = append(toBeRestarted, adminState.ToBeRestarted...)
	new += len(adminState.NewJobQueue)
	changed += len(adminState.ChangedJobQueue)
	deleted += len(adminState.DeletedJobQueue)

	// auth
	authState, err := s.authManager.PrepareReconciliation([]stnrv1.Config{&req.Auth}, req)
	if err != nil {
		return fmt.Errorf("error preparing reconciliation for auth config: %s",
			err.Error())
	}
	toBeRestarted = append(toBeRestarted, authState.ToBeRestarted...)
	new += len(authState.NewJobQueue)
	changed += len(authState.ChangedJobQueue)
	deleted += len(authState.DeletedJobQueue)

	// listener
	lconf := make([]stnrv1.Config, len(req.Listeners))
	for i := range req.Listeners {
		lconf[i] = &(req.Listeners[i])
	}
	listenerState, err := s.listenerManager.PrepareReconciliation(lconf, req)
	if err != nil {
		return fmt.Errorf("error preparing reconciliation for listener config: %s", err.Error())
	}
	toBeRestarted = append(toBeRestarted, listenerState.ToBeRestarted...)
	new += len(listenerState.NewJobQueue)
	changed += len(listenerState.ChangedJobQueue)
	deleted += len(listenerState.DeletedJobQueue)

	// cluster
	cconf := make([]stnrv1.Config, len(req.Clusters))
	for i := range req.Clusters {
		cconf[i] = &(req.Clusters[i])
	}
	clusterState, err := s.clusterManager.PrepareReconciliation(cconf, req)
	if err != nil {
		return fmt.Errorf("error preparing reconciliation for cluster config: %s", err.Error())
	}
	toBeRestarted = append(toBeRestarted, clusterState.ToBeRestarted...)
	new += len(clusterState.NewJobQueue)
	changed += len(clusterState.ChangedJobQueue)
	deleted += len(clusterState.DeletedJobQueue)

	// find all objects (listeners) to be restarted and stop each
	if !s.dryRun {
		if err := s.stop(toBeRestarted); err != nil {
			s.log.Errorf("Could not stop object: %s", err.Error())
			errFinal = err
			if !inRollback {
				goto rollback
			}
			// failing to stop the server is not critical: suppress error and go on
		}
	}

	s.log.Tracef("Reconciliation preparation ready")

	// finish reconciliation
	// admin
	err = s.adminManager.FinishReconciliation(adminState)
	if err != nil {
		s.log.Errorf("Could not reconcile admin config: %s", err.Error())
		errFinal = err
		if !inRollback {
			goto rollback
		}
		return errFinal
	}
	toBeStarted = append(toBeStarted, adminState.ToBeStarted...)

	s.log.Infof("Setting loglevel to %q", s.GetAdmin().LogLevel)
	s.logger.SetLevel(s.GetAdmin().LogLevel)

	// auth
	err = s.authManager.FinishReconciliation(authState)
	if err != nil {
		s.log.Errorf("Could not reconcile auth config: %s", err.Error())
		errFinal = err
		if !inRollback {
			goto rollback
		}
		return errFinal
	}
	toBeStarted = append(toBeStarted, authState.ToBeStarted...)

	// listener
	err = s.listenerManager.FinishReconciliation(listenerState)
	if err != nil {
		s.log.Errorf("Could not reconcile listener config: %s", err.Error())
		errFinal = err
		if !inRollback {
			goto rollback
		}
		return errFinal
	}
	toBeStarted = append(toBeStarted, listenerState.ToBeStarted...)

	if len(s.listenerManager.Keys()) == 0 {
		s.log.Warn("Running with no listeners")
	}

	// cluster
	err = s.clusterManager.FinishReconciliation(clusterState)
	if err != nil {
		s.log.Errorf("Could not reconcile cluster config: %s", err.Error())
		errFinal = err
		if !inRollback {
			goto rollback
		}
		return errFinal
	}
	toBeStarted = append(toBeStarted, clusterState.ToBeStarted...)

	if len(s.clusterManager.Keys()) == 0 {
		s.log.Warn("Running with no clusters: all traffic will be dropped")
	}

	// find all objects (listeners) to be started or restarted and start each
	if !s.dryRun {
		if err := s.start(toBeStarted, toBeRestarted); err != nil {
			s.log.Errorf("Could not start object: %s", err.Error())
			errFinal = err
			if !inRollback {
				goto rollback
			}
			return errFinal
		}
	}

	// we are "ready" unless we are being shut down
	if !s.shutdown && !s.ready {
		s.ready = true
	}

	s.log.Infof("Reconciliation ready: new objects: %d, changed objects: %d, "+
		"deleted objects: %d, started objects: %d, restarted objects: %d",
		new, changed, deleted, len(toBeStarted), len(toBeRestarted))

	s.log.Infof("New dataplane status: %s", s.Status().String())

	if len(toBeRestarted) > 0 {
		names := make([]string, len(toBeRestarted))
		for i, n := range toBeRestarted {
			names[i] = fmt.Sprintf("%s: %s", n.ObjectType(), n.ObjectName())
		}

		return stnrv1.ErrRestarted{Objects: names}
	}

	return nil

rollback:
	if !s.suppressRollback {
		s.log.Infof("Rolling back to previous configuration: %s", rollback.String())
		return s.reconcileWithRollback(rollback, true)
	}

	return errFinal
}

func (s *Stunner) stop(restarted []object.Object) error {
	for _, o := range restarted {
		switch l := o.(type) {
		case *object.Listener:
			if err := l.Close(); err != nil {
				return err
			}
		default:
			s.log.Errorf("Internal error: stop() is not implemented for object %q",
				o.ObjectName())
		}
	}

	return nil
}

func (s *Stunner) start(started, restarted []object.Object) error {
	for _, o := range append(started, restarted...) {
		switch l := o.(type) {
		case *object.Listener:
			if err := s.StartServer(l); err != nil {
				return err
			}
		default:
			s.log.Errorf("Internal error: start() is not implemented for object %q",
				o.ObjectName())
		}
	}

	return nil
}
