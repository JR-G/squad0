package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func closedTestDB(t *testing.T) *memory.DB {
	t.Helper()
	db := openTestDB(t)
	require.NoError(t, db.Close())
	return db
}

func TestGraphStore_CreateEntity_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewGraphStore(closedTestDB(t))
	_, err := store.CreateEntity(context.Background(), memory.Entity{Type: memory.EntityModule, Name: "x"})
	assert.Error(t, err)
}

func TestGraphStore_UpdateEntitySummary_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewGraphStore(closedTestDB(t))
	err := store.UpdateEntitySummary(context.Background(), 1, "new")
	assert.Error(t, err)
}

func TestGraphStore_CreateRelationship_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewGraphStore(closedTestDB(t))
	_, err := store.CreateRelationship(context.Background(), memory.Relationship{
		SourceID: 1, TargetID: 2, Type: memory.RelationDependsOn, Confidence: 0.5,
	})
	assert.Error(t, err)
}

func TestGraphStore_InvalidateRelationship_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewGraphStore(closedTestDB(t))
	err := store.InvalidateRelationship(context.Background(), 1)
	assert.Error(t, err)
}

func TestFactStore_CreateFact_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	_, err := store.CreateFact(context.Background(), memory.Fact{
		EntityID: 1, Content: "test", Type: memory.FactObservation, Confidence: 0.5,
	})
	assert.Error(t, err)
}

func TestFactStore_GetFact_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	_, err := store.GetFact(context.Background(), 1)
	assert.Error(t, err)
}

func TestFactStore_FactsByEntity_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	_, err := store.FactsByEntity(context.Background(), 1)
	assert.Error(t, err)
}

func TestFactStore_ConfirmFact_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	err := store.ConfirmFact(context.Background(), 1)
	assert.Error(t, err)
}

func TestFactStore_InvalidateFact_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	err := store.InvalidateFact(context.Background(), 1)
	assert.Error(t, err)
}

func TestFactStore_CreateBelief_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	_, err := store.CreateBelief(context.Background(), memory.Belief{Content: "test", Confidence: 0.5})
	assert.Error(t, err)
}

func TestFactStore_GetBelief_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	_, err := store.GetBelief(context.Background(), 1)
	assert.Error(t, err)
}

func TestFactStore_ConfirmBelief_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	err := store.ConfirmBelief(context.Background(), 1)
	assert.Error(t, err)
}

func TestFactStore_ContradictBelief_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	err := store.ContradictBelief(context.Background(), 1)
	assert.Error(t, err)
}

func TestFactStore_TopBeliefs_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFactStore(closedTestDB(t))
	_, err := store.TopBeliefs(context.Background(), 10)
	assert.Error(t, err)
}

func TestEpisodeStore_CreateEpisode_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewEpisodeStore(closedTestDB(t))
	_, err := store.CreateEpisode(context.Background(), memory.Episode{
		Agent: "test", Summary: "test", Outcome: memory.OutcomeSuccess,
	})
	assert.Error(t, err)
}

func TestEpisodeStore_GetEpisode_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewEpisodeStore(closedTestDB(t))
	_, err := store.GetEpisode(context.Background(), 1)
	assert.Error(t, err)
}

func TestEpisodeStore_EpisodesByAgent_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewEpisodeStore(closedTestDB(t))
	_, err := store.EpisodesByAgent(context.Background(), "test")
	assert.Error(t, err)
}

func TestEpisodeStore_RecentEpisodes_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewEpisodeStore(closedTestDB(t))
	_, err := store.RecentEpisodes(context.Background(), 10)
	assert.Error(t, err)
}

func TestEpisodeStore_UpdateEmbedding_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewEpisodeStore(closedTestDB(t))
	err := store.UpdateEmbedding(context.Background(), 1, []float32{0.1})
	assert.Error(t, err)
}

func TestEpisodeStore_EpisodesWithEmbeddings_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewEpisodeStore(closedTestDB(t))
	_, err := store.EpisodesWithEmbeddings(context.Background())
	assert.Error(t, err)
}

func TestEpisodeStore_EpisodesByTicket_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewEpisodeStore(closedTestDB(t))
	_, err := store.EpisodesByTicket(context.Background(), "JAM-1")
	assert.Error(t, err)
}

func TestFTSStore_SearchFacts_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFTSStore(closedTestDB(t))
	_, err := store.SearchFacts(context.Background(), "test", 10)
	assert.Error(t, err)
}

func TestFTSStore_SearchEpisodes_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFTSStore(closedTestDB(t))
	_, err := store.SearchEpisodes(context.Background(), "test", 10)
	assert.Error(t, err)
}

func TestFTSStore_SearchBeliefs_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewFTSStore(closedTestDB(t))
	_, err := store.SearchBeliefs(context.Background(), "test", 10)
	assert.Error(t, err)
}

func TestGraphStore_DirectNeighbours_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	store := memory.NewGraphStore(closedTestDB(t))
	_, err := store.RelatedEntities(context.Background(), 1, 1)
	assert.Error(t, err)
}
