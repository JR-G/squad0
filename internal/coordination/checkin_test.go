package coordination_test

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL&_foreign_keys=ON")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func setupStore(t *testing.T) *coordination.CheckInStore {
	t.Helper()
	db := openTestDB(t)
	store := coordination.NewCheckInStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	return store
}

func TestCheckInStore_InitSchema_CreatesTable(t *testing.T) {
	t.Parallel()
	store := setupStore(t)
	_ = store
}

func TestCheckInStore_Upsert_CreatesNew(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	err := store.Upsert(ctx, coordination.CheckIn{
		Agent:         agent.RoleEngineer1,
		Ticket:        "SQ-42",
		Status:        coordination.StatusWorking,
		FilesTouching: []string{"main.go", "handler.go"},
		Message:       "implementing auth",
	})

	require.NoError(t, err)

	checkIn, err := store.GetByAgent(ctx, agent.RoleEngineer1)
	require.NoError(t, err)
	assert.Equal(t, agent.RoleEngineer1, checkIn.Agent)
	assert.Equal(t, "SQ-42", checkIn.Ticket)
	assert.Equal(t, coordination.StatusWorking, checkIn.Status)
	assert.Equal(t, []string{"main.go", "handler.go"}, checkIn.FilesTouching)
}

func TestCheckInStore_Upsert_UpdatesExisting(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Ticket: "SQ-1", Status: coordination.StatusWorking, FilesTouching: []string{},
	})

	err := store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Ticket: "SQ-2", Status: coordination.StatusBlocked, FilesTouching: []string{"new.go"},
	})

	require.NoError(t, err)

	checkIn, err := store.GetByAgent(ctx, agent.RoleEngineer1)
	require.NoError(t, err)
	assert.Equal(t, "SQ-2", checkIn.Ticket)
	assert.Equal(t, coordination.StatusBlocked, checkIn.Status)
}

func TestCheckInStore_Upsert_CancelledContext_ReturnsContextErr(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusIdle, FilesTouching: []string{},
	})

	require.ErrorIs(t, err, context.Canceled)
}

func TestCheckInStore_Upsert_ConcurrentWriters_NoErrors(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	const writers = 8
	const writes = 25
	errCh := make(chan error, writers*writes)

	var wg sync.WaitGroup
	for writer := range writers {
		wg.Add(1)
		go func(role agent.Role, writerIdx int) {
			defer wg.Done()
			for j := range writes {
				ticket := fmt.Sprintf("SQ-%d-%d", writerIdx, j)
				errCh <- store.Upsert(ctx, coordination.CheckIn{
					Agent: role, Ticket: ticket, Status: coordination.StatusWorking, FilesTouching: []string{},
				})
			}
		}(agent.AllRoles()[writer%len(agent.AllRoles())], writer)
	}
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

func TestCheckInStore_GetByAgent_NotFound_ReturnsError(t *testing.T) {
	t.Parallel()

	store := setupStore(t)

	_, err := store.GetByAgent(context.Background(), agent.RoleDesigner)

	require.Error(t, err)
}

func TestCheckInStore_GetAll_ReturnsAllCheckIns(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, coordination.CheckIn{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{}})
	_ = store.Upsert(ctx, coordination.CheckIn{Agent: agent.RoleEngineer2, Status: coordination.StatusIdle, FilesTouching: []string{}})

	checkIns, err := store.GetAll(ctx)

	require.NoError(t, err)
	assert.Len(t, checkIns, 2)
}

func TestCheckInStore_IdleAgents_ReturnsOnlyIdle(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, coordination.CheckIn{Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{}})
	_ = store.Upsert(ctx, coordination.CheckIn{Agent: agent.RoleEngineer2, Status: coordination.StatusIdle, FilesTouching: []string{}})
	_ = store.Upsert(ctx, coordination.CheckIn{Agent: agent.RoleEngineer3, Status: coordination.StatusIdle, FilesTouching: []string{}})

	idle, err := store.IdleAgents(ctx)

	require.NoError(t, err)
	assert.Len(t, idle, 2)
}

func TestCheckInStore_SetIdle_ClearsTicketAndFiles(t *testing.T) {
	t.Parallel()

	store := setupStore(t)
	ctx := context.Background()

	_ = store.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Ticket: "SQ-42", Status: coordination.StatusWorking,
		FilesTouching: []string{"a.go", "b.go"}, Message: "busy",
	})

	err := store.SetIdle(ctx, agent.RoleEngineer1)
	require.NoError(t, err)

	checkIn, err := store.GetByAgent(ctx, agent.RoleEngineer1)
	require.NoError(t, err)
	assert.Equal(t, coordination.StatusIdle, checkIn.Status)
	assert.Empty(t, checkIn.Ticket)
	assert.Empty(t, checkIn.FilesTouching)
}
