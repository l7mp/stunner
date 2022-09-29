package object

import (
	"github.com/pion/logging"

	"github.com/l7mp/stunner/internal/monitoring"
	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

const DefaultAdminObjectName = "DefaultAdmin"

// Admin is the main object holding STUNner administration info
type Admin struct {
	Name, LogLevel, MetricsEndpoint string
	log                             logging.LeveledLogger
	MonitoringFrontend              monitoring.Frontend
}

// NewAdmin creates a new Admin object. Requires a server restart (returns
// v1alpha1.ErrRestartRequired)
func NewAdmin(conf v1alpha1.Config, mf monitoring.Frontend, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*v1alpha1.AdminConfig)
	if !ok {
		return nil, v1alpha1.ErrInvalidConf
	}

	admin := Admin{MonitoringFrontend: mf, log: logger.NewLogger("stunner-admin")}
	admin.log.Tracef("NewAdmin: %#v", req)

	if err := admin.Reconcile(req); err != nil && err != v1alpha1.ErrRestartRequired {
		return nil, err
	}

	return &admin, nil
}

// Inspect examines whether a configuration change on the object would require a restart. An empty
// new-config means it is about to be deleted, an empty old-config means it is to be deleted,
// otherwise it will be reconciled from the old configuration to the new one
func (a *Admin) Inspect(old, new v1alpha1.Config) bool {
	return false
}

// Reconcile updates the authenticator for a new configuration. Requires a valid reconciliation
// request
func (a *Admin) Reconcile(conf v1alpha1.Config) error {
	req, ok := conf.(*v1alpha1.AdminConfig)
	if !ok {
		return v1alpha1.ErrInvalidConf
	}

	if err := req.Validate(); err != nil {
		return err
	}

	a.log.Tracef("Reconcile: %#v", req)

	a.Name = req.Name
	a.LogLevel = req.LogLevel
	a.MetricsEndpoint = req.MetricsEndpoint

	// monitoring
	if err := a.MonitoringFrontend.Reconcile(a.MetricsEndpoint); err != nil {
		return err
	}

	return nil
}

// Name returns the name of the object
func (a *Admin) ObjectName() string {
	// singleton!
	return v1alpha1.DefaultAdminName
}

// GetConfig returns the configuration of the running object
func (a *Admin) GetConfig() v1alpha1.Config {
	a.log.Tracef("GetConfig")
	return &v1alpha1.AdminConfig{
		Name:            a.Name,
		LogLevel:        a.LogLevel,
		MetricsEndpoint: a.MetricsEndpoint,
	}
}

// Close closes the Admin object
func (a *Admin) Close() error {
	a.log.Tracef("Close")
	return nil
}

// AdminFactory can create now Admin objects
type AdminFactory struct {
	monitoringFrontend monitoring.Frontend
	logger             logging.LoggerFactory
}

// NewAdminFactory creates a new factory for Admin objects
func NewAdminFactory(mf monitoring.Frontend, logger logging.LoggerFactory) Factory {
	return &AdminFactory{monitoringFrontend: mf, logger: logger}
}

// New can produce a new Admin object from the given configuration. A nil config will create an
// empty admin object (useful for creating throwaway objects for, e.g., calling Inpect)
func (f *AdminFactory) New(conf v1alpha1.Config) (Object, error) {
	if conf == nil {
		return &Admin{}, nil
	}

	return NewAdmin(conf, f.monitoringFrontend, f.logger)
}
