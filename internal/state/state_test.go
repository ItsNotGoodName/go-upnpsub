package state

import (
	"testing"
	"time"
)

func TestState(t *testing.T) {
	s := New()
	if s.Active() {
		t.Error("Expected state to be inactive")
	}

	s.Activate(10)
	if !s.Active() {
		t.Error("Expected state to be active")
	}

	s.Deactivate()
	if s.Active() {
		t.Error("Expected state to be inactive")
	}

	if s.LastActive() == time.Unix(0, 0) {
		t.Error("Expected LastActive to not be zero")
	}
}
