package orchestrator

import "sync"

// BlockedTickets is the set of tickets squad0 has explicitly given
// up on after exhausting review cycles. Smart-assigner consults this
// before proposing assignments so a freshly-blocked ticket isn't
// instantly re-picked into a new work item — that loop was the
// JAM-24 failure mode where blocking + re-assignment cycled forever.
//
// In-memory only — restarting the orchestrator clears the set, which
// is fine: a fresh start is the operator's chance to re-triage.
type BlockedTickets struct {
	mu      sync.RWMutex
	tickets map[string]struct{}
}

// NewBlockedTickets returns an empty set ready for use.
func NewBlockedTickets() *BlockedTickets {
	return &BlockedTickets{tickets: make(map[string]struct{})}
}

// Block marks a ticket as blocked. Subsequent IsBlocked calls return
// true until Clear is called.
func (b *BlockedTickets) Block(ticket string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.tickets[ticket] = struct{}{}
}

// IsBlocked reports whether the ticket is currently blocked.
func (b *BlockedTickets) IsBlocked(ticket string) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	_, ok := b.tickets[ticket]
	return ok
}

// Clear removes a ticket from the blocked set — used when an
// operator manually un-blocks via Slack command (future work).
func (b *BlockedTickets) Clear(ticket string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.tickets, ticket)
}

// Snapshot returns a copy of the blocked set for diagnostics.
func (b *BlockedTickets) Snapshot() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]string, 0, len(b.tickets))
	for ticket := range b.tickets {
		out = append(out, ticket)
	}
	return out
}

// BlockedTickets returns the orchestrator's blocked-tickets set so
// the smart-assigner (or future Slack triage commands) can consult
// or mutate it.
func (orch *Orchestrator) BlockedTickets() *BlockedTickets {
	return orch.blockedTickets
}
