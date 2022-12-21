package stunner

import (
	"fmt"

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

	if err := req.Validate(); err != nil {
		return err
	}

	s.log.Debugf("reconciling STUNner for config: %s ", req.String())

	rollback := s.GetConfig()
	toBeStarted, toBeRestarted := []object.Object{}, []object.Object{}

	// admin
	adminState, err := s.adminManager.PrepareReconciliation([]v1alpha1.Config{&req.Admin}, &req)
	if err != nil {
		return fmt.Errorf("error preparing reconciliation for admin config: %s",
			err.Error())
	}

	// auth
	authState, err := s.authManager.PrepareReconciliation([]v1alpha1.Config{&req.Auth}, &req)
	if err != nil {
		return fmt.Errorf("error preparing reconciliation for auth config: %s",
			err.Error())
	}

	// listener
	lconf := make([]v1alpha1.Config, len(req.Listeners))
	for i := range req.Listeners {
		lconf[i] = &(req.Listeners[i])
	}
	listenerState, err := s.listenerManager.PrepareReconciliation(lconf, &req)
	if err != nil {
		return fmt.Errorf("error preparing reconciliation for listener config: %s", err.Error())
	}

	// cluster
	cconf := make([]v1alpha1.Config, len(req.Clusters))
	for i := range req.Clusters {
		cconf[i] = &(req.Clusters[i])
	}
	clusterState, err := s.clusterManager.PrepareReconciliation(cconf, &req)
	if err != nil {
		return fmt.Errorf("error preparing reconciliation for cluster config: %s", err.Error())
	}

	s.log.Tracef("reconciliation preparation ready")

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

	toBeRestarted = append(toBeRestarted, adminState.ToBeRestarted...)
	toBeStarted = append(toBeStarted, adminState.ToBeStarted...)
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

	toBeRestarted = append(toBeRestarted, authState.ToBeRestarted...)
	toBeStarted = append(toBeStarted, authState.ToBeStarted...)
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

	toBeRestarted = append(toBeRestarted, listenerState.ToBeRestarted...)
	toBeStarted = append(toBeStarted, listenerState.ToBeStarted...)
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

	toBeRestarted = append(toBeRestarted, clusterState.ToBeRestarted...)
	toBeStarted = append(toBeStarted, clusterState.ToBeStarted...)
	new += len(clusterState.NewJobQueue)
	changed += len(clusterState.ChangedJobQueue)
	deleted += len(clusterState.DeletedJobQueue)

	// find all objects (listeners) to be restarted and restart
	if !s.dryRun {
		if err := s.restart(toBeStarted, toBeRestarted); err != nil {
			s.log.Errorf("could (re)starting object: %s", err.Error())
			errFinal = err
			goto rollback
		}
	}

	// we are "ready" unless we are being shut down
	if !s.shutdown && !s.ready {
		s.ready = true
	}

	s.log.Infof("reconciliation ready: new objects: %d, changed objects: %d, "+
		"deleted objects: %d, started objects: %d, restarted objects: %d",
		new, changed, deleted, len(toBeStarted), len(toBeRestarted))

	s.log.Info(s.Status())

	if len(toBeRestarted) > 0 {
		names := make([]string, len(toBeRestarted))
		for i, n := range toBeRestarted {
			names[i] = fmt.Sprintf("%s: %s", n.ObjectType(), n.ObjectName())
		}

		return v1alpha1.ErrRestarted{Objects: names}
	}

	return nil

rollback:
	if !s.suppressRollback {
		s.log.Infof("rolling back to previous configuration: %s", rollback.String())
		return s.Reconcile(*rollback)
	}

	return errFinal
}

func (s *Stunner) restart(started, restarted []object.Object) error {
	for _, o := range started {
		switch o.(type) {
		case *object.Listener:
			l, _ := o.(*object.Listener)

			if err := s.StartServer(l); err != nil {
				return err
			}
		default:
			s.log.Errorf("internal error: start() is not implemented for object %q",
				o.ObjectName())
		}
	}

	for _, o := range restarted {
		switch o.(type) {
		case *object.Listener:
			l, _ := o.(*object.Listener)

			if err := l.Close(); err != nil {
				return err
			}

			if err := s.StartServer(l); err != nil {
				return err
			}
		default:
			s.log.Errorf("internal error: restart is not implemented for object %q",
				o.ObjectName())
		}
	}

	return nil
}
