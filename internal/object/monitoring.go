package object

import (
	"github.com/pion/logging"

	"github.com/l7mp/stunner/pkg/apis/v1alpha1"
)

// Monitoring holds STUNner monitoring configuration
type Monitoring struct {
	Port  int
	Url   string
	Group string
	log   logging.LeveledLogger
}

// NewMonitoring creates a new monitoring object.
func NewMonitoring(conf v1alpha1.Config, logger logging.LoggerFactory) (Object, error) {
	req, ok := conf.(*v1alpha1.MonitoringConfig)
	if !ok {
		return nil, v1alpha1.ErrInvalidConf
	}

	monitoring := Monitoring{log: logger.NewLogger("stunner-monitoring")}
	monitoring.log.Tracef("NewMonitoring: %#v", req)

	if err := monitoring.Reconcile(req); err != nil && err != v1alpha1.ErrRestartRequired {
		return nil, err
	}

	return &monitoring, v1alpha1.ErrRestartRequired
}

// Reconcile updates the monitoring for a new configuration.
func (monitoring *Monitoring) Reconcile(conf v1alpha1.Config) error {
	req, ok := conf.(*v1alpha1.MonitoringConfig)
	if !ok {
		return v1alpha1.ErrInvalidConf
	}

	monitoring.log.Tracef("Reconcile: %#v", req)

	if err := req.Validate(); err != nil {
		return err
	}

	monitoring.Port = req.Port
	monitoring.log.Infof("using port: %d", monitoring.Port)

	monitoring.Url = req.Url
	monitoring.Group = req.Group

	return nil
}

// Name returns the name of the object
func (monitoring *Monitoring) ObjectName() string {
	// singleton!
	return v1alpha1.DefaultMonitoringName
}

// GetConfig returns the configuration of the running authenticator
func (monitoring *Monitoring) GetConfig() v1alpha1.Config {
	monitoring.log.Tracef("GetConfig")
	r := v1alpha1.MonitoringConfig{
		Port:  monitoring.Port,
		Url:   monitoring.Url,
		Group: monitoring.Group,
	}

	return &r
}

// Close closes the authenticator
func (monitoring *Monitoring) Close() error {
	monitoring.log.Tracef("Close")
	return nil
}
