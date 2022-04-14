package upnpsub

import (
	"sync"
	"time"
)

type state struct {
	mu       sync.Mutex
	now      time.Time
	last     time.Time
	duration time.Duration
}

func newState() *state {
	t := time.Now()
	return &state{
		now:  t,
		last: t,
	}
}

func (a *state) active() bool {
	a.mu.Lock()
	b := time.Since(a.now) < a.duration
	a.mu.Unlock()
	return b
}

func (a *state) lastActive() time.Time {
	a.mu.Lock()
	t := a.last
	a.mu.Unlock()
	return t
}

func (a *state) activate(seconds int) {
	a.mu.Lock()
	a.now = time.Now()
	a.last = a.now
	a.duration = time.Duration(seconds) * time.Second
	a.mu.Unlock()
}

func (a *state) deactivate() {
	a.mu.Lock()
	a.now = time.Unix(0, 0)
	a.mu.Unlock()
}
