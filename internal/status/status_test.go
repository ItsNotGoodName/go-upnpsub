package status

import (
	"testing"
	"time"
)

func TestStatus(t *testing.T) {
	s := New()
	if s.Active() {
		t.Error("Expected status to be inactive")
	}

	s.Activate(10)
	if !s.Active() {
		t.Error("Expected status to be active")
	}

	s.Deactivate()
	if s.Active() {
		t.Error("Expected status to be inactive")
	}

	if s.LastActive() == time.Unix(0, 0) {
		t.Error("Expected LastActive to not be zero")
	}
}
