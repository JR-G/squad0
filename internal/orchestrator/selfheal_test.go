package orchestrator_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/health"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSetHealthMonitor_SetsMonitor(t *testing.T) {
	t.Parallel()

	orch, _ := setupLifecycleOrch(t)
	monitor := health.NewMonitor(
		[]agent.Role{agent.RolePM},
		health.MonitorConfig{MaxConsecutiveErrors: 3},
	)

	assert.NotPanics(t, func() {
		orch.SetHealthMonitor(monitor)
	})
}

func TestSetHealthMonitor_FiltersFailingAgents(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	eng1Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}

	pmAgent := setupPMAgent(t, pmRunner)
	eng1Agent := setupAgentWithRole(t, eng1Runner, agent.RoleEngineer1)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1Agent,
	}

	monitor := health.NewMonitor(
		[]agent.Role{agent.RolePM, agent.RoleEngineer1},
		health.MonitorConfig{MaxSessionTime: time.Hour, MaxConsecutiveErrors: 2},
	)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second, WorkEnabled: true},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetHealthMonitor(monitor)

	// Record enough errors to put engineer-1 into failing state.
	monitor.RecordSessionStart(agent.RoleEngineer1)
	monitor.RecordError(agent.RoleEngineer1, "crash")
	monitor.RecordError(agent.RoleEngineer1, "crash")
	monitor.RecordError(agent.RoleEngineer1, "crash")
	monitor.Evaluate()

	// Run orchestrator briefly — engineer-1 should be skipped.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	// PM should NOT have been asked for assignments since the only
	// engineer is in a failing state.
	assert.LessOrEqual(t, len(pmRunner.calls), 1)
}

func TestOrchestrator_WithMonitor_RecordsHealthEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	monitor := health.NewMonitor(
		[]agent.Role{agent.RolePM},
		health.MonitorConfig{MaxSessionTime: time.Hour, MaxConsecutiveErrors: 5},
	)

	// Just verify the methods don't panic with a monitor set.
	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetHealthMonitor(monitor)

	timedCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)

	allHealth := monitor.AllHealth(ctx)
	assert.NotNil(t, allHealth)
}

func TestUpdateRoster_RefreshesConversationNames(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Use a single runner shared by all agents — we check stdin content.
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Hey!"}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	newRoster := map[agent.Role]string{
		agent.RoleEngineer1: "Spark",
		agent.RoleEngineer2: "Nova",
	}
	engine.UpdateRoster(newRoster)

	engine.OnMessage(ctx, "engineering", "ceo", "hello")

	foundRoster := false
	for _, call := range runner.calls {
		if strings.Contains(call.stdin, "Spark") || strings.Contains(call.stdin, "Nova") {
			foundRoster = true
			break
		}
	}
	assert.True(t, foundRoster, "expected prompts to include updated roster names")
}
