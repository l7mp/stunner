package v1

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidConf    = errors.New("invalid configuration")
	ErrNoSuchListener = errors.New("no such listener")
	ErrNoSuchCluster  = errors.New("no such cluster")
	// ErrInvalidRoute   = errors.New("invalid route")
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
