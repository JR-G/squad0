package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildSeanceContextFull_IncludesHandoffs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	episodeDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = episodeDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	episodeStore := memory.NewEpisodeStore(episodeDB)
	handoffStore := pipeline.NewHandoffStore(sqlDB)
	require.NoError(t, handoffStore.InitSchema(ctx))

	// Create a handoff for the ticket.
	_, err = handoffStore.Create(ctx, pipeline.Handoff{
		Ticket:  "JAM-42",
		Agent:   "engineer-2",
		Status:  "partial",
		Summary: "Auth token refresh was flaky",
	})
	require.NoError(t, err)

	result := orchestrator.BuildSeanceContextFull(ctx, episodeStore, nil, handoffStore, "JAM-42", agent.RoleEngineer1)

	assert.Contains(t, result, "Previous Work")
	assert.Contains(t, result, "Session Handoffs")
	assert.Contains(t, result, "Auth token refresh")
}

func TestBuildSeanceContextFull_IncludesCrossAgentBeliefs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	episodeDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = episodeDB.Close() })

	agentDB1, db1Err := memory.Open(ctx, ":memory:")
	require.NoError(t, db1Err)
	t.Cleanup(func() { _ = agentDB1.Close() })

	agentDB2, db2Err := memory.Open(ctx, ":memory:")
	require.NoError(t, db2Err)
	t.Cleanup(func() { _ = agentDB2.Close() })

	episodeStore := memory.NewEpisodeStore(episodeDB)
	factStore1 := memory.NewFactStore(agentDB1)
	factStore2 := memory.NewFactStore(agentDB2)

	// Engineer-2 has a belief mentioning this ticket.
	_, _ = factStore2.CreateBelief(ctx, memory.Belief{
		Content:    "JAM-42 requires careful error handling in the auth module",
		Confidence: 0.7,
	})

	agentFactStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: factStore1,
		agent.RoleEngineer2: factStore2,
	}

	result := orchestrator.BuildSeanceContextFull(ctx, episodeStore, agentFactStores, nil, "JAM-42", agent.RoleEngineer1)

	assert.Contains(t, result, "Previous Work")
	assert.Contains(t, result, "Beliefs from Other Engineers")
	assert.Contains(t, result, "error handling")
}

func TestBuildSeanceContextFull_ExcludesOwnBeliefs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	episodeDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = episodeDB.Close() })

	agentDB1, db1Err := memory.Open(ctx, ":memory:")
	require.NoError(t, db1Err)
	t.Cleanup(func() { _ = agentDB1.Close() })

	episodeStore := memory.NewEpisodeStore(episodeDB)
	factStore1 := memory.NewFactStore(agentDB1)

	// Engineer-1's own belief should be excluded.
	_, _ = factStore1.CreateBelief(ctx, memory.Belief{
		Content:    "JAM-42 is tricky",
		Confidence: 0.8,
	})

	agentFactStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: factStore1,
	}

	result := orchestrator.BuildSeanceContextFull(ctx, episodeStore, agentFactStores, nil, "JAM-42", agent.RoleEngineer1)

	// Own beliefs excluded, no episodes, no handoffs => empty.
	assert.Empty(t, result)
}

func TestBuildSeanceContextFull_NilStores_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	result := orchestrator.BuildSeanceContextFull(
		context.Background(), nil, nil, nil, "JAM-42", agent.RoleEngineer1,
	)

	assert.Empty(t, result)
}

func TestBuildSeanceContextFull_CombinesAllSources(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	episodeDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = episodeDB.Close() })

	agentDB, dbErr := memory.Open(ctx, ":memory:")
	require.NoError(t, dbErr)
	t.Cleanup(func() { _ = agentDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	episodeStore := memory.NewEpisodeStore(episodeDB)
	factStore := memory.NewFactStore(agentDB)
	handoffStore := pipeline.NewHandoffStore(sqlDB)
	require.NoError(t, handoffStore.InitSchema(ctx))

	// Episode from engineer-2.
	_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent:   string(agent.RoleEngineer2),
		Ticket:  "JAM-42",
		Summary: "Partial progress on auth",
		Outcome: memory.OutcomePartial,
	})

	// Handoff.
	_, _ = handoffStore.Create(ctx, pipeline.Handoff{
		Ticket: "JAM-42", Agent: "engineer-2", Status: "partial", Summary: "Left off at token refresh",
	})

	// Belief from engineer-2.
	_, _ = factStore.CreateBelief(ctx, memory.Belief{
		Content:    "JAM-42 token refresh needs exponential backoff",
		Confidence: 0.7,
	})

	agentFactStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer2: factStore,
	}

	result := orchestrator.BuildSeanceContextFull(ctx, episodeStore, agentFactStores, handoffStore, "JAM-42", agent.RoleEngineer1)

	assert.Contains(t, result, "Previous Work")
	assert.Contains(t, result, "Partial progress on auth")
	assert.Contains(t, result, "Session Handoffs")
	assert.Contains(t, result, "Left off at token refresh")
	assert.Contains(t, result, "Beliefs from Other Engineers")
	assert.Contains(t, result, "exponential backoff")
}

func TestHandoffStore_AllForTicket_ReturnsAll(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewHandoffStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	_, _ = store.Create(ctx, pipeline.Handoff{
		Ticket: "JAM-42", Agent: "engineer-1", Status: "failed", Summary: "first attempt",
	})
	_, _ = store.Create(ctx, pipeline.Handoff{
		Ticket: "JAM-42", Agent: "engineer-2", Status: "completed", Summary: "second attempt",
	})
	_, _ = store.Create(ctx, pipeline.Handoff{
		Ticket: "JAM-99", Agent: "engineer-1", Status: "completed", Summary: "other ticket",
	})

	handoffs, err := store.AllForTicket(ctx, "JAM-42")

	require.NoError(t, err)
	assert.Len(t, handoffs, 2)
	// Most recent first.
	assert.Equal(t, "second attempt", handoffs[0].Summary)
	assert.Equal(t, "first attempt", handoffs[1].Summary)
}

func TestHandoffStore_AllForTicket_NoHandoffs_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewHandoffStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	handoffs, err := store.AllForTicket(ctx, "NONEXISTENT")

	require.NoError(t, err)
	assert.Empty(t, handoffs)
}
