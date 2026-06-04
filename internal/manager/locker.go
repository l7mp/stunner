package manager

import "sync"

type RWLocker interface {
	RLock()
	RUnlock()
	Lock()
	Unlock()
}

type RWMutex struct {
	mu *sync.RWMutex
}

func NewRWMutex() *RWMutex {
	return &RWMutex{mu: &sync.RWMutex{}}
}

func (w *RWMutex) RLock()   { w.mu.RLock() }
func (w *RWMutex) RUnlock() { w.mu.RUnlock() }
func (w *RWMutex) Lock()    { w.mu.Lock() }
func (w *RWMutex) Unlock()  { w.mu.Unlock() }
