package offload

// NullName is the name of the null engine, the one that runs when no kernel engine does.
const NullName = "null"

// NullEngine is a no-op offload engine.
type NullEngine struct{}

// NewNullEngine creates a null offload engine.
func NewNullEngine() *NullEngine { return &NullEngine{} }

// Name returns the offload engine type.
func (o *NullEngine) Name() string { return NullName }

// Start initializes the offload engine.
func (o *NullEngine) Start(_ string, _ []string) error { return nil }

// Close stops the offload engine.
func (o *NullEngine) Close() error { return nil }

// Upsert establishes a new offloaded connection on the engine or modifies an existing one.
func (o *NullEngine) Upsert(_, _ Connection, _, _ string) error { return nil }

// Remove removes an offloaded connection.
func (o *NullEngine) Remove(_, _ Connection) error { return nil }

// Packets reports that the null engine knows no connection: it offloads nothing, so every flow
// stays visible to its own pumps and needs no kernel-side liveness signal.
func (o *NullEngine) Packets(_, _ Connection) (uint64, bool) { return 0, false }

// Flows reports no flows: the admission of New counts them.
func (o *NullEngine) Flows() (live, limit int) { return 0, -1 }

// Stats returns the last cached offload statistics.
func (o *NullEngine) Stats() (StatMap, error) { return StatMap{}, nil }
