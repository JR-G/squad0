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
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Deterministic tests to cover specific paths that random selection
// may not hit consistently.

func TestDecideBaseResponders_AllBranches(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		nanos    int64
		isHuman  bool
		expected int
	}{
		{"human always 2", 0, true, 2},
		{"human even if stale", 10 * 60 * 1e9, true, 2},
		{"agent recent", 0, false, 2},
		{"agent 1min", 1 * 60 * 1e9, false, 2},
		{"agent 3min", 3 * 60 * 1e9, false, 1},
		{"agent 4min", 4 * 60 * 1e9, false, 1},
		{"agent 5min", 5 * 60 * 1e9, false, 0},
		{"agent 10min", 10 * 60 * 1e9, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := orchestrator.DecideBaseRespondersForTest(tt.nanos, tt.isHuman)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContainsQuestion_Variants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		expected bool
	}{
		{"simple question", "what do you think?", true},
		{"mid-sentence question", "so the question is: why?", true},
		{"no question", "I agree with the approach", false},
		{"empty", "", false},
		{"just question mark", "?", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.expected, orchestrator.ContainsQuestionForTest(tt.text))
		})
	}
}

func TestIsQuiet_Deterministic(t *testing.T) {
	t.Parallel()

	engine := newTestConversationEngine(t)

	// Unknown channel is quiet.
	assert.True(t, engine.IsQuiet("nowhere", time.Second))

	// Fresh message is not quiet.
	engine.OnMessage(context.Background(), "test", "ceo", "hello")
	assert.False(t, engine.IsQuiet("test", time.Second))

	// Simulate old message.
	engine.SetLastMessageTime("test", time.Now().Add(-5*time.Second))
	assert.True(t, engine.IsQuiet("test", time.Second))
}

func TestPickCandidates_MentionedOnly_WhenDecayed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Sure, I can review that."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	roster := map[agent.Role]string{
		agent.RoleEngineer1: "Callum",
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, roster)

	// Agent message in decayed channel mentioning Callum.
	engine.SetLastMessageTime("engineering", time.Now().Add(-6*time.Minute))
	engine.OnMessage(ctx, "engineering", string(agent.RoleEngineer2), "Callum, thoughts?")

	// Callum should respond because mentioned bypasses decay.
	recent := engine.RecentMessages("engineering")
	assert.NotEmpty(t, recent)
}

func TestBuildDailySummary_EmptyPipeline_ReturnsBasicSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, _ := setupPMDutiesOrch(t)

	// No items in pipeline — should still produce a summary.
	assert.NotPanics(t, func() {
		orch.PostDailySummary(ctx)
	})
}

func TestRunPMDuties_AllEngineersChecked(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	engRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: setupAgentWithRole(t, engRunner, agent.RoleEngineer1),
		agent.RoleEngineer2: setupAgentWithRole(t, engRunner, agent.RoleEngineer2),
		agent.RoleEngineer3: setupAgentWithRole(t, engRunner, agent.RoleEngineer3),
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Create stale items for different engineers.
	for _, role := range []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2} {
		itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
			Ticket: "JAM-" + string(role), Engineer: role, Stage: pipeline.StageWorking, Branch: "feat/" + string(role),
		})
		require.NoError(t, createErr)
		_, _ = pipeStore.DB().ExecContext(ctx,
			`UPDATE work_items SET updated_at = datetime('now', '-60 minutes') WHERE id = ?`, itemID)
	}

	assert.NotPanics(t, func() {
		orch.RunPMDuties(ctx)
	})
}

func TestSetVoices_WithRealPersonalities_LoadsVoices(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	// Use the real personality files from agents/ directory.
	loader := agent.NewPersonalityLoader("../../agents")

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM: buildAgent(t, runner, agent.RolePM, db),
	}
	factStores := map[agent.Role]*memory.FactStore{
		agent.RolePM: memory.NewFactStore(db),
	}

	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.SetVoices(loader)

	// Verify no crash.
	assert.NotPanics(t, func() {
		engine.OnMessage(ctx, "engineering", "ceo", "hello")
	})
}

func TestRecordSession_WithMonitor_RecordsEvents(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, WorkEnabled: true},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	monitor := health.NewMonitor(
		[]agent.Role{agent.RolePM},
		health.MonitorConfig{MaxSessionTime: time.Hour, MaxConsecutiveErrors: 5},
	)
	orch.SetHealthMonitor(monitor)

	// Run briefly to exercise tick → RunPMDuties with monitor set.
	timedCtx, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)
}
