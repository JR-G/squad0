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

func TestPauseAll_WithRunningSessions_CancelsAll(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	eng1Agent := setupAgentWithRole(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}, agent.RoleEngineer1)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1Agent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// Set agents to working.
	for _, role := range []agent.Role{agent.RolePM, agent.RoleEngineer1} {
		_ = checkIns.Upsert(ctx, coordination.CheckIn{
			Agent: role, Status: coordination.StatusWorking, FilesTouching: []string{},
		})
	}

	// PauseAll cancels all sessions and sets all to paused.
	require.NoError(t, orch.PauseAll(ctx))

	allCheckIns, err := checkIns.GetAll(ctx)
	require.NoError(t, err)
	for _, checkIn := range allCheckIns {
		assert.Equal(t, coordination.StatusPaused, checkIn.Status, "agent %s should be paused", checkIn.Agent)
	}

	// ResumeAll brings everyone back.
	require.NoError(t, orch.ResumeAll(ctx))

	idleRoles, err := checkIns.IdleAgents(ctx)
	require.NoError(t, err)
	assert.Len(t, idleRoles, 2)
}

func TestAnnounceAsRole_NilBot_DoesNotPanic(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, _ := setupLifecycleOrch(t)
	_ = pmRunner

	assert.NotPanics(t, func() {
		orch.AnnounceForTest(context.Background(), "feed", "test", agent.RolePM)
	})
}

func TestSetRoster_And_NameForRole(t *testing.T) {
	t.Parallel()

	orch, _ := setupLifecycleOrch(t)

	assert.Equal(t, "engineer-1", orch.NameForRole(agent.RoleEngineer1))

	orch.SetRoster(map[agent.Role]string{
		agent.RoleEngineer1: "Callum",
		agent.RoleEngineer2: "Mara",
	})

	assert.Equal(t, "Callum", orch.NameForRole(agent.RoleEngineer1))
	assert.Equal(t, "Mara", orch.NameForRole(agent.RoleEngineer2))
	assert.Equal(t, "pm", orch.NameForRole(agent.RolePM))
}

func TestFilterPassResponse_EdgeCases(t *testing.T) {
	t.Parallel()

	assert.Empty(t, orchestrator.FilterPassResponseForTest("  PASS  "))
	assert.Empty(t, orchestrator.FilterPassResponseForTest("I'll pass on this one"))
	assert.NotEmpty(t, orchestrator.FilterPassResponseForTest("Let's discuss the approach"))
}

func TestCancelAllSessions_CancelsRegisteredContexts(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	eng1 := setupAgentWithRole(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}, agent.RoleEngineer1)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// Register real cancel functions to exercise cancelAllSessions loop.
	cancelled := false
	_, cancel := context.WithCancel(ctx)
	orch.RegisterCancelForTest(agent.RoleEngineer1, func() {
		cancelled = true
		cancel()
	})

	_ = checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RolePM, Status: coordination.StatusWorking, FilesTouching: []string{},
	})
	_ = checkIns.Upsert(ctx, coordination.CheckIn{
		Agent: agent.RoleEngineer1, Status: coordination.StatusWorking, FilesTouching: []string{},
	})

	require.NoError(t, orch.PauseAll(ctx))

	assert.True(t, cancelled, "cancel function should have been called")

	checkIn, _ := checkIns.GetByAgent(ctx, agent.RoleEngineer1)
	assert.Equal(t, coordination.StatusPaused, checkIn.Status)
}

func TestConversationEngine_PausedAgent_SkipsResponse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I should not respond."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	engine.SetPauseChecker(func(_ context.Context, _ agent.Role) bool {
		return true
	})

	engine.OnMessage(ctx, "engineering", "ceo", "anyone there?")

	assert.Empty(t, runner.calls, "paused agents should not make any Claude calls")
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
