// Package to handle the licensing status of a client.
package license

import (
	"fmt"

	"github.com/pion/logging"

	stnrv2 "github.com/l7mp/stunner/v2/pkg/apis/v2"
)

var constructor = NewStub
var _ ConfigManager = &BaseManager{}

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
	GetConfig() *stnrv2.LicenseConfig
	// Reconcile updates the the licensing status, i.e., the licensed feature-set and the
	// subscription type, based in an ecrpyted key/passphrase pair.
	Reconcile(config *stnrv2.LicenseConfig)
	// Validate checks whether a client is entitled to use a feature.
	Validate(feature Feature) bool
	// SubscriptionType returns the current subscription type (e.g., free, member, enterprise).
	SubscriptionType() SubscriptionType
	// Status returns the current licensing status.
	Status() string
}

// New creares a new license config manager.
func New(log logging.LeveledLogger) ConfigManager {
	return constructor(log)
}

// BaseManager implements the basic functionality every license manager embeds.
type BaseManager struct {
	config *stnrv2.LicenseConfig
	log    logging.LeveledLogger
}

func newBaseManager(log logging.LeveledLogger) BaseManager {
	m := BaseManager{log: log}
	return m
}

func (m *BaseManager) GetConfig() *stnrv2.LicenseConfig       { return m.config }
func (m *BaseManager) Reconcile(config *stnrv2.LicenseConfig) { m.config = config }
func (m *BaseManager) Validate(_ Feature) bool                { return false }
func (m *BaseManager) SubscriptionType() SubscriptionType     { return NewNilSubscriptionType() }
func (m *BaseManager) Status() string                         { return "{tier:free}" }
