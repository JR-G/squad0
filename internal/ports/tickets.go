package ports

import (
	"context"
	"time"
)

// Ticket is the project-management entity squad0 assigns to engineers.
type Ticket struct {
	ID          string // human-readable identifier ("JAM-42")
	Title       string
	Description string
	State       string // "Backlog" | "Todo" | "In Progress" | "In Review" | "Done"
	AssigneeID  string // empty if unassigned
	Priority    int    // 1=urgent, 4=low; 0=none
	UpdatedAt   time.Time
	DependsOn   []string // upstream ticket IDs that must be Done first
}

// TicketSource is the contract for the team's project-management
// system (Linear today; could be Jira / GitHub Issues / Notion
// tomorrow).
//
// Implementations must be safe for concurrent use and handle their
// own auth + retry. Squad0's orchestrator treats the source as the
// authoritative state for ticket priority and dependencies.
type TicketSource interface {
	// ListReady returns tickets in a workable state (Backlog or
	// Todo) for the configured project. Implementations should
	// honour the project/team scoping established at construction.
	ListReady(ctx context.Context) ([]Ticket, error)

	// Get fetches a single ticket by ID.
	Get(ctx context.Context, ticketID string) (Ticket, error)

	// UpdateState transitions a ticket to a new workflow state.
	// Names match the host's vocabulary ("In Progress", "Done", etc.).
	UpdateState(ctx context.Context, ticketID, newState string) error

	// CreateComment attaches a free-text comment to the ticket.
	CreateComment(ctx context.Context, ticketID, body string) error
}
