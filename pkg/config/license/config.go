// Package to handle the licensing status of a client.
package license

import (
	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var constructor = NewStub
var _ ConfigManager = &baseManager{}

// Feature is an enum of supported features.
type Feature int

// SubscriptionType is an enum of known subscription types.
type SubscriptionType int

// Manager is a genetic API for negotiating licensing status.
type ConfigManager interface {
	// GetConfig returns the current config, i.e., the ecrpyted key/passphrase pair.
	GetConfig() *stnrv1.LicenseConfig
	// Reconcile updates the the licensing status, i.e., the licensed feature-set and the
	// subscription type, based in an ecrpyted key/passphrase pair.
	Reconcile(config *stnrv1.LicenseConfig)
	// Validate checks whether a client is entitled to use a feature.
	Validate(feature Feature) bool
	// SubscriptionType returns the current subscription type (e.g., free, member, enterprise).
	SubscriptionType() SubscriptionType
	// Status generates a status string.
	Status() string
}

// New creares a new license config manager.
func New(log logging.LeveledLogger) ConfigManager {
	return constructor(log)
}

// baseManager implements the basic functionality so that all license manager implementations can embed it
type baseManager struct {
	config *stnrv1.LicenseConfig
	log    logging.LeveledLogger
}

func newBaseManager(log logging.LeveledLogger) baseManager {
	m := baseManager{log: log}
	return m
}

func (m *baseManager) GetConfig() *stnrv1.LicenseConfig       { return m.config }
func (m *baseManager) Reconcile(config *stnrv1.LicenseConfig) { m.config = config }
func (m *baseManager) Validate(feature Feature) bool          { return false }
func (m *baseManager) SubscriptionType() SubscriptionType     { return SubscriptionType(0) }
func (m *baseManager) Status() string                         { return "<N/A>" }
