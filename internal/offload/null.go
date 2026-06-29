package offload

// NullEngine is a no-op offload engine.
type NullEngine struct{}

// NewNullEngine creates a null offload engine.
func NewNullEngine() *NullEngine { return &NullEngine{} }

// Name returns the offload engine type.
func (o *NullEngine) Name() string { return "null" }

// Start initializes the offload engine.
func (o *NullEngine) Start(_ string, _ []string) error { return nil }

// Close stops the offload engine.
func (o *NullEngine) Close() error { return nil }

// Upsert establishes a new offloaded connection on the engine or modifies an existing one.
func (o *NullEngine) Upsert(_, _ Connection, _, _ string) error { return nil }

// Remove removes an offloaded connection.
func (o *NullEngine) Remove(_, _ Connection) error { return nil }

// Stats returns the last cached offload statistics.
func (o *NullEngine) Stats() (StatMap, error) { return map[StatKey]StatInfo{}, nil }
