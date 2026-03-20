package coordination_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckFileConflicts_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)

	store := coordination.NewCheckInStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	require.NoError(t, db.Close())

	_, err = coordination.CheckFileConflicts(
		context.Background(), store, agent.RoleEngineer2, []string{"handler.go"},
	)

	require.Error(t, err)
}

func TestCheckFileConflicts_EmptyPlannedFiles_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	require.NoError(t, store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking,
		FilesTouching: []string{"handler.go"},
	}))

	conflicts, err := coordination.CheckFileConflicts(ctx, store, agent.RoleEngineer2, []string{})

	require.NoError(t, err)
	assert.Empty(t, conflicts)
}

func TestCheckFileConflicts_NoCheckIns_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	store := setupStore(t)

	conflicts, err := coordination.CheckFileConflicts(
		context.Background(), store, agent.RoleEngineer1, []string{"handler.go"},
	)

	require.NoError(t, err)
	assert.Empty(t, conflicts)
}

func TestCheckFileConflicts_BlockedAgent_ReturnsConflict(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	require.NoError(t, store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusBlocked,
		FilesTouching: []string{"handler.go"},
	}))

	conflicts, err := coordination.CheckFileConflicts(
		ctx, store, agent.RoleEngineer2, []string{"handler.go"},
	)

	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	assert.Equal(t, "handler.go", conflicts[0].File)
}

func TestCheckFileConflicts_ReviewingAgent_ReturnsConflict(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	require.NoError(t, store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusReviewing,
		FilesTouching: []string{"handler.go"},
	}))

	conflicts, err := coordination.CheckFileConflicts(
		ctx, store, agent.RoleEngineer2, []string{"handler.go"},
	)

	require.NoError(t, err)
	require.Len(t, conflicts, 1)
	assert.Equal(t, agent.RoleEngineer1, conflicts[0].HeldBy)
}
