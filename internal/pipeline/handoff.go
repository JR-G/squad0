package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Handoff captures the state when a session ends so the next session
// for the same ticket can resume where the predecessor left off.
type Handoff struct {
	ID        int64
	Ticket    string
	Agent     string
	Status    string // "completed", "failed", "partial"
	Summary   string
	Remaining string
	GitBranch string
	GitState  string // "clean", "dirty", "conflicting"
	Blockers  string
	CreatedAt time.Time
}

// HandoffStore provides CRUD operations for session handoffs.
type HandoffStore struct {
	db *sql.DB
}

// NewHandoffStore creates a HandoffStore backed by the given database.
func NewHandoffStore(db *sql.DB) *HandoffStore {
	return &HandoffStore{db: db}
}

// InitSchema creates the handoffs table if it does not exist.
func (store *HandoffStore) InitSchema(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS handoffs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ticket TEXT NOT NULL,
			agent TEXT NOT NULL,
			status TEXT NOT NULL,
			summary TEXT NOT NULL,
			remaining TEXT,
			git_branch TEXT,
			git_state TEXT,
			blockers TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`)
	if err != nil {
		return fmt.Errorf("creating handoffs table: %w", err)
	}

	_, err = store.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_handoffs_ticket ON handoffs(ticket)`)
	if err != nil {
		return fmt.Errorf("creating handoffs ticket index: %w", err)
	}

	return nil
}

// Create inserts a new handoff and returns its ID.
func (store *HandoffStore) Create(ctx context.Context, handoff Handoff) (int64, error) {
	result, err := store.db.ExecContext(ctx, `
		INSERT INTO handoffs (ticket, agent, status, summary, remaining, git_branch, git_state, blockers)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		handoff.Ticket, handoff.Agent, handoff.Status, handoff.Summary,
		handoff.Remaining, handoff.GitBranch, handoff.GitState, handoff.Blockers,
	)
	if err != nil {
		return 0, fmt.Errorf("creating handoff for %s: %w", handoff.Ticket, err)
	}

	return result.LastInsertId()
}

// LatestForTicket returns the most recent handoff for a ticket.
// Returns sql.ErrNoRows if no handoff exists.
func (store *HandoffStore) LatestForTicket(ctx context.Context, ticket string) (Handoff, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, ticket, agent, status, summary, remaining,
		       git_branch, git_state, blockers, created_at
		FROM handoffs
		WHERE ticket = ?
		ORDER BY id DESC LIMIT 1`, ticket)

	return scanHandoff(row)
}

// AllForTicket returns all handoffs for a ticket, most recent first.
func (store *HandoffStore) AllForTicket(ctx context.Context, ticket string) ([]Handoff, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, ticket, agent, status, summary, remaining,
		       git_branch, git_state, blockers, created_at
		FROM handoffs
		WHERE ticket = ?
		ORDER BY id DESC`, ticket)
	if err != nil {
		return nil, fmt.Errorf("querying handoffs for ticket %s: %w", ticket, err)
	}
	defer func() { _ = rows.Close() }()

	var handoffs []Handoff
	for rows.Next() {
		var handoff Handoff
		var remaining, gitBranch, gitState, blockers sql.NullString

		scanErr := rows.Scan(
			&handoff.ID, &handoff.Ticket, &handoff.Agent, &handoff.Status,
			&handoff.Summary, &remaining, &gitBranch, &gitState, &blockers,
			&handoff.CreatedAt,
		)
		if scanErr != nil {
			return nil, fmt.Errorf("scanning handoff row: %w", scanErr)
		}

		handoff.Remaining = remaining.String
		handoff.GitBranch = gitBranch.String
		handoff.GitState = gitState.String
		handoff.Blockers = blockers.String
		handoffs = append(handoffs, handoff)
	}

	return handoffs, rows.Err()
}

func scanHandoff(row interface{ Scan(...any) error }) (Handoff, error) {
	var handoff Handoff
	var remaining, gitBranch, gitState, blockers sql.NullString

	err := row.Scan(
		&handoff.ID, &handoff.Ticket, &handoff.Agent, &handoff.Status,
		&handoff.Summary, &remaining, &gitBranch, &gitState, &blockers,
		&handoff.CreatedAt,
	)
	if err != nil {
		return Handoff{}, fmt.Errorf("scanning handoff: %w", err)
	}

	handoff.Remaining = remaining.String
	handoff.GitBranch = gitBranch.String
	handoff.GitState = gitState.String
	handoff.Blockers = blockers.String

	return handoff, nil
}
