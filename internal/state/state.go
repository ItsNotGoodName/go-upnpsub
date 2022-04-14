package state

import (
	"sync"
	"time"
)

type State struct {
	mu       sync.Mutex
	now      time.Time
	last     time.Time
	duration time.Duration
}

func New() *State {
	t := time.Now()
	return &State{
		now:  t,
		last: t,
	}
}

func (a *State) Active() bool {
	a.mu.Lock()
	b := time.Since(a.now) < a.duration
	a.mu.Unlock()
	return b
}

func (a *State) LastActive() time.Time {
	a.mu.Lock()
	t := a.last
	a.mu.Unlock()
	return t
}

func (a *State) Activate(seconds int) {
	a.mu.Lock()
	a.now = time.Now()
	a.last = a.now
	a.duration = time.Duration(seconds) * time.Second
	a.mu.Unlock()
}

func (a *State) Deactivate() {
	a.mu.Lock()
	a.now = time.Unix(0, 0)
	a.mu.Unlock()
}
