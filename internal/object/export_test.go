package object

import "github.com/l7mp/stunner/v2/internal/api"

// SetSink replaces where a listener emits its conns, for the external tests.
func (l *Listener) SetSink(f func(api.Conn)) { l.sink = f }

// NewTLSConfig is the TLS configuration seam, for the external tests.
var NewTLSConfig = &newTLSConfig
