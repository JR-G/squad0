package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEpisodeStore_CreateEpisode_ReturnsID(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)

	episodeID, err := store.CreateEpisode(context.Background(), memory.Episode{
		Agent:   "engineer-1",
		Ticket:  "SQ-42",
		Summary: "Implemented auth middleware",
		Outcome: memory.OutcomeSuccess,
	})

	require.NoError(t, err)
	assert.Greater(t, episodeID, int64(0))
}

func TestEpisodeStore_CreateEpisode_WithEmbedding(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)

	embedding := []float32{0.1, 0.2, 0.3}
	episodeID, err := store.CreateEpisode(context.Background(), memory.Episode{
		Agent:     "engineer-2",
		Ticket:    "SQ-43",
		Summary:   "Fixed payment bug",
		Embedding: embedding,
		Outcome:   memory.OutcomeSuccess,
	})
	require.NoError(t, err)

	episode, err := store.GetEpisode(context.Background(), episodeID)

	require.NoError(t, err)
	require.Len(t, episode.Embedding, 3)
	assert.InDelta(t, 0.1, float64(episode.Embedding[0]), 0.001)
}

func TestEpisodeStore_GetEpisode_ReturnsEpisode(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	episodeID, _ := store.CreateEpisode(ctx, memory.Episode{
		Agent:   "pm",
		Ticket:  "SQ-1",
		Summary: "Ran standup",
		Outcome: memory.OutcomeSuccess,
	})

	episode, err := store.GetEpisode(ctx, episodeID)

	require.NoError(t, err)
	assert.Equal(t, "pm", episode.Agent)
	assert.Equal(t, "SQ-1", episode.Ticket)
	assert.Equal(t, "Ran standup", episode.Summary)
	assert.Equal(t, memory.OutcomeSuccess, episode.Outcome)
}

func TestEpisodeStore_GetEpisode_NotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)

	_, err := store.GetEpisode(context.Background(), 999)

	require.Error(t, err)
}

func TestEpisodeStore_EpisodesByAgent_ReturnsFiltered(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	_, _ = store.CreateEpisode(ctx, memory.Episode{Agent: "engineer-1", Summary: "task 1", Outcome: memory.OutcomeSuccess})
	_, _ = store.CreateEpisode(ctx, memory.Episode{Agent: "engineer-2", Summary: "task 2", Outcome: memory.OutcomeSuccess})
	_, _ = store.CreateEpisode(ctx, memory.Episode{Agent: "engineer-1", Summary: "task 3", Outcome: memory.OutcomeFailure})

	episodes, err := store.EpisodesByAgent(ctx, "engineer-1")

	require.NoError(t, err)
	assert.Len(t, episodes, 2)
}

func TestEpisodeStore_RecentEpisodes_ReturnsLimited(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		_, _ = store.CreateEpisode(ctx, memory.Episode{Agent: "pm", Summary: "episode", Outcome: memory.OutcomeSuccess})
	}

	episodes, err := store.RecentEpisodes(ctx, 3)

	require.NoError(t, err)
	assert.Len(t, episodes, 3)
}

func TestEpisodeStore_UpdateEmbedding_SetsEmbedding(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	episodeID, _ := store.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Summary: "no embedding yet", Outcome: memory.OutcomeSuccess,
	})

	embedding := []float32{0.5, 0.6, 0.7}
	err := store.UpdateEmbedding(ctx, episodeID, embedding)
	require.NoError(t, err)

	episode, err := store.GetEpisode(ctx, episodeID)

	require.NoError(t, err)
	require.Len(t, episode.Embedding, 3)
	assert.InDelta(t, 0.5, float64(episode.Embedding[0]), 0.001)
}

func TestEpisodeStore_EpisodesWithEmbeddings_FiltersNulls(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := memory.NewEpisodeStore(db)
	ctx := context.Background()

	_, _ = store.CreateEpisode(ctx, memory.Episode{Agent: "a", Summary: "no vec", Outcome: memory.OutcomeSuccess})
	_, _ = store.CreateEpisode(ctx, memory.Episode{
		Agent: "b", Summary: "has vec", Embedding: []float32{0.1}, Outcome: memory.OutcomeSuccess,
	})

	episodes, err := store.EpisodesWithEmbeddings(ctx)

	require.NoError(t, err)
	assert.Len(t, episodes, 1)
	assert.Equal(t, "has vec", episodes[0].Summary)
}
