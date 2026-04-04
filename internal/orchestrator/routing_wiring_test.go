package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/routing"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// SetSpecialisationStore / SetOpinionStore / SetTokenLedger /
// SetComplexityClassifier — all at 0% coverage
// ---------------------------------------------------------------------------

func TestSetSpecialisationStore_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	specStore := routing.NewSpecialisationStore(sqlDB)

	assert.NotPanics(t, func() {
		orch.SetSpecialisationStore(specStore)
	})
}

func TestSetOpinionStore_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	factStores := map[agent.Role]*memory.FactStore{
		agent.RoleEngineer1: memory.NewFactStore(memDB),
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	opinionStore := routing.NewOpinionStore(factStores)

	assert.NotPanics(t, func() {
		orch.SetOpinionStore(opinionStore)
	})
}

func TestSetTokenLedger_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	ledger := routing.NewTokenLedger(100000, 500000)

	assert.NotPanics(t, func() {
		orch.SetTokenLedger(ledger)
	})
}

func TestSetComplexityClassifier_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	classifier := routing.NewComplexityClassifier("haiku", "sonnet", "opus")

	assert.NotPanics(t, func() {
		orch.SetComplexityClassifier(classifier)
	})
}

// ---------------------------------------------------------------------------
// NameForRole — branch coverage
// ---------------------------------------------------------------------------

func TestNameForRole_NilRoster_ReturnsRoleString(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	// No roster set — should return the role string.
	assert.Equal(t, string(agent.RoleEngineer1), orch.NameForRole(agent.RoleEngineer1))
}

func TestNameForRole_WithRoster_ReturnsName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	orch.SetRoster(map[agent.Role]string{
		agent.RoleEngineer1: "Mara",
		agent.RoleEngineer2: string(agent.RoleEngineer2), // Same as role ID.
	})

	assert.Equal(t, "Mara", orch.NameForRole(agent.RoleEngineer1))
	// Name same as role ID — treated as unset, returns role string.
	assert.Equal(t, string(agent.RoleEngineer2), orch.NameForRole(agent.RoleEngineer2))
	// Missing from roster — returns role string.
	assert.Equal(t, string(agent.RoleEngineer3), orch.NameForRole(agent.RoleEngineer3))
}
