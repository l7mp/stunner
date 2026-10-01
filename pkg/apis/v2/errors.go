package v2

import (
	"errors"
	"fmt"
	"strings"
)

var (
	ErrInvalidConf    = errors.New("invalid configuration")
	ErrNoSuchListener = errors.New("no such listener")
	ErrNoSuchServer   = errors.New("no such server")
	ErrNoSuchCluster  = errors.New("no such cluster")
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
