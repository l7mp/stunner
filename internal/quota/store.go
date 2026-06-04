package quota

import (
	"sync"
)

type Store interface {
	CheckAndIncrement(username, realm string, quota int) bool
	Decrement(username, realm string)
}

// A per-user quota store implementation. Keeps per-username allocation status in a map.
type store struct {
	// allocs per realm/username
	allocs map[string](map[string]int)
	lock   sync.Mutex
}

// Create a new private quota handler
func NewStore() *store {
	return &store{
		allocs: map[string](map[string]int){},
	}
}

// CheckAndIncrement returns false if the current user has reached the allocation limit, or
// otherwise return true and incremental the user's allocation count.
func (q *store) CheckAndIncrement(username, realm string, quota int) bool {
	if quota == 0 {
		return true
	}

	q.lock.Lock()
	defer q.lock.Unlock()
	if rallocs, ok := q.allocs[realm]; ok {
		if n, ok := rallocs[username]; ok {
			if n == quota {
				return false
			}
		} else {
			q.allocs[realm] = map[string]int{}
		}
	}
	q.allocs[realm][username]++

	return true
}

// Decrement deletes a user allocation from the store.
func (q *store) Decrement(username, realm string) {
	q.lock.Lock()
	defer q.lock.Unlock()
	if rallocs, ok := q.allocs[realm]; ok {
		if n, ok := rallocs[username]; ok && n > 0 {
			q.allocs[realm][username]--
		} else {
			delete(rallocs, username)
		}
		if len(rallocs) == 0 {
			delete(q.allocs, realm)
		}
	}
}
