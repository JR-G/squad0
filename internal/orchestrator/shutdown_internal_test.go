package orchestrator

import (
	"testing"
	"time"
)

func TestDrainSessions_NoInFlight_ReturnsImmediately(t *testing.T) {
	t.Parallel()

	orch := &Orchestrator{sessions: NewSessionTracker()}

	start := time.Now()
	orch.drainSessionsFor(50 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed > 30*time.Millisecond {
		t.Fatalf("expected immediate return, took %s", elapsed)
	}
}

func TestDrainSessions_StuckGoroutine_HitsGrace(t *testing.T) {
	t.Parallel()

	orch := &Orchestrator{sessions: NewSessionTracker()}
	release := make(chan struct{})

	orch.sessions.Add(1)
	go func() {
		defer orch.sessions.Done()
		<-release
	}()
	t.Cleanup(func() { close(release) })

	start := time.Now()
	orch.drainSessionsFor(50 * time.Millisecond)
	elapsed := time.Since(start)

	if elapsed < 50*time.Millisecond {
		t.Fatalf("expected to wait grace period, only waited %s", elapsed)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("waited too long after grace: %s", elapsed)
	}
}
