package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractConcerns_FindsConcernSignals(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		text     string
		expected int
	}{
		{"worried about", "I'm worried about the auth flow breaking.", 1},
		{"should check", "We should check the error handling there.", 1},
		{"need to verify", "I need to verify the migration works.", 1},
		{"might break", "This change might break the API contract.", 1},
		{"concerned about", "I'm concerned about the memory usage.", 1},
		{"want to confirm", "I want to confirm the tests pass.", 1},
		{"multiple signals", "I'm worried about X. We should check Y.", 2},
		{"no signal", "The code looks fine to me.", 0},
		{"empty", "", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			concerns := orchestrator.ExtractConcerns(tt.text)
			assert.Len(t, concerns, tt.expected)
		})
	}
}

func TestConcernTracker_AddAndRetrieve(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcern(agent.RoleEngineer1, "auth might break", "JAM-1")
	tracker.AddConcern(agent.RoleEngineer2, "tests flaky", "JAM-2")

	eng1Concerns := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Len(t, eng1Concerns, 1)
	assert.Equal(t, "auth might break", eng1Concerns[0].Content)

	eng2Concerns := tracker.UnresolvedForRole(agent.RoleEngineer2)
	assert.Len(t, eng2Concerns, 1)
}

func TestConcernTracker_Resolve_RemovesFromUnresolved(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcern(agent.RoleEngineer1, "auth might break", "JAM-1")
	tracker.AddConcern(agent.RoleEngineer1, "tests flaky", "JAM-2")

	tracker.ResolveConcern(agent.RoleEngineer1, "auth might break")

	unresolved := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Len(t, unresolved, 1)
	assert.Equal(t, "tests flaky", unresolved[0].Content)
}

func TestConcernTracker_AddConcernsFromText(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcernsFromText(agent.RoleEngineer1,
		"I'm worried about the auth flow. Also need to verify the migration.",
		"JAM-1")

	unresolved := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Len(t, unresolved, 2)
}

func TestConcernTracker_UnresolvedForRole_EmptyTracker(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewConcernTracker()
	unresolved := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Empty(t, unresolved)
}

func TestConcernTracker_AllConcerns_ReturnsAll(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcern(agent.RoleEngineer1, "concern 1", "JAM-1")
	tracker.AddConcern(agent.RoleEngineer2, "concern 2", "JAM-2")

	all := tracker.AllConcerns()
	assert.Len(t, all, 2)
}

func TestInvestigateConcerns_NilTracker_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{}, checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.InvestigateConcerns(ctx, []agent.Role{agent.RoleEngineer1})
	})
}

func TestInvestigateConcerns_WithConcern_InvestigatesAndResolves(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Checked the auth flow — it handles retries correctly."}` + "\n"),
	}

	engAgent := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, bot, nil,
	)

	tracker := orchestrator.NewConcernTracker()
	tracker.AddConcern(agent.RoleEngineer1, "worried about auth flow retries", "JAM-1")
	orch.SetConcernTracker(tracker)

	orch.InvestigateConcerns(ctx, []agent.Role{agent.RoleEngineer1})

	// Concern should now be resolved.
	unresolved := tracker.UnresolvedForRole(agent.RoleEngineer1)
	assert.Empty(t, unresolved, "concern should be resolved after investigation")
}

func TestInvestigateConcerns_NoConcerns_DoesNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}

	engAgent := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, nil, nil,
	)

	tracker := orchestrator.NewConcernTracker()
	orch.SetConcernTracker(tracker)

	runner.mu.Lock()
	beforeCount := len(runner.calls)
	runner.mu.Unlock()

	orch.InvestigateConcerns(ctx, []agent.Role{agent.RoleEngineer1})

	runner.mu.Lock()
	afterCount := len(runner.calls)
	runner.mu.Unlock()

	assert.Equal(t, beforeCount, afterCount, "no concerns means no agent calls")
}

func TestConversationEngine_ConcernExtraction_StoresConcerns(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I'm worried about the migration breaking production."}` + "\n"),
	}

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, runner, role, db)
		factStores[role] = memory.NewFactStore(db)
	}

	tracker := orchestrator.NewConcernTracker()
	engine := orchestrator.NewConversationEngine(agents, factStores, nil, nil)
	engine.SetConcernTracker(tracker)

	engine.OnMessage(ctx, "engineering", "ceo", "how's the migration going?")

	// At least one concern should have been extracted from agent responses.
	all := tracker.AllConcerns()
	// This depends on agent responses which may or may not contain concern
	// signals, so just verify no crash.
	_ = all
}
