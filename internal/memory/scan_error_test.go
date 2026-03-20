//go:build sqlite_fts5

package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFactsByEntity_ScanError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	entityID, err := graphStore.CreateEntity(ctx, memory.Entity{
		Type: memory.EntityModule, Name: "scan-err",
	})
	require.NoError(t, err)

	_, err = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "valid fact",
		Type: memory.FactObservation, Confidence: 0.5,
	})
	require.NoError(t, err)

	// Alter the facts table to make the scan fail by adding a row with
	// a type that can't scan into the expected struct.
	_, err = db.RawDB().Exec(
		`INSERT INTO facts (entity_id, content, fact_type, confidence, confirmations, created_at)
		 VALUES (?, 'bad', 'obs', 'not-a-float', 1, 'not-a-timestamp')`,
		entityID,
	)
	require.NoError(t, err)

	_, err = factStore.FactsByEntity(ctx, entityID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scanning")
}

func TestTopBeliefs_ScanError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ctx := context.Background()

	_, err := factStore.CreateBelief(ctx, memory.Belief{
		Content: "valid belief", Confidence: 0.5,
	})
	require.NoError(t, err)

	// Insert a row with invalid data to cause a scan error
	_, err = db.RawDB().Exec(
		`INSERT INTO beliefs (content, confidence, confirmations, contradictions, created_at)
		 VALUES ('bad', 'not-a-float', 1, 0, 'not-a-timestamp')`,
	)
	require.NoError(t, err)

	_, err = factStore.TopBeliefs(ctx, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scanning")
}

func TestEpisodesByAgent_ScanError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	_, err := store.CreateEpisode(ctx, memory.Episode{
		Agent: "scan-err-agent", Summary: "valid", Outcome: memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	// Insert a row with invalid data
	_, err = db.RawDB().Exec(
		`INSERT INTO episodes (agent, summary, outcome, created_at)
		 VALUES ('scan-err-agent', 'bad', 'success', 'not-a-timestamp')`,
	)
	require.NoError(t, err)

	_, err = store.EpisodesByAgent(ctx, "scan-err-agent")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scanning")
}

func TestRecentEpisodes_ScanError_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	_, err := db.RawDB().Exec(
		`INSERT INTO episodes (agent, summary, outcome, created_at)
		 VALUES ('scan-agent', 'bad', 'success', 'not-a-timestamp')`,
	)
	require.NoError(t, err)

	_, err = store.RecentEpisodes(ctx, 10)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "scanning")
}
