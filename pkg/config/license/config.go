// Package to handle the licensing status of a client.
package license

import (
	"fmt"

	"github.com/pion/logging"

	stnrv1 "github.com/l7mp/stunner/pkg/apis/v1"
)

var constructor = NewStub
var _ ConfigManager = &baseManager{}

// Feature defines the supported features.
type Feature interface {
	fmt.Stringer
}

type baseFeature struct{} //nolint:unused

func (f baseFeature) String() string { return "N/A" } //nolint:unused

// SubscriptionType is the current subscription type.
type SubscriptionType interface {
	fmt.Stringer
}

type nilSubscriptionType struct{}

func (f nilSubscriptionType) String() string { return "free" }

func NewNilSubscriptionType() *nilSubscriptionType { return &nilSubscriptionType{} }

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
	// Status returns the current licensing status.
	Status() stnrv1.LicenseStatus
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
func (m *baseManager) Validate(_ Feature) bool                { return false }
func (m *baseManager) SubscriptionType() SubscriptionType     { return NewNilSubscriptionType() }
func (m *baseManager) Status() stnrv1.LicenseStatus           { return stnrv1.NewEmptyLicenseStatus() }
