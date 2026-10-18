package quota

import (
	"sync"

	"github.com/l7mp/stunner/v2/internal/runtime"
)

var _ runtime.QuotaHandler = &Quota{}

// Quota counts the live sessions of every username and refuses a new one over a non-zero quota.
// The realm is ignored: usernames are counted across realms.
type Quota struct {
	lock     sync.Mutex
	sessions map[string]int
}

// New returns an empty quota store.
func New() *Quota { return &Quota{sessions: map[string]int{}} }

// CheckAndIncrement admits a session of a username unless the username already holds quota
// sessions; a zero quota admits every session. An admitted session is counted.
func (q *Quota) CheckAndIncrement(username, _ string, quota int) bool {
	q.lock.Lock()
	defer q.lock.Unlock()
	if quota > 0 && q.sessions[username] >= quota {
		return false
	}
	q.sessions[username]++
	return true
}

// Decrement releases a session of a username.
func (q *Quota) Decrement(username, _ string) {
	q.lock.Lock()
	defer q.lock.Unlock()
	n, ok := q.sessions[username]
	if !ok {
		return
	}
	if n <= 1 {
		delete(q.sessions, username)
		return
	}
	q.sessions[username] = n - 1
}
