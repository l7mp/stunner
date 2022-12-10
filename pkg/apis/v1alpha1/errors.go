package v1alpha1

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrRestarted   = errors.New("restarted")
	ErrInvalidConf = errors.New("invalid configuration")
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
