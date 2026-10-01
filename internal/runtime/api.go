package runtime

// This file declares the interfaces of the runtime services that the Runtime stores and hands
// out. The concrete implementations live in their own packages (internal/quota, ...) and satisfy
// these interfaces structurally. Keeping the interface here, rather than importing the
// implementation package into runtime, lets those packages import runtime (and so reach every
// other runtime service, e.g. the license manager) without an import cycle.

// QuotaHandler tracks per-user session quotas. CheckAndIncrement reports whether a new session is
// admissible for the (username, realm) pair given the quota and accounts for it; Decrement
// releases one previously admitted session. Implemented by internal/quota.
type QuotaHandler interface {
	CheckAndIncrement(username, realm string, quota int) bool
	Decrement(username, realm string)
}
