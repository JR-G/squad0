package memory

import (
	"context"
	"fmt"
	"time"
)

// Outcome enumerates the possible outcomes of a session episode.
type Outcome string

const (
	// OutcomeSuccess indicates the session completed successfully.
	OutcomeSuccess Outcome = "success"
	// OutcomeFailure indicates the session failed.
	OutcomeFailure Outcome = "failure"
	// OutcomePartial indicates the session partially completed.
	OutcomePartial Outcome = "partial"
)

// Episode represents a single agent session's record.
type Episode struct {
	ID        int64
	Agent     string
	Ticket    string
	Summary   string
	Embedding []float32
	Outcome   Outcome
	CreatedAt time.Time
}

// EpisodeStore provides CRUD operations for session episodes.
type EpisodeStore struct {
	db *DB
}

// NewEpisodeStore creates an EpisodeStore backed by the given database.
func NewEpisodeStore(db *DB) *EpisodeStore {
	return &EpisodeStore{db: db}
}

// CreateEpisode inserts a new episode and its FTS5 index row atomically.
// If Embedding is non-nil, it is serialised and stored as a BLOB.
func (store *EpisodeStore) CreateEpisode(ctx context.Context, episode Episode) (int64, error) {
	tx, err := store.db.RawDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("beginning transaction: %w", err)
	}
	defer rollback(tx)

	var embeddingBlob []byte
	if episode.Embedding != nil {
		embeddingBlob = SerialiseVector(episode.Embedding)
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO episodes (agent, ticket, summary, embedding, outcome) VALUES (?, ?, ?, ?, ?)`,
		episode.Agent, episode.Ticket, episode.Summary, embeddingBlob, string(episode.Outcome),
	)
	if err != nil {
		return 0, fmt.Errorf("inserting episode: %w", err)
	}

	id, err := result.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("getting episode id: %w", err)
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO episodes_fts(rowid, summary, ticket) VALUES(?, ?, ?)`,
		id, episode.Summary, episode.Ticket,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting episode FTS: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("committing episode: %w", err)
	}

	return id, nil
}

// GetEpisode retrieves an episode by ID.
func (store *EpisodeStore) GetEpisode(ctx context.Context, id int64) (Episode, error) {
	var episode Episode
	var embeddingBlob []byte
	err := store.db.RawDB().QueryRowContext(ctx,
		`SELECT id, agent, ticket, summary, embedding, outcome, created_at FROM episodes WHERE id = ?`, id,
	).Scan(&episode.ID, &episode.Agent, &episode.Ticket, &episode.Summary,
		&embeddingBlob, &episode.Outcome, &episode.CreatedAt)
	if err != nil {
		return Episode{}, fmt.Errorf("getting episode %d: %w", id, err)
	}

	if embeddingBlob == nil {
		return episode, nil
	}

	episode.Embedding, err = DeserialiseVector(embeddingBlob)
	if err != nil {
		return Episode{}, fmt.Errorf("deserialising episode %d embedding: %w", id, err)
	}

	return episode, nil
}

// EpisodesByAgent returns all episodes for the given agent name, ordered
// by created_at descending.
func (store *EpisodeStore) EpisodesByAgent(ctx context.Context, agent string) ([]Episode, error) {
	rows, err := store.db.RawDB().QueryContext(ctx,
		`SELECT id, agent, ticket, summary, embedding, outcome, created_at
		 FROM episodes WHERE agent = ? ORDER BY created_at DESC`, agent,
	)
	if err != nil {
		return nil, fmt.Errorf("querying episodes for agent %s: %w", agent, err)
	}
	defer func() { _ = rows.Close() }()

	return scanEpisodes(rows)
}

// RecentEpisodes returns the N most recent episodes across all agents.
func (store *EpisodeStore) RecentEpisodes(ctx context.Context, limit int) ([]Episode, error) {
	rows, err := store.db.RawDB().QueryContext(ctx,
		`SELECT id, agent, ticket, summary, embedding, outcome, created_at
		 FROM episodes ORDER BY created_at DESC LIMIT ?`, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("querying recent episodes: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEpisodes(rows)
}

// EpisodesByTicket returns all episodes for a given ticket, most recent first.
func (store *EpisodeStore) EpisodesByTicket(ctx context.Context, ticket string) ([]Episode, error) {
	rows, err := store.db.RawDB().QueryContext(ctx,
		`SELECT id, agent, ticket, summary, embedding, outcome, created_at
		 FROM episodes WHERE ticket = ? ORDER BY created_at DESC`, ticket,
	)
	if err != nil {
		return nil, fmt.Errorf("querying episodes for ticket %s: %w", ticket, err)
	}
	defer func() { _ = rows.Close() }()

	return scanEpisodes(rows)
}

// UpdateEmbedding sets the embedding vector on an existing episode.
func (store *EpisodeStore) UpdateEmbedding(ctx context.Context, id int64, embedding []float32) error {
	blob := SerialiseVector(embedding)
	_, err := store.db.RawDB().ExecContext(ctx,
		`UPDATE episodes SET embedding = ? WHERE id = ?`, blob, id,
	)
	if err != nil {
		return fmt.Errorf("updating episode %d embedding: %w", id, err)
	}

	return nil
}

// EpisodesWithEmbeddings returns all episodes that have a non-null embedding.
func (store *EpisodeStore) EpisodesWithEmbeddings(ctx context.Context) ([]Episode, error) {
	rows, err := store.db.RawDB().QueryContext(ctx,
		`SELECT id, agent, ticket, summary, embedding, outcome, created_at
		 FROM episodes WHERE embedding IS NOT NULL`,
	)
	if err != nil {
		return nil, fmt.Errorf("querying episodes with embeddings: %w", err)
	}
	defer func() { _ = rows.Close() }()

	return scanEpisodes(rows)
}

func scanEpisodes(rows interface {
	Next() bool
	Scan(...any) error
	Err() error
},
) ([]Episode, error) {
	var episodes []Episode
	for rows.Next() {
		episode, err := scanOneEpisode(rows)
		if err != nil {
			return nil, err
		}
		episodes = append(episodes, episode)
	}
	return episodes, rows.Err()
}

func scanOneEpisode(row interface{ Scan(...any) error }) (Episode, error) {
	var episode Episode
	var embeddingBlob []byte

	if err := row.Scan(&episode.ID, &episode.Agent, &episode.Ticket, &episode.Summary,
		&embeddingBlob, &episode.Outcome, &episode.CreatedAt); err != nil {
		return Episode{}, fmt.Errorf("scanning episode: %w", err)
	}

	if embeddingBlob == nil {
		return episode, nil
	}

	var err error
	episode.Embedding, err = DeserialiseVector(embeddingBlob)
	if err != nil {
		return Episode{}, fmt.Errorf("deserialising episode embedding: %w", err)
	}

	return episode, nil
}
