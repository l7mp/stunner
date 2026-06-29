package quota

import "github.com/l7mp/stunner/internal/runtime"

// The the stub below satisfies the runtime.QuotaHandler interface structurally. New takes the
// Runtime so a licensed implementation can reach the license manager (rt.License) and re-check
// entitlement on every call. The open-source stub ignores it and enforces no quota.

var constructor = func(*runtime.Runtime) runtime.QuotaHandler { return &stub{} }

// New returns a quota store.
func New(rt *runtime.Runtime) runtime.QuotaHandler { return constructor(rt) }

// stub is a no-op quota store: it admits every allocation regardless of quota or license.
type stub struct{}

// CheckAndIncrement always admits the allocation.
func (stub) CheckAndIncrement(_, _ string, _ int) bool { return true }

// Decrement is a no-op.
func (stub) Decrement(_, _ string) {}
