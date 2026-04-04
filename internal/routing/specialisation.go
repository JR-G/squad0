package routing

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/JR-G/squad0/internal/agent"
)

// SpecialisationRecord tracks an agent's historical performance on a
// ticket category. Emerges from actual outcomes, not configuration.
type SpecialisationRecord struct {
	Role     agent.Role
	Category string
	Wins     int
	Losses   int
	Score    float64 // wins / (wins + losses)
}

// SpecialisationStore persists agent performance per ticket category.
type SpecialisationStore struct {
	db *sql.DB
}

// NewSpecialisationStore creates a store backed by the given database.
func NewSpecialisationStore(db *sql.DB) *SpecialisationStore {
	return &SpecialisationStore{db: db}
}

// InitSchema creates the specialisation table if it doesn't exist.
func (store *SpecialisationStore) InitSchema(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS specialisations (
			role TEXT NOT NULL,
			category TEXT NOT NULL,
			wins INTEGER NOT NULL DEFAULT 0,
			losses INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (role, category)
		)`)
	if err != nil {
		return fmt.Errorf("creating specialisations table: %w", err)
	}
	return nil
}

// Record logs a success or failure for the given role and category.
func (store *SpecialisationStore) Record(ctx context.Context, role agent.Role, category string, success bool) error {
	col := "losses"
	if success {
		col = "wins"
	}

	query := fmt.Sprintf(`
		INSERT INTO specialisations (role, category, %s) VALUES (?, ?, 1)
		ON CONFLICT(role, category) DO UPDATE SET %s = %s + 1`,
		col, col, col)

	_, err := store.db.ExecContext(ctx, query, string(role), category)
	if err != nil {
		return fmt.Errorf("recording specialisation for %s/%s: %w", role, category, err)
	}
	return nil
}

// BestForCategory returns agents sorted by success rate for a category.
func (store *SpecialisationStore) BestForCategory(ctx context.Context, category string) ([]SpecialisationRecord, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT role, category, wins, losses,
		       CAST(wins AS REAL) / CAST(wins + losses AS REAL) AS score
		FROM specialisations
		WHERE category = ? AND (wins + losses) >= 2
		ORDER BY score DESC`, category)
	if err != nil {
		return nil, fmt.Errorf("querying specialisations for %s: %w", category, err)
	}
	defer func() { _ = rows.Close() }()

	return scanSpecialisations(rows)
}

// StatsForRole returns all specialisation records for a given agent.
func (store *SpecialisationStore) StatsForRole(ctx context.Context, role agent.Role) ([]SpecialisationRecord, error) {
	rows, err := store.db.QueryContext(ctx, `
		SELECT role, category, wins, losses,
		       CASE WHEN (wins + losses) > 0
		            THEN CAST(wins AS REAL) / CAST(wins + losses AS REAL)
		            ELSE 0 END AS score
		FROM specialisations
		WHERE role = ?
		ORDER BY score DESC`, string(role))
	if err != nil {
		return nil, fmt.Errorf("querying stats for %s: %w", role, err)
	}
	defer func() { _ = rows.Close() }()

	return scanSpecialisations(rows)
}

func scanSpecialisations(rows *sql.Rows) ([]SpecialisationRecord, error) {
	var records []SpecialisationRecord
	for rows.Next() {
		var rec SpecialisationRecord
		var role string
		if err := rows.Scan(&role, &rec.Category, &rec.Wins, &rec.Losses, &rec.Score); err != nil {
			return nil, fmt.Errorf("scanning specialisation: %w", err)
		}
		rec.Role = agent.Role(role)
		records = append(records, rec)
	}
	return records, rows.Err()
}
