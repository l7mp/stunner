package v1

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidConf    = errors.New("Invalid configuration")
	ErrNoSuchListener = errors.New("No such listener")
	ErrNoSuchCluster  = errors.New("No such cluster")
	// ErrInvalidRoute   = errors.New("Invalid route")
)

type ErrRestarted struct {
	Objects []string
}

func (e ErrRestarted) Error() string {
	s := []string{}
	for _, o := range e.Objects {
		s = append(s, fmt.Sprintf("[%s]", o))
	}
	return fmt.Sprintf("restarted: %s", strings.Join(s, ", "))
}
