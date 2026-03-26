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

func TestBuildSeanceContextFull_NilHandoffStore_ReturnsBeliefs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	episodeDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = episodeDB.Close() })

	agentDB, dbErr := memory.Open(ctx, ":memory:")
	require.NoError(t, dbErr)
	t.Cleanup(func() { _ = agentDB.Close() })

	episodeStore := memory.NewEpisodeStore(episodeDB)
	factStore := memory.NewFactStore(agentDB)

	// Engineer-2 has a belief mentioning this ticket.
	_, _ = factStore.CreateBelief(ctx, memory.Belief{
		Content:    "TASK-99 needs careful null checks in the handler",
		Confidence: 0.7,
	})

	agentFactStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer2: factStore,
	}

	// Nil handoff store should not prevent beliefs from appearing.
	result := orchestrator.BuildSeanceContextFull(
		ctx, episodeStore, agentFactStores, nil, "TASK-99", agent.RoleEngineer1,
	)

	assert.Contains(t, result, "Previous Work")
	assert.Contains(t, result, "Beliefs from Other Engineers")
	assert.Contains(t, result, "null checks")
}

func TestBuildSeanceContextFull_NilFactStores_ReturnsEpisodes(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	episodeDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = episodeDB.Close() })

	episodeStore := memory.NewEpisodeStore(episodeDB)

	// Engineer-2 previously worked on this ticket.
	_, _ = episodeStore.CreateEpisode(ctx, memory.Episode{
		Agent:   string(agent.RoleEngineer2),
		Ticket:  "TASK-77",
		Summary: "Partial progress on validation",
		Outcome: memory.OutcomePartial,
	})

	// Nil fact stores should not prevent episodes from appearing.
	result := orchestrator.BuildSeanceContextFull(
		ctx, episodeStore, nil, nil, "TASK-77", agent.RoleEngineer1,
	)

	assert.Contains(t, result, "Previous Work")
	assert.Contains(t, result, "Partial progress on validation")
}

func TestBuildSeanceContextFull_EmptyFactStores_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	episodeDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = episodeDB.Close() })

	episodeStore := memory.NewEpisodeStore(episodeDB)

	// Empty agent fact stores map, no episodes, no handoffs.
	agentFactStores := map[agent.Role]*memory.FactStore{}

	result := orchestrator.BuildSeanceContextFull(
		ctx, episodeStore, agentFactStores, nil, "TASK-55", agent.RoleEngineer1,
	)

	assert.Empty(t, result)
}

func TestBuildSeanceContextFull_OnlyHandoffs_IncludesHandoffSection(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	episodeDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = episodeDB.Close() })

	episodeStore := memory.NewEpisodeStore(episodeDB)

	// Nil handoff store, nil fact stores, no episodes.
	result := orchestrator.BuildSeanceContextFull(
		ctx, episodeStore, nil, nil, "TASK-33", agent.RoleEngineer1,
	)

	assert.Empty(t, result, "no sources populated means empty result")
}

func TestBuildSeanceContextFull_HandoffWithBlockers_IncludesBlockers(t *testing.T) {
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

	// Create a handoff with blockers populated.
	_, handoffErr := handoffStore.Create(ctx, pipeline.Handoff{
		Ticket:   "TASK-44",
		Agent:    "engineer-2",
		Status:   "blocked",
		Summary:  "Stuck on database migration",
		Blockers: "Migration script fails on nullable columns",
	})
	require.NoError(t, handoffErr)

	result := orchestrator.BuildSeanceContextFull(
		ctx, episodeStore, nil, handoffStore, "TASK-44", agent.RoleEngineer1,
	)

	assert.Contains(t, result, "Previous Work")
	assert.Contains(t, result, "Session Handoffs")
	assert.Contains(t, result, "Stuck on database migration")
	assert.Contains(t, result, "Blockers: Migration script fails on nullable columns")
}
