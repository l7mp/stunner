package object

import (
	"github.com/pion/logging"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

const DefaultAdminObjectName = "DefaultAdmin"

// Admin is the main object holding STUNner administration info
type Admin struct {
        Name, LogLevel string
        log logging.LeveledLogger
}

// NewAdmin creates a new Admin object. Requires a server restart (returns v1alpha1.ErrRestartRequired)
func NewAdmin(conf v1alpha1.Config, logger logging.LoggerFactory) (Object, error) {
        req, ok := conf.(*v1alpha1.AdminConfig)
        if !ok {
                return nil, v1alpha1.ErrInvalidConf
        }
        
 	admin := Admin{ log: logger.NewLogger("stunner-admin") }
	admin.log.Tracef("NewAdmin: %#v", req)

        if err := admin.Reconcile(req); err != nil && err != v1alpha1.ErrRestartRequired {
                return nil, err
        }

        return &admin, v1alpha1.ErrRestartRequired
}

// Reconcile updates the authenticator for a new configuration. Does require a server restart
func (a *Admin) Reconcile(conf v1alpha1.Config) error {
        req, ok := conf.(*v1alpha1.AdminConfig)
        if !ok {
                return v1alpha1.ErrInvalidConf
        }
        
	a.log.Tracef("Reconcile: %#v", req)
        
        if err := req.Validate(); err != nil {
                return err
        }
        
        a.Name     = req.Name
        a.LogLevel = req.LogLevel

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
		Name:     a.Name,
		LogLevel: a.LogLevel,
	}
}

// Close closes the Admin object
func (a *Admin) Close() error {
	a.log.Tracef("Close")
        return nil
}
