package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// WorkingStore is the session-scoped scratchpad agents use to track
// in-progress state across tool calls within a single session.
// Distinct from FactStore / BeliefStore (long-term memory) and
// EpisodeStore (post-session log) — entries here are deliberately
// ephemeral and a Clear at session end is part of the lifecycle.
//
// Cognitive-memory analogue: working memory. The other long-term
// stores cover episodic (episodes), semantic (facts, beliefs,
// entities), and procedural (technique-typed facts) memory.
type WorkingStore struct {
	db *sql.DB
}

// NewWorkingStore wraps a memory.DB to read/write the
// working_memory table.
func NewWorkingStore(memDB *DB) *WorkingStore {
	return &WorkingStore{db: memDB.db}
}

// Set stores a key/value pair under sessionID, replacing any prior
// entry for the same (sessionID, key) pair. Empty session or key
// strings are rejected so callers don't accidentally write
// orphaned scratch.
func (store *WorkingStore) Set(ctx context.Context, sessionID, key, value string) error {
	if sessionID == "" {
		return errors.New("session ID is required")
	}
	if key == "" {
		return errors.New("key is required")
	}

	_, err := store.db.ExecContext(ctx, `
		INSERT INTO working_memory (session_id, key, value)
		VALUES (?, ?, ?)
		ON CONFLICT(session_id, key) DO UPDATE SET
			value = excluded.value,
			created_at = CURRENT_TIMESTAMP`,
		sessionID, key, value,
	)
	if err != nil {
		return fmt.Errorf("setting working memory %s/%s: %w", sessionID, key, err)
	}
	return nil
}

// Get returns the value stored under (sessionID, key). Returns the
// empty string and ErrNoEntry if nothing is stored — callers should
// branch on that rather than checking for empty value, because an
// empty value is a legal scratchpad entry.
func (store *WorkingStore) Get(ctx context.Context, sessionID, key string) (string, error) {
	var value string
	err := store.db.QueryRowContext(ctx,
		`SELECT value FROM working_memory WHERE session_id = ? AND key = ?`,
		sessionID, key,
	).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNoEntry
	}
	if err != nil {
		return "", fmt.Errorf("reading working memory %s/%s: %w", sessionID, key, err)
	}
	return value, nil
}

// Keys lists every key stored under the session. Useful so an agent
// can ask "what scratch did I leave for myself" without remembering
// each key.
func (store *WorkingStore) Keys(ctx context.Context, sessionID string) ([]string, error) {
	rows, err := store.db.QueryContext(ctx,
		`SELECT key FROM working_memory WHERE session_id = ? ORDER BY created_at`,
		sessionID,
	)
	if err != nil {
		return nil, fmt.Errorf("listing working memory keys for %s: %w", sessionID, err)
	}
	defer func() { _ = rows.Close() }()

	var keys []string
	for rows.Next() {
		var key string
		if scanErr := rows.Scan(&key); scanErr != nil {
			return nil, fmt.Errorf("scanning working memory key: %w", scanErr)
		}
		keys = append(keys, key)
	}
	return keys, rows.Err()
}

// Clear deletes every entry under the session. Called at session
// end to prevent scratch from leaking into the next session's
// working memory (or accumulating forever).
func (store *WorkingStore) Clear(ctx context.Context, sessionID string) error {
	_, err := store.db.ExecContext(ctx,
		`DELETE FROM working_memory WHERE session_id = ?`, sessionID,
	)
	if err != nil {
		return fmt.Errorf("clearing working memory for %s: %w", sessionID, err)
	}
	return nil
}

// ErrNoEntry is returned by Get when the session/key pair has no
// stored value. Distinct from a stored empty string.
var ErrNoEntry = errors.New("no working-memory entry")
