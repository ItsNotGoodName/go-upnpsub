package status

import (
	"sync"
	"time"
)

type Status struct {
	mu       sync.Mutex
	now      time.Time
	last     time.Time
	duration time.Duration
}

func New() *Status {
	t := time.Now()
	return &Status{
		now:  t,
		last: t,
	}
}

func (s *Status) Active() bool {
	s.mu.Lock()
	b := time.Since(s.now) < s.duration
	s.mu.Unlock()
	return b
}

func (s *Status) LastActive() time.Time {
	s.mu.Lock()
	t := s.last
	s.mu.Unlock()
	return t
}

func (s *Status) Activate(seconds int) {
	s.mu.Lock()
	s.now = time.Now()
	s.last = s.now
	s.duration = time.Duration(seconds) * time.Second
	s.mu.Unlock()
}

func (s *Status) Deactivate() {
	s.mu.Lock()
	s.now = time.Unix(0, 0)
	s.mu.Unlock()
}
