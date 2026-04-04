package orchestrator

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

const (
	// escalationStaleThreshold is how long a triage item can sit
	// without human action before it gets re-escalated.
	escalationStaleThreshold = 4 * time.Hour

	// maxEscalations is the maximum number of times a situation
	// can be re-escalated before being auto-blocked.
	maxEscalations = 2
)

// EscalationTracker monitors triage items for staleness and
// re-escalates or auto-blocks them. Runs as part of sensors.
type EscalationTracker struct {
	mu    sync.Mutex
	items map[string]*escalatedItem // situation key → item
}

type escalatedItem struct {
	Situation    Situation
	EscalatedAt  time.Time
	Acknowledged bool
}

// NewEscalationTracker creates an empty tracker.
func NewEscalationTracker() *EscalationTracker {
	return &EscalationTracker{
		items: make(map[string]*escalatedItem),
	}
}

// Track records a situation that was posted to triage. Starts the
// stale timer. Subsequent calls for the same key are ignored.
func (tracker *EscalationTracker) Track(sit Situation) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	key := sit.Key()
	if _, exists := tracker.items[key]; exists {
		return
	}

	tracker.items[key] = &escalatedItem{
		Situation:   sit,
		EscalatedAt: time.Now(),
	}
}

// Acknowledge marks a triage item as seen by a human. Prevents
// re-escalation. Called when the CEO reacts or comments.
func (tracker *EscalationTracker) Acknowledge(key string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	item, ok := tracker.items[key]
	if !ok {
		return
	}
	item.Acknowledged = true
}

// CheckStale returns situations that have been in triage longer than
// the stale threshold without acknowledgement. These need re-escalation.
func (tracker *EscalationTracker) CheckStale() []Situation {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	stale := make([]Situation, 0, len(tracker.items))
	now := time.Now()

	for key, item := range tracker.items {
		if item.Acknowledged {
			continue
		}

		if now.Sub(item.EscalatedAt) < escalationStaleThreshold {
			continue
		}

		if item.Situation.Escalations >= maxEscalations {
			// Max escalations reached — will be auto-blocked.
			continue
		}

		stale = append(stale, item.Situation)
		// Update the escalation time so it doesn't re-fire next tick.
		item.EscalatedAt = now
		item.Situation.Escalations++
		tracker.items[key] = item
	}

	return stale
}

// AutoBlocked returns situations that have hit max escalations without
// human action. These tickets should be blocked automatically.
func (tracker *EscalationTracker) AutoBlocked() []Situation {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()

	blocked := make([]Situation, 0, len(tracker.items))
	now := time.Now()

	for key, item := range tracker.items {
		if item.Acknowledged {
			continue
		}

		if item.Situation.Escalations < maxEscalations {
			continue
		}

		if now.Sub(item.EscalatedAt) < escalationStaleThreshold {
			continue
		}

		blocked = append(blocked, item.Situation)
		delete(tracker.items, key)
	}

	return blocked
}

// Remove clears a tracked item. Called when the situation is resolved.
func (tracker *EscalationTracker) Remove(key string) {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	delete(tracker.items, key)
}

// Len returns the number of tracked items.
func (tracker *EscalationTracker) Len() int {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	return len(tracker.items)
}

// RunEscalationCheckForTest exports RunEscalationCheck for testing.
func (orch *Orchestrator) RunEscalationCheckForTest(t interface{ Helper() }) {
	t.Helper()
	orch.RunEscalationCheck(context.Background())
}

// RunEscalationCheck runs as part of sensors. Re-escalates stale
// triage items and auto-blocks those that hit the limit.
func (orch *Orchestrator) RunEscalationCheck(ctx context.Context) {
	if orch.escalations == nil || orch.situations == nil {
		return
	}

	// Re-escalate stale items — bump severity and re-queue.
	for _, sit := range orch.escalations.CheckStale() {
		log.Printf("escalation: re-escalating %s (%s, escalation #%d)",
			sit.Ticket, sit.Type, sit.Escalations)
		orch.situations.Escalate(sit)
	}

	// Auto-block items that hit max escalations.
	for _, sit := range orch.escalations.AutoBlocked() {
		log.Printf("escalation: auto-blocking %s after %d escalations", sit.Ticket, sit.Escalations)
		orch.announceAsRole(ctx, "triage",
			fmt.Sprintf("%s auto-blocked — %d escalations without human action. Ticket will be blocked on Linear.", sit.Ticket, sit.Escalations),
			agent.RolePM)

		// Move ticket to blocked state.
		pmAgent := orch.agents[agent.RolePM]
		if pmAgent != nil {
			go MoveTicketState(ctx, pmAgent, sit.Ticket, "Blocked")
		}
	}
}
