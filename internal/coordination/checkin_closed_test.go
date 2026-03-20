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

func setupClosedStore(t *testing.T) *coordination.CheckInStore {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)

	store := coordination.NewCheckInStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	require.NoError(t, db.Close())

	return store
}

func TestCheckInStore_GetAll_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	store := setupClosedStore(t)

	_, err := store.GetAll(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "querying all checkins")
}

func TestCheckInStore_IdleAgents_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	store := setupClosedStore(t)

	_, err := store.IdleAgents(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "querying idle agents")
}

func TestCheckInStore_Upsert_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	store := setupClosedStore(t)

	err := store.Upsert(context.Background(), coordination.CheckIn{
		Agent:         agent.RoleEngineer1,
		Status:        coordination.StatusWorking,
		FilesTouching: []string{"a.go"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "upserting checkin")
}

func TestCheckInStore_GetByAgent_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	store := setupClosedStore(t)

	_, err := store.GetByAgent(context.Background(), agent.RoleEngineer1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "getting checkin")
}

func TestCheckInStore_SetIdle_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	store := setupClosedStore(t)

	err := store.SetIdle(context.Background(), agent.RoleEngineer1)

	require.Error(t, err)
}

func TestCheckInStore_InitSchema_ClosedDB_ReturnsError(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	require.NoError(t, db.Close())

	store := coordination.NewCheckInStore(db)
	err = store.InitSchema(context.Background())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "creating checkin table")
}

func TestCheckInStore_GetAll_ScanError_InvalidJSON(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := coordination.NewCheckInStore(db)
	require.NoError(t, store.InitSchema(context.Background()))

	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO checkin (agent, ticket, status, files_touching, message, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		"engineer-1", "SQ-1", "working", "not-valid-json", "working",
	)
	require.NoError(t, err)

	_, err = store.GetAll(ctx)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshalling files")
}

func TestCheckInStore_GetAll_Empty_ReturnsNil(t *testing.T) {
	t.Parallel()

	store := setupStore(t)

	checkIns, err := store.GetAll(context.Background())

	require.NoError(t, err)
	assert.Nil(t, checkIns)
}

func TestCheckInStore_IdleAgents_Empty_ReturnsNil(t *testing.T) {
	t.Parallel()

	store := setupStore(t)

	roles, err := store.IdleAgents(context.Background())

	require.NoError(t, err)
	assert.Nil(t, roles)
}

func TestCheckInStore_IdleAgents_NoIdle_ReturnsNil(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	require.NoError(t, store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	}))
	require.NoError(t, store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer2, Status: coordination.StatusBlocked, FilesTouching: []string{},
	}))

	roles, err := store.IdleAgents(ctx)

	require.NoError(t, err)
	assert.Nil(t, roles)
}

func TestCheckInStore_GetByAgent_InvalidFilesJSON_ReturnsError(t *testing.T) {
	t.Parallel()

	db := openTestDB(t)
	store := coordination.NewCheckInStore(db)
	require.NoError(t, store.InitSchema(context.Background()))

	ctx := context.Background()

	_, err := db.ExecContext(ctx, `
		INSERT INTO checkin (agent, ticket, status, files_touching, message, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		"engineer-1", "SQ-1", "working", "{bad-json", "working",
	)
	require.NoError(t, err)

	_, err = store.GetByAgent(ctx, agent.RoleEngineer1)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshalling files")
}
