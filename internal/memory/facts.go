package memory

import (
	"context"
	"fmt"
	"time"
)

// FactType enumerates the kinds of facts stored.
type FactType string

const (
	// FactObservation represents something observed during a session.
	FactObservation FactType = "observation"
	// FactPreference represents a preference learned about the codebase.
	FactPreference FactType = "preference"
	// FactWarning represents a known pitfall or danger area.
	FactWarning FactType = "warning"
	// FactTechnique represents a technique that works well.
	FactTechnique FactType = "technique"
)

// Fact represents a specific learning tied to an entity.
type Fact struct {
	ID              int64
	EntityID        int64
	Content         string
	Type            FactType
	Confidence      float64
	Confirmations   int
	SourceEpisodeID *int64
	CreatedAt       time.Time
	InvalidatedAt   *time.Time
	LastAccessedAt  *time.Time
	AccessCount     int
}

// Belief represents a causal belief that evolves with evidence.
type Belief struct {
	ID              int64
	Content         string
	Confidence      float64
	Confirmations   int
	Contradictions  int
	LastConfirmedAt *time.Time
	CreatedAt       time.Time
	LastAccessedAt  *time.Time
	AccessCount     int
	SourceOutcome   string
}

// FactStore provides CRUD operations for facts and beliefs.
type FactStore struct {
	db *DB
}

// NewFactStore creates a FactStore backed by the given database.
func NewFactStore(db *DB) *FactStore {
	return &FactStore{db: db}
}

// CreateFact inserts a new fact and its FTS5 index row atomically.
func (store *FactStore) CreateFact(ctx context.Context, fact Fact) (int64, error) {
	tx, err := store.db.RawDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer rollback(tx)

	result, err := tx.ExecContext(ctx,
		`INSERT INTO facts (entity_id, content, fact_type, confidence, confirmations, source_episode_id)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		fact.EntityID, fact.Content, string(fact.Type), fact.Confidence, fact.Confirmations, fact.SourceEpisodeID,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting fact: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting fact id: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO facts_fts(rowid, content, fact_type) VALUES(?, ?, ?)`,
		id, fact.Content, string(fact.Type),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting fact FTS: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing fact: %w", err)
	}

	return id, nil
}

// GetFact retrieves a fact by ID.
func (store *FactStore) GetFact(ctx context.Context, id int64) (Fact, error) {
	var fact Fact
	err := store.db.RawDB().QueryRowContext(ctx,
		`SELECT id, entity_id, content, fact_type, confidence, confirmations, source_episode_id, created_at, invalidated_at,
		 last_accessed_at, access_count
		 FROM facts WHERE id = ?`, id,
	).Scan(&fact.ID, &fact.EntityID, &fact.Content, &fact.Type, &fact.Confidence,
		&fact.Confirmations, &fact.SourceEpisodeID, &fact.CreatedAt, &fact.InvalidatedAt,
		&fact.LastAccessedAt, &fact.AccessCount)
	if err != nil {
		return Fact{}, fmt.Errorf("getting fact %d: %w", id, err)
	}

	return fact, nil
}

// FactsByEntity returns all valid (non-invalidated) facts for the given
// entity, ordered by confidence descending.
func (store *FactStore) FactsByEntity(ctx context.Context, entityID int64) ([]Fact, error) {
	rows, err := store.db.RawDB().QueryContext(ctx,
		`SELECT id, entity_id, content, fact_type, confidence, confirmations, source_episode_id, created_at, invalidated_at,
		 last_accessed_at, access_count
		 FROM facts WHERE entity_id = ? AND invalidated_at IS NULL ORDER BY confidence DESC`, entityID,
	)
	if err != nil {
		return nil, fmt.Errorf("querying facts for entity %d: %w", entityID, err)
	}
	defer func() { _ = rows.Close() }()

	return scanFacts(rows)
}

// ConfirmFact increments the confirmation count and recalculates confidence.
func (store *FactStore) ConfirmFact(ctx context.Context, id int64) error {
	_, err := store.db.RawDB().ExecContext(ctx,
		`UPDATE facts SET confirmations = confirmations + 1,
		 confidence = 1.0 - (1.0 / CAST(confirmations + 2 AS REAL))
		 WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("confirming fact %d: %w", id, err)
	}

	return nil
}

// InvalidateFact sets the invalidated_at timestamp on a fact.
func (store *FactStore) InvalidateFact(ctx context.Context, id int64) error {
	_, err := store.db.RawDB().ExecContext(ctx,
		`UPDATE facts SET invalidated_at = CURRENT_TIMESTAMP WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("invalidating fact %d: %w", id, err)
	}

	return nil
}

