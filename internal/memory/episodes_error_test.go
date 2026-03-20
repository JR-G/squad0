//go:build sqlite_fts5

package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEpisodeStore_CreateEpisode_DroppedFTS_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	_, err := db.RawDB().Exec(`DROP TABLE episodes_fts`)
	require.NoError(t, err)

	_, err = store.CreateEpisode(ctx, memory.Episode{
		Agent:   "engineer-1",
		Summary: "should fail on FTS insert",
		Outcome: memory.OutcomeSuccess,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "FTS")
}

func TestEpisodeStore_CreateEpisode_DroppedTable_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	_, err := db.RawDB().Exec(`DROP TABLE episodes`)
	require.NoError(t, err)

	_, err = store.CreateEpisode(ctx, memory.Episode{
		Agent:   "engineer-1",
		Summary: "should fail on insert",
		Outcome: memory.OutcomeSuccess,
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "inserting episode")
}

func TestEpisodeStore_GetEpisode_NilEmbedding_ReturnsEpisode(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	episodeID, err := store.CreateEpisode(ctx, memory.Episode{
		Agent:   "pm",
		Ticket:  "SQ-100",
		Summary: "no embedding session",
		Outcome: memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	episode, err := store.GetEpisode(ctx, episodeID)

	require.NoError(t, err)
	assert.Nil(t, episode.Embedding)
	assert.Equal(t, "pm", episode.Agent)
}

func TestEpisodeStore_GetEpisode_CorruptEmbedding_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	episodeID, err := store.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "corrupt", Outcome: memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	_, err = db.RawDB().Exec(`UPDATE episodes SET embedding = X'AABBCC' WHERE id = ?`, episodeID)
	require.NoError(t, err)

	_, err = store.GetEpisode(ctx, episodeID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "deserialising")
}

func TestEpisodeStore_EpisodesByAgent_ClosedDB_ScanError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	require.NoError(t, db.Close())

	_, err := store.EpisodesByAgent(context.Background(), "test")

	assert.Error(t, err)
}

func TestEpisodeStore_EpisodesWithEmbeddings_CorruptData_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	episodeID, err := store.CreateEpisode(ctx, memory.Episode{
		Agent:     "engineer-1",
		Summary:   "has corrupt embedding",
		Embedding: []float32{0.1, 0.2},
		Outcome:   memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	_, err = db.RawDB().Exec(`UPDATE episodes SET embedding = X'AABB' WHERE id = ?`, episodeID)
	require.NoError(t, err)

	_, err = store.EpisodesWithEmbeddings(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "deserialising")
}
