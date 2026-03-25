package pipeline

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// WorkItem tracks a single piece of work through the pipeline.
type WorkItem struct {
	ID           int64
	Ticket       string
	Engineer     agent.Role
	Reviewer     agent.Role
	Stage        Stage
	PRURL        string
	Branch       string
	ReviewCycles int
	StartedAt    time.Time
	UpdatedAt    time.Time
	FinishedAt   *time.Time
}

// WorkItemStore provides CRUD operations for pipeline work items.
type WorkItemStore struct {
	db *sql.DB
}

// NewWorkItemStore creates a WorkItemStore backed by the given database.
func NewWorkItemStore(db *sql.DB) *WorkItemStore {
	return &WorkItemStore{db: db}
}

// InitSchema creates the work_items table if it does not exist.
func (store *WorkItemStore) InitSchema(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS work_items (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			ticket TEXT NOT NULL,
			engineer TEXT NOT NULL,
			reviewer TEXT DEFAULT '',
			stage TEXT NOT NULL DEFAULT 'assigned',
			pr_url TEXT DEFAULT '',
			branch TEXT DEFAULT '',
			review_cycles INTEGER DEFAULT 0,
			started_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			finished_at TIMESTAMP
		)`)
	if err != nil {
		return fmt.Errorf("creating work_items table: %w", err)
	}

	_, err = store.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_work_items_engineer_stage
		ON work_items(engineer, stage)`)
	if err != nil {
		return fmt.Errorf("creating engineer_stage index: %w", err)
	}

	_, err = store.db.ExecContext(ctx, `
		CREATE INDEX IF NOT EXISTS idx_work_items_ticket
		ON work_items(ticket)`)
	if err != nil {
		return fmt.Errorf("creating ticket index: %w", err)
	}

	return nil
}

// Create inserts a new work item and returns its ID.
func (store *WorkItemStore) Create(ctx context.Context, item WorkItem) (int64, error) {
	result, err := store.db.ExecContext(ctx, `
		INSERT INTO work_items (ticket, engineer, stage, branch)
		VALUES (?, ?, ?, ?)`,
		item.Ticket, string(item.Engineer), string(item.Stage), item.Branch,
	)
	if err != nil {
		return 0, fmt.Errorf("creating work item for %s: %w", item.Ticket, err)
	}

	return result.LastInsertId()
}

// Advance moves a work item to the given stage.
func (store *WorkItemStore) Advance(ctx context.Context, itemID int64, stage Stage) error {
	query := `UPDATE work_items SET stage = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`
	if stage.IsTerminal() {
		query = `UPDATE work_items SET stage = ?, updated_at = CURRENT_TIMESTAMP, finished_at = CURRENT_TIMESTAMP WHERE id = ?`
	}

	_, err := store.db.ExecContext(ctx, query, string(stage), itemID)
	if err != nil {
		return fmt.Errorf("advancing work item %d to %s: %w", itemID, stage, err)
	}

	return nil
}

// SetPRURL records the pull request URL for a work item.
func (store *WorkItemStore) SetPRURL(ctx context.Context, itemID int64, prURL string) error {
	_, err := store.db.ExecContext(ctx, `
		UPDATE work_items SET pr_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		prURL, itemID,
	)
	if err != nil {
		return fmt.Errorf("setting PR URL for work item %d: %w", itemID, err)
	}

	return nil
}

// SetReviewer records which agent is reviewing the work item.
func (store *WorkItemStore) SetReviewer(ctx context.Context, itemID int64, reviewer agent.Role) error {
	_, err := store.db.ExecContext(ctx, `
		UPDATE work_items SET reviewer = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		string(reviewer), itemID,
	)
	if err != nil {
		return fmt.Errorf("setting reviewer for work item %d: %w", itemID, err)
	}

	return nil
}

// IncrementReviewCycles bumps the review cycle count by one.
func (store *WorkItemStore) IncrementReviewCycles(ctx context.Context, itemID int64) error {
	_, err := store.db.ExecContext(ctx, `
		UPDATE work_items SET review_cycles = review_cycles + 1, updated_at = CURRENT_TIMESTAMP WHERE id = ?`,
		itemID,
	)
	if err != nil {
		return fmt.Errorf("incrementing review cycles for work item %d: %w", itemID, err)
	}

	return nil
}

// GetByID returns a single work item by its ID.
func (store *WorkItemStore) GetByID(ctx context.Context, itemID int64) (WorkItem, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, ticket, engineer, reviewer, stage, pr_url, branch,
		       review_cycles, started_at, updated_at, finished_at
		FROM work_items WHERE id = ?`, itemID)

	return scanWorkItem(row)
}

// ActiveByTicket returns the latest non-terminal work item for a ticket.
func (store *WorkItemStore) ActiveByTicket(ctx context.Context, ticket string) (WorkItem, error) {
	row := store.db.QueryRowContext(ctx, `
		SELECT id, ticket, engineer, reviewer, stage, pr_url, branch,
		       review_cycles, started_at, updated_at, finished_at
		FROM work_items
		WHERE ticket = ? AND stage NOT IN ('merged', 'failed')
		ORDER BY id DESC LIMIT 1`, ticket)

	return scanWorkItem(row)
}

// OpenByEngineer returns all non-terminal work items for an engineer.
func (store *WorkItemStore) OpenByEngineer(ctx context.Context, role agent.Role) ([]WorkItem, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, ticket, engineer, reviewer, stage, pr_url, branch,
		       review_cycles, started_at, updated_at, finished_at
		FROM work_items
		WHERE engineer = ? AND stage NOT IN ('merged', 'failed')
		ORDER BY id`, string(role))
	if err != nil {
		return nil, fmt.Errorf("querying open items for %s: %w", role, err)
	}
	defer func() { _ = rows.Close() }()

	return scanWorkItems(rows)
}

// CompletedByEngineer returns all merged work items for an engineer.
func (store *WorkItemStore) CompletedByEngineer(ctx context.Context, role agent.Role) ([]WorkItem, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT id, ticket, engineer, reviewer, stage, pr_url, branch,
		       review_cycles, started_at, updated_at, finished_at
		FROM work_items
		WHERE engineer = ? AND stage = 'merged'
		ORDER BY finished_at DESC`, string(role))
	if err != nil {
		return nil, fmt.Errorf("querying completed items for %s: %w", role, err)
	}
	defer func() { _ = rows.Close() }()

	return scanWorkItems(rows)
}

func scanWorkItem(row interface{ Scan(...any) error }) (WorkItem, error) {
	var item WorkItem
	var engineer, reviewer, stage string

	err := row.Scan(
		&item.ID, &item.Ticket, &engineer, &reviewer, &stage,
		&item.PRURL, &item.Branch, &item.ReviewCycles,
		&item.StartedAt, &item.UpdatedAt, &item.FinishedAt,
	)
	if err != nil {
		return WorkItem{}, fmt.Errorf("scanning work item: %w", err)
	}

	item.Engineer = agent.Role(engineer)
	item.Reviewer = agent.Role(reviewer)
	item.Stage = Stage(stage)

	return item, nil
}

func scanWorkItems(rows *sql.Rows) ([]WorkItem, error) {
	var items []WorkItem

	for rows.Next() {
		item, err := scanWorkItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}
