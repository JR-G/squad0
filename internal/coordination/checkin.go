package coordination

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// Status represents an agent's current working state.
type Status string

const (
	// StatusWorking indicates the agent is actively implementing.
	StatusWorking Status = "working"
	// StatusBlocked indicates the agent is waiting on something.
	StatusBlocked Status = "blocked"
	// StatusIdle indicates the agent has no current work.
	StatusIdle Status = "idle"
	// StatusReviewing indicates the agent is reviewing a PR.
	StatusReviewing Status = "reviewing"
	// StatusPaused indicates the agent has been paused by the CEO.
	StatusPaused Status = "paused"
)

// CheckIn represents an agent's current state in the coordination DB.
type CheckIn struct {
	ID            int64
	Agent         agent.Role
	Ticket        string
	Status        Status
	FilesTouching []string
	Message       string
	UpdatedAt     time.Time
}

// CheckInStore provides CRUD operations for agent check-ins.
type CheckInStore struct {
	db *sql.DB
	// writeMu serialises Upsert calls so concurrent writers from
	// multiple session goroutines can't race on the
	// INSERT…ON CONFLICT UPDATE path. SQLite's busy-timeout handles
	// cross-process races, but in-process the cheaper guarantee is
	// a mutex.
	writeMu sync.Mutex
}

// NewCheckInStore creates a CheckInStore backed by the given database.
func NewCheckInStore(db *sql.DB) *CheckInStore {
	return &CheckInStore{db: db}
}

// upsertMaxAttempts caps retries for transient SQLite locking errors.
// 3 attempts with backoff comfortably covers the WAL checkpoint window
// without holding the calling goroutine for long.
const upsertMaxAttempts = 3

// InitSchema creates the check-in table if it does not exist.
func (store *CheckInStore) InitSchema(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS checkin (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			agent TEXT NOT NULL UNIQUE,
			ticket TEXT,
			status TEXT NOT NULL,
			files_touching TEXT,
			message TEXT,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`)
	if err != nil {
		return fmt.Errorf("creating checkin table: %w", err)
	}
	return nil
}

// Upsert creates or updates a check-in for the given agent. Retries
// transient SQLite "database is locked"/"busy" errors so a contending
// writer doesn't leave the agent's status stale, and treats a
// cancelled context as a clean exit (paused agents trigger this on
// purpose).
func (store *CheckInStore) Upsert(ctx context.Context, checkIn CheckIn) error {
	filesJSON, err := json.Marshal(checkIn.FilesTouching)
	if err != nil {
		return fmt.Errorf("marshalling files: %w", err)
	}

	store.writeMu.Lock()
	defer store.writeMu.Unlock()

	var lastErr error
	for attempt := 1; attempt <= upsertMaxAttempts; attempt++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}

		_, lastErr = store.db.ExecContext(ctx, `
			INSERT INTO checkin (agent, ticket, status, files_touching, message, updated_at)
			VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
			ON CONFLICT(agent) DO UPDATE SET
				ticket = excluded.ticket,
				status = excluded.status,
				files_touching = excluded.files_touching,
				message = excluded.message,
				updated_at = CURRENT_TIMESTAMP`,
			string(checkIn.Agent), checkIn.Ticket, string(checkIn.Status), string(filesJSON), checkIn.Message,
		)
		if lastErr == nil {
			return nil
		}
		if !isSQLiteBusy(lastErr) {
			return fmt.Errorf("upserting checkin for %s: %w", checkIn.Agent, lastErr)
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(attempt) * 50 * time.Millisecond):
		}
	}

	return fmt.Errorf("upserting checkin for %s after %d attempts: %w", checkIn.Agent, upsertMaxAttempts, lastErr)
}

// isSQLiteBusy reports whether err looks like a SQLite locking
// failure that benefits from a retry. We match on the message rather
// than the driver error type to avoid pulling sqlite3 imports into
// this package.
func isSQLiteBusy(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database table is locked") || strings.Contains(msg, "sqlite_busy")
}

// GetByAgent returns the current check-in for the given agent.
func (store *CheckInStore) GetByAgent(ctx context.Context, role agent.Role) (CheckIn, error) {
	var checkIn CheckIn
	var filesJSON string
	var agentStr string
	var statusStr string

	err := store.db.QueryRowContext(ctx,
		`SELECT id, agent, ticket, status, files_touching, message, updated_at
		 FROM checkin WHERE agent = ?`, string(role),
	).Scan(&checkIn.ID, &agentStr, &checkIn.Ticket, &statusStr, &filesJSON, &checkIn.Message, &checkIn.UpdatedAt)
	if err != nil {
		return CheckIn{}, fmt.Errorf("getting checkin for %s: %w", role, err)
	}

	checkIn.Agent = agent.Role(agentStr)
	checkIn.Status = Status(statusStr)

	if err := json.Unmarshal([]byte(filesJSON), &checkIn.FilesTouching); err != nil {
		return CheckIn{}, fmt.Errorf("unmarshalling files for %s: %w", role, err)
	}

	return checkIn, nil
}

// GetAll returns all current check-ins.
func (store *CheckInStore) GetAll(ctx context.Context) ([]CheckIn, error) {
	rows, err := store.db.QueryContext(ctx,
		`SELECT id, agent, ticket, status, files_touching, message, updated_at FROM checkin`)
	if err != nil {
		return nil, fmt.Errorf("querying all checkins: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var checkIns []CheckIn
	for rows.Next() {
		checkIn, scanErr := scanCheckIn(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		checkIns = append(checkIns, checkIn)
	}

	return checkIns, rows.Err()
}

// IdleAgents returns the roles of all agents with idle status.
func (store *CheckInStore) IdleAgents(ctx context.Context) ([]agent.Role, error) {
	rows, err := store.db.QueryContext(ctx,
		`SELECT agent FROM checkin WHERE status = ?`, string(StatusIdle))
	if err != nil {
		return nil, fmt.Errorf("querying idle agents: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var roles []agent.Role
	for rows.Next() {
		var roleStr string
		if scanErr := rows.Scan(&roleStr); scanErr != nil {
			return nil, fmt.Errorf("scanning idle agent: %w", scanErr)
		}
		roles = append(roles, agent.Role(roleStr))
	}

	return roles, rows.Err()
}

// SetIdle sets the given agent's status to idle and clears their ticket
// and file list.
func (store *CheckInStore) SetIdle(ctx context.Context, role agent.Role) error {
	return store.Upsert(ctx, CheckIn{
		Agent:         role,
		Status:        StatusIdle,
		FilesTouching: []string{},
	})
}

func scanCheckIn(row interface{ Scan(...any) error }) (CheckIn, error) {
	var checkIn CheckIn
	var filesJSON string
	var agentStr string
	var statusStr string

	err := row.Scan(&checkIn.ID, &agentStr, &checkIn.Ticket, &statusStr, &filesJSON, &checkIn.Message, &checkIn.UpdatedAt)
	if err != nil {
		return CheckIn{}, fmt.Errorf("scanning checkin: %w", err)
	}

	checkIn.Agent = agent.Role(agentStr)
	checkIn.Status = Status(statusStr)

	if err := json.Unmarshal([]byte(filesJSON), &checkIn.FilesTouching); err != nil {
		return CheckIn{}, fmt.Errorf("unmarshalling files: %w", err)
	}

	return checkIn, nil
}