// CreateBelief inserts a new belief and its FTS5 index row atomically.
func (store *FactStore) CreateBelief(ctx context.Context, belief Belief) (int64, error) {
	tx, err := store.db.RawDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer rollback(tx)

	result, err := tx.ExecContext(ctx,
		`INSERT INTO beliefs (content, confidence, confirmations, contradictions)
		 VALUES (?, ?, ?, ?)`,
		belief.Content, belief.Confidence, belief.Confirmations, belief.Contradictions,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting belief: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting belief id: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO beliefs_fts(rowid, content) VALUES(?, ?)`,
		id, belief.Content,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting belief FTS: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing belief: %w", err)
	}

	return id, nil
}

// GetBelief retrieves a belief by ID.
func (store *FactStore) GetBelief(ctx context.Context, id int64) (Belief, error) {
	var belief Belief
	err := store.db.RawDB().QueryRowContext(ctx,
		`SELECT id, content, confidence, confirmations, contradictions, last_confirmed_at, created_at,
		 last_accessed_at, access_count, source_outcome
		 FROM beliefs WHERE id = ?`, id,
	).Scan(&belief.ID, &belief.Content, &belief.Confidence, &belief.Confirmations,
		&belief.Contradictions, &belief.LastConfirmedAt, &belief.CreatedAt,
		&belief.LastAccessedAt, &belief.AccessCount, &belief.SourceOutcome)
	if err != nil {
		return Belief{}, fmt.Errorf("getting belief %d: %w", id, err)
	}

	return belief, nil
}

// ConfirmBelief increments the confirmation count, updates
// last_confirmed_at, and recalculates confidence.
func (store *FactStore) ConfirmBelief(ctx context.Context, id int64) error {
	_, err := store.db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET confirmations = confirmations + 1,
		 last_confirmed_at = CURRENT_TIMESTAMP,
		 confidence = CAST(confirmations + 1 AS REAL) / CAST(confirmations + 1 + contradictions + 1 AS REAL)
		 WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("confirming belief %d: %w", id, err)
	}

	return nil
}

// ContradictBelief increments the contradiction count and recalculates
// confidence.
func (store *FactStore) ContradictBelief(ctx context.Context, id int64) error {
	_, err := store.db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET contradictions = contradictions + 1,
		 confidence = CAST(confirmations AS REAL) / CAST(confirmations + contradictions + 2 AS REAL)
		 WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("contradicting belief %d: %w", id, err)
	}

	return nil
}

// TopBeliefs returns the N highest-confidence beliefs.
func (store *FactStore) TopBeliefs(ctx context.Context, limit int) ([]Belief, error) {
	rows, err := store.db.RawDB().QueryContext(ctx,
		`SELECT id, content, confidence, confirmations, contradictions, last_confirmed_at, created_at,
		 last_accessed_at, access_count, source_outcome
		 FROM beliefs ORDER BY confidence DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying top beliefs: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanBeliefs(rows)
}

// RecordFactAccess bumps the access count and last_accessed_at for a fact.
func (store *FactStore) RecordFactAccess(ctx context.Context, id int64) error {
	_, err := store.db.RawDB().ExecContext(ctx,
		`UPDATE facts SET access_count = access_count + 1, last_accessed_at = CURRENT_TIMESTAMP WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("recording fact access %d: %w", id, err)
	}
	return nil
}

// RecordBeliefAccess bumps the access count and last_accessed_at for a belief.
func (store *FactStore) RecordBeliefAccess(ctx context.Context, id int64) error {
	_, err := store.db.RawDB().ExecContext(ctx,
		`UPDATE beliefs SET access_count = access_count + 1, last_accessed_at = CURRENT_TIMESTAMP WHERE id = ?`, id,
	)
	if err != nil {
		return fmt.Errorf("recording belief access %d: %w", id, err)
	}
	return nil
}

// EmotionalSalienceMultiplier returns the confidence multiplier based on
// whether the belief was formed from a failed or successful session.
// Failed sessions produce stronger initial memories (1.4x).
func EmotionalSalienceMultiplier(outcome string) float64 {
	switch outcome {
	case "failure":
		return 1.4
	case "partial":
		return 1.2
	default:
		return 1.0
	}
}

func scanFacts(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
},
) ([]Fact, error) {
	var facts []Fact
	for rows.Next() {
		var fact Fact
		if err := rows.Scan(&fact.ID, &fact.EntityID, &fact.Content, &fact.Type, &fact.Confidence,
			&fact.Confirmations, &fact.SourceEpisodeID, &fact.CreatedAt, &fact.InvalidatedAt,
			&fact.LastAccessedAt, &fact.AccessCount); err != nil {
			return nil, fmt.Errorf("scanning fact: %w", err)
		}
		facts = append(facts, fact)
	}
	return facts, rows.Err()
}

func scanBeliefs(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
},
) ([]Belief, error) {
	var beliefs []Belief
	for rows.Next() {
		var belief Belief
		if err := rows.Scan(&belief.ID, &belief.Content, &belief.Confidence, &belief.Confirmations,
			&belief.Contradictions, &belief.LastConfirmedAt, &belief.CreatedAt,
			&belief.LastAccessedAt, &belief.AccessCount, &belief.SourceOutcome); err != nil {
			return nil, fmt.Errorf("scanning belief: %w", err)
		}
		beliefs = append(beliefs, belief)
	}
	return beliefs, rows.Err()
}
