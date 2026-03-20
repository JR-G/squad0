package memory_test

import (
	"context"
	"testing"

	"github.com/JR-G/squad0/internal/memory"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFTSStore_SearchFacts_FindsMatchingContent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "payments"})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "Stripe webhook retries need exponential backoff",
		Type: memory.FactWarning, Confidence: 0.8,
	})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "Payment amounts are in cents",
		Type: memory.FactObservation, Confidence: 0.7,
	})

	results, err := ftsStore.SearchFacts(ctx, "webhook retries", 10)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "facts", results[0].Table)
	assert.Greater(t, results[0].Score, 0.0)
}

func TestFTSStore_SearchEpisodes_FindsMatchingSummary(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	episodeStore := memory.NewEpisodeStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-1", Ticket: "SQ-42", Summary: "Implemented retry logic for failed payments",
		Outcome: memory.OutcomeSuccess,
	})
	_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent: "engineer-2", Ticket: "SQ-43", Summary: "Added user profile page",
		Outcome: memory.OutcomeSuccess,
	})

	results, err := ftsStore.SearchEpisodes(ctx, "retry payments", 10)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "episodes", results[0].Table)
}

func TestFTSStore_SearchBeliefs_FindsMatchingContent(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	factStore := memory.NewFactStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "table-driven tests are cleaner", Confidence: 0.8})
	_, _ = factStore.CreateBelief(ctx, memory.Belief{Content: "dependency injection improves testability", Confidence: 0.7})

	results, err := ftsStore.SearchBeliefs(ctx, "tests cleaner", 10)

	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "beliefs", results[0].Table)
}

func TestFTSStore_SearchFacts_EmptyQuery_ReturnsNil(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)

	results, err := ftsStore.SearchFacts(context.Background(), "", 10)

	require.NoError(t, err)
	assert.Nil(t, results)
}

func TestFTSStore_SearchFacts_NoMatches_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	graphStore := memory.NewGraphStore(db)
	factStore := memory.NewFactStore(db)
	ftsStore := memory.NewFTSStore(db)
	ctx := context.Background()

	entityID, _ := graphStore.CreateEntity(ctx, memory.Entity{Type: memory.EntityModule, Name: "x"})
	_, _ = factStore.CreateFact(ctx, memory.Fact{
		EntityID: entityID, Content: "something unrelated", Type: memory.FactObservation, Confidence: 0.5,
	})

	results, err := ftsStore.SearchFacts(ctx, "nonexistent term xyz", 10)

	require.NoError(t, err)
	assert.Empty(t, results)
}

func TestFTSStore_SearchFacts_SpecialCharacters_DoesNotPanic(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	ftsStore := memory.NewFTSStore(db)

	results, err := ftsStore.SearchFacts(context.Background(), `"quoted" (parens) *star* 'apos'`, 10)

	require.NoError(t, err)
	assert.Empty(t, results)
}
