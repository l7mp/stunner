// Package server holds what the TURN and L4 servers (the subpackages) share: the accept queue, the
// conntrack and ErrNotServing.
package server

import "errors"

// ErrNotServing answers a conn handed to a server that is stopped or restarting.
var ErrNotServing = errors.New("server is not serving")
