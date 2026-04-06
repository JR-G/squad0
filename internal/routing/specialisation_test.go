package routing_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/routing"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newSpecStore(t *testing.T) *routing.SpecialisationStore {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := routing.NewSpecialisationStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	return store
}

func TestSpecialisationStore_Record_Success(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSpecStore(t)

	require.NoError(t, store.Record(ctx, agent.RoleEngineer1, "frontend", true))
	require.NoError(t, store.Record(ctx, agent.RoleEngineer1, "frontend", true))

	stats, err := store.StatsForRole(ctx, agent.RoleEngineer1)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, 2, stats[0].Wins)
	assert.Equal(t, 0, stats[0].Losses)
	assert.Equal(t, 1.0, stats[0].Score)
}

func TestSpecialisationStore_Record_Failure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSpecStore(t)

	require.NoError(t, store.Record(ctx, agent.RoleEngineer2, "backend", false))

	stats, err := store.StatsForRole(ctx, agent.RoleEngineer2)
	require.NoError(t, err)
	require.Len(t, stats, 1)
	assert.Equal(t, 0, stats[0].Wins)
	assert.Equal(t, 1, stats[0].Losses)
}

func TestSpecialisationStore_BestForCategory_SortsByScore(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSpecStore(t)

	// Engineer-1: 3 wins, 1 loss = 75%
	for range 3 {
		require.NoError(t, store.Record(ctx, agent.RoleEngineer1, "auth", true))
	}
	require.NoError(t, store.Record(ctx, agent.RoleEngineer1, "auth", false))

	// Engineer-2: 2 wins, 0 losses = 100%
	for range 2 {
		require.NoError(t, store.Record(ctx, agent.RoleEngineer2, "auth", true))
	}

	best, err := store.BestForCategory(ctx, "auth")
	require.NoError(t, err)
	require.Len(t, best, 2)
	assert.Equal(t, agent.RoleEngineer2, best[0].Role, "100%% should rank first")
	assert.Equal(t, agent.RoleEngineer1, best[1].Role, "75%% should rank second")
}

func TestSpecialisationStore_BestForCategory_MinimumAttempts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSpecStore(t)

	// Only 1 attempt — should not appear (minimum is 2).
	require.NoError(t, store.Record(ctx, agent.RoleEngineer1, "infra", true))

	best, err := store.BestForCategory(ctx, "infra")
	require.NoError(t, err)
	assert.Empty(t, best)
}

func TestSpecialisationStore_StatsForRole_MultipleCategories(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSpecStore(t)

	require.NoError(t, store.Record(ctx, agent.RoleEngineer3, "frontend", true))
	require.NoError(t, store.Record(ctx, agent.RoleEngineer3, "backend", false))

	stats, err := store.StatsForRole(ctx, agent.RoleEngineer3)
	require.NoError(t, err)
	assert.Len(t, stats, 2)
}

func TestSpecialisationStore_Record_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	store := routing.NewSpecialisationStore(db)
	require.NoError(t, store.InitSchema(ctx))
	_ = db.Close()

	assert.Error(t, store.Record(ctx, agent.RoleEngineer1, "test", true))
}

func TestSpecialisationStore_BestForCategory_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	store := routing.NewSpecialisationStore(db)
	require.NoError(t, store.InitSchema(ctx))
	_ = db.Close()

	_, qErr := store.BestForCategory(ctx, "test")
	assert.Error(t, qErr)
}

func TestSpecialisationStore_StatsForRole_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	store := routing.NewSpecialisationStore(db)
	require.NoError(t, store.InitSchema(ctx))
	_ = db.Close()

	_, qErr := store.StatsForRole(ctx, agent.RoleEngineer1)
	assert.Error(t, qErr)
}

func TestSpecialisationStore_InitSchema_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	_ = db.Close()

	store := routing.NewSpecialisationStore(db)
	assert.Error(t, store.InitSchema(context.Background()))
}

func TestSpecialisationStore_EmptyCategory_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := newSpecStore(t)

	best, err := store.BestForCategory(ctx, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, best)
}
