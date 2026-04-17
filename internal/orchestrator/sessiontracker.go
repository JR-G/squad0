package orchestrator

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// SessionTracker tracks in-flight per-agent sessions: their cancel
// functions (so pause / stop can interrupt them) and a shared wait
// group (so shutdown can drain them).
//
// Extracted from the Orchestrator god-object as the first slice of
// the architecture roadmap. Self-contained, no external dependencies
// beyond agent.Role; safe to share by pointer across goroutines.
type SessionTracker struct {
	mu      sync.Mutex
	cancels map[agent.Role]context.CancelFunc
	wg      sync.WaitGroup
}

// NewSessionTracker returns an empty tracker ready for use.
func NewSessionTracker() *SessionTracker {
	return &SessionTracker{
		cancels: make(map[agent.Role]context.CancelFunc),
	}
}

// Register stores the cancel for the role's current session,
// replacing any prior entry without invoking it (the caller is
// expected to have stopped the prior session before re-registering).
func (tracker *SessionTracker) Register(role agent.Role, cancel context.CancelFunc) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	tracker.cancels[role] = cancel
}

// Clear forgets the cancel for the role without invoking it. Use
// when a session has finished naturally — calling cancel on an
// already-completed context is a noop but the entry should be
// removed so a fresh session can register a new one.
func (tracker *SessionTracker) Clear(role agent.Role) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.cancels, role)
}

// Cancel invokes the registered cancel for the role and forgets it.
// No-op if no session is registered.
func (tracker *SessionTracker) Cancel(role agent.Role) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	cancel, ok := tracker.cancels[role]
	if !ok {
		return
	}
	cancel()
	delete(tracker.cancels, role)
}

// CancelAll invokes every registered cancel and clears them.
func (tracker *SessionTracker) CancelAll() {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	for role, cancel := range tracker.cancels {
		cancel()
		delete(tracker.cancels, role)
	}
}

// Add wraps the underlying WaitGroup for goroutines that should
// block shutdown drain.
func (tracker *SessionTracker) Add(delta int) { tracker.wg.Add(delta) }

// Done signals one tracked goroutine has completed; pair with Add.
func (tracker *SessionTracker) Done() { tracker.wg.Done() }

// Wait blocks until every tracked goroutine completes. Unbounded —
// callers that need a deadline should use DrainFor.
func (tracker *SessionTracker) Wait() { tracker.wg.Wait() }

// DrainFor waits for all tracked goroutines to finish or for grace
// to elapse, whichever comes first. Logs the outcome so operators
// can spot stuck shutdowns.
func (tracker *SessionTracker) DrainFor(grace time.Duration) {
	done := make(chan struct{})
	go func() {
		tracker.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("orchestrator: all sessions drained cleanly")
	case <-time.After(grace):
		log.Printf("orchestrator: shutdown grace (%s) elapsed with sessions still running — exiting anyway", grace)
	}
}
