package object

import "errors"

var (
	// ErrRestartRequired indicates that an object needs to be restarted for reconciliation.
	ErrRestartRequired = errors.New("restart required")
)
