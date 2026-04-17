package cli_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/cmd/squad0/cli"
	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupBusyCheckerStore(t *testing.T) *coordination.CheckInStore {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	store := coordination.NewCheckInStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	return store
}

func TestBusyCheckerFromCheckIns_NoCheckIn_ReturnsFalse(t *testing.T) {
	t.Parallel()

	check := cli.BusyCheckerFromCheckIns(setupBusyCheckerStore(t))

	assert.False(t, check(context.Background(), agent.RoleEngineer1))
}

func TestBusyCheckerFromCheckIns_WorkingStatus_ReturnsTrue(t *testing.T) {
	t.Parallel()

	store := setupBusyCheckerStore(t)
	require.NoError(t, store.Upsert(context.Background(), coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	}))

	check := cli.BusyCheckerFromCheckIns(store)

	assert.True(t, check(context.Background(), agent.RoleEngineer1))
}

func TestBusyCheckerFromCheckIns_IdleStatus_ReturnsFalse(t *testing.T) {
	t.Parallel()

	store := setupBusyCheckerStore(t)
	require.NoError(t, store.Upsert(context.Background(), coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusIdle, FilesTouching: []string{},
	}))

	check := cli.BusyCheckerFromCheckIns(store)

	assert.False(t, check(context.Background(), agent.RoleEngineer1))
}

func TestBusyCheckerFromCheckIns_ReviewingStatus_ReturnsFalse(t *testing.T) {
	t.Parallel()

	// Only the working state counts as "busy". Reviewing is busy in
	// pipeline terms but not for chat-routing purposes.
	store := setupBusyCheckerStore(t)
	require.NoError(t, store.Upsert(context.Background(), coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusReviewing, FilesTouching: []string{},
	}))

	check := cli.BusyCheckerFromCheckIns(store)

	assert.False(t, check(context.Background(), agent.RoleEngineer1))
}
