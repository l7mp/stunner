package v1alpha1

import "errors"

var (
	ErrRestartRequired = errors.New("Server restart required")
	ErrInvalidConf     = errors.New("Invalid configuration")
)
