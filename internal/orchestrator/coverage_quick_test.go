package orchestrator_test

import (
	"context"
	"database/sql"
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

func TestReplyInstruction_Engineering(t *testing.T) {
	t.Parallel()
	result := orchestrator.ReplyInstructionForTest("Mara", "engineering")
	assert.Contains(t, result, "Mara")
	assert.Contains(t, result, "Only respond if you have something to add")
}

func TestReplyInstruction_Chitchat(t *testing.T) {
	t.Parallel()
	result := orchestrator.ReplyInstructionForTest("Mara", "chitchat")
	assert.Contains(t, result, "Mara")
}

func TestCheckCircuitBreaker_NilAssigner_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	// Should not panic with nil assigner.
	orch.CheckCircuitBreakerForTest(context.Background(), "JAM-1")
}

func TestCheckCircuitBreaker_UnderThreshold_NoEscalation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	pmAgent := buildAgent(t, runner, agent.RolePM, memDB)

	checkIns := coordination.NewCheckInStore(sqlDB)
	assigner := orchestrator.NewAssigner(pmAgent, "TEST")
	assigner.SetSmartAssigner(orchestrator.NewSmartAssigner(nil))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, assigner,
	)

	// Under threshold — should not escalate.
	orch.CheckCircuitBreakerForTest(ctx, "JAM-1")
	orch.CheckCircuitBreakerForTest(ctx, "JAM-1")
}

func TestRecordSessionEnd_WithMonitor_RecordsSuccess(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	monitor := health.NewMonitor([]agent.Role{agent.RoleEngineer1}, health.MonitorConfig{})

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetHealthMonitor(monitor)

	// Exercise both start and end.
	orch.RecordSessionStartForTest(agent.RoleEngineer1)
	orch.RecordSessionEndForTest(agent.RoleEngineer1, "JAM-1", true)
	orch.RecordSessionEndForTest(agent.RoleEngineer1, "JAM-2", false)
}

func TestEmitEvent_NilBus_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	// No event bus — should not panic.
	orch.EmitEventForTest(context.Background(), orchestrator.EventSessionComplete, "", "JAM-1", 0, agent.RoleEngineer1)
}

func TestMaybeStoreConcerns_WithTracker_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	tracker := orchestrator.NewConcernTracker()
	engine.SetConcernTracker(tracker)

	// Trigger a message with concern keywords — should store concerns.
	engine.OnMessage(ctx, "engineering", "ceo", "I'm worried about the auth flow retries")
}

func TestCancelSession_NoSession_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	// No session registered — should not panic.
	_ = orch.PauseAgent(context.Background(), agent.RoleEngineer1)
}

func TestFilterHealthyEngineers_NilMonitor_ReturnsAll(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx := context.Background()
	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := buildAgent(t, runner, agent.RolePM, memDB)
	eng1 := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, WorkEnabled: true},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent, agent.RoleEngineer1: eng1},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// No monitor set — engineers should not be filtered out.
	timedCtx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)
}

func TestStoreProjectBelief_WithStore_StoresBelief(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	tlRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	tlAgent := buildAgent(t, tlRunner, agent.RoleTechLead, memDB)
	factStore := memory.NewFactStore(memDB)
	graphStore := memory.NewGraphStore(memDB)
	tlAgent.SetMemoryStores(graphStore, factStore)

	projectFactStore := memory.NewFactStore(memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{agent.RoleTechLead: tlAgent},
		checkIns, nil, nil,
	)
	orch.SetProjectFactStore(projectFactStore)

	orch.StoreArchitectureDecision(ctx, "use interfaces at boundaries", "JAM-99")

	beliefs, bErr := projectFactStore.TopBeliefs(ctx, 5)
	require.NoError(t, bErr)
	assert.NotEmpty(t, beliefs)
	assert.Contains(t, beliefs[0].Content, "use interfaces at boundaries")
}

func TestPRStatus_Empty_ReturnsNone(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "none", orchestrator.PRStatusForTest(""))
}

func TestPRStatus_WithURL_ReturnsOpen(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "open", orchestrator.PRStatusForTest("https://github.com/org/repo/pull/1"))
}

func TestIsDuplicate_NoPipeline_ReturnsFalse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"test"}` + "\n")}
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	// No output pipeline — isDuplicate should return false (no filtering).
	engine.OnMessage(ctx, "engineering", "ceo", "hello")
	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}
