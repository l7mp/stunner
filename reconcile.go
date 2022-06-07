package stunner

import (
	"fmt"
        
	// "github.com/pion/logging"
	// "github.com/pion/transport/vnet"

	"github.com/l7mp/stunner/internal/object"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Reconcile handles the updates to the STUNner configuration. Some updates are destructive so the server must be closed and restarted with the new configuration manually (see the documentation of the corresponding STUNner objects for when STUNner may restart after a reconciliation). Reconcile returns nil if no action is required by the caller, v1alpha1.ErrRestartRequired to indicate that the caller must issue a Close/Start cycle to install the reconciled configuration, and a general error if the reconciliation has failed
func (s *Stunner) Reconcile(req *v1alpha1.StunnerConfig) error {
	s.log.Debugf("reconciling STUNner for config: %#v ", req)

        // validate config
        if err := req.Validate(); err != nil {
                return err
        }

        restart := false

        // admin
        newAdmin, err := s.adminManager.Reconcile([]v1alpha1.Config{&req.Admin})
        if err != nil {
                if err == v1alpha1.ErrRestartRequired {
                        restart = true
                } else {
                        return fmt.Errorf("could not reconcile admin config: %s", err.Error())
                }
        }

        for _, c := range newAdmin {
                o, err := object.NewAdmin(c, s.logger)
                if err != nil && err != v1alpha1.ErrRestartRequired {
                        return err
                }
                s.adminManager.Upsert(o)
        }
        s.logger = NewLoggerFactory(s.GetAdmin().LogLevel)
        s.log    = s.logger.NewLogger("stunner")

        // auth
        newAuth, err := s.authManager.Reconcile([]v1alpha1.Config{&req.Auth})
        if err != nil {
                if err == v1alpha1.ErrRestartRequired {
                        restart = true
                } else {
                        return fmt.Errorf("could not reconcile auth config: %s", err.Error())
                }
        }

        for _, c := range newAuth {
                o, err := object.NewAuth(c, s.logger)
                if err != nil && err != v1alpha1.ErrRestartRequired {
                        return err
                }
                s.authManager.Upsert(o)
        }

        // listener
        lconf := make([]v1alpha1.Config, len(req.Listeners))
        for i, _ := range req.Listeners {
                lconf[i] = &(req.Listeners[i])
        }        
        newListener, err := s.listenerManager.Reconcile(lconf)
        if err != nil {
                if err == v1alpha1.ErrRestartRequired {
                        restart = true
                } else {
                        return fmt.Errorf("could not reconcile listener config: %s", err.Error())
                }
        }

        for _, c := range newListener {
                o, err := object.NewListener(c, s.net, s.logger)
                if err != nil && err != v1alpha1.ErrRestartRequired {
                        return err
                }
                s.listenerManager.Upsert(o)
                // new listeners require a restart
                restart = true
        }

        if len(s.listenerManager.Keys()) == 0 {
                s.log.Warn("running with no listeners")
        }

        // cluster
        cconf := make([]v1alpha1.Config, len(req.Clusters))
        for i, _ := range req.Clusters {
                cconf[i] = &(req.Clusters[i])
        }
        newCluster, err := s.clusterManager.Reconcile(cconf)
        if err != nil {
                if err == v1alpha1.ErrRestartRequired {
                        restart = true
                } else {
                        return fmt.Errorf("could not reconcile cluster config: %s", err.Error())
                }
        }

        for _, c := range newCluster {
                o, err := object.NewCluster(c, s.resolver, s.logger)
                if err != nil && err != v1alpha1.ErrRestartRequired {
                        return err
                }
                s.clusterManager.Upsert(o)
        }

        if len(s.clusterManager.Keys()) == 0 {
                s.log.Warn("running with no clusters: all traffic will be dropped")
        }

        if restart {
                return v1alpha1.ErrRestartRequired
        }

        return nil
}

