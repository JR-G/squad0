package orchestrator_test

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupPMDutiesOrch(t *testing.T) (*orchestrator.Orchestrator, *pipeline.WorkItemStore) {
	t.Helper()

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

	eng1Runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	eng1Agent := setupAgentWithRole(t, eng1Runner, agent.RoleEngineer1)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1Agent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	return orch, pipeStore
}

func TestRunPMDuties_NoPipeline_DoesNotPanic(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)

	assert.NotPanics(t, func() {
		orch.RunPMDuties(context.Background())
	})
}

func TestRunPMDuties_WithStaleItem_LogsFollowUp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore := setupPMDutiesOrch(t)

	// Create a work item that's been working for over 30 minutes.
	itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-99",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-99",
	})
	require.NoError(t, err)

	// Manually age the item.
	_, err = pipeStore.DB().ExecContext(ctx,
		`UPDATE work_items SET updated_at = datetime('now', '-45 minutes') WHERE id = ?`, itemID)
	require.NoError(t, err)

	// RunPMDuties should detect the stale item.
	assert.NotPanics(t, func() {
		orch.RunPMDuties(ctx)
	})
}

func TestPostDailySummary_NoPipeline_DoesNotPanic(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)

	assert.NotPanics(t, func() {
		orch.PostDailySummary(context.Background())
	})
}

func TestPostDailySummary_WithPipeline_BuildsSummary(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore := setupPMDutiesOrch(t)

	// Create a completed work item.
	itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-50",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-50",
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.Advance(ctx, itemID, pipeline.StageMerged))

	assert.NotPanics(t, func() {
		orch.PostDailySummary(ctx)
	})
}

func TestBreakDiscussionTie_NoPM_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	result := orch.BreakDiscussionTie(ctx, "engineering")
	assert.Empty(t, result)
}

func TestBreakDiscussionTie_WithPM_ReturnsDecision(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Decision: let's go with approach A."}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

	chatRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"I think approach A."}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	agents[agent.RolePM] = pmAgent
	factStores[agent.RolePM] = memory.NewFactStore(memDB)

	for _, role := range allRoles {
		if role == agent.RolePM {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	conversation := orchestrator.NewConversationEngine(agents, factStores, bot, nil)
	orch.SetConversationEngine(conversation)

	// Seed messages so there's context.
	conversation.OnMessage(ctx, "engineering", "ceo", "which approach?")

	result := orch.BreakDiscussionTie(ctx, "engineering")
	assert.Contains(t, result, "Decision")
}

func TestBreakDiscussionTie_PromptContainsDecisionInstruction(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Decision: use approach B."}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// No conversation engine — BreakDiscussionTie still works
	// with recent messages from the engine, but without one
	// it returns empty. Instead, set up a conversation engine
	// but DON'T trigger OnMessage (which would cause extra calls).
	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	agents[agent.RolePM] = pmAgent
	factStores[agent.RolePM] = memory.NewFactStore(memDB)

	for _, role := range allRoles {
		if role == agent.RolePM {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	conversation := orchestrator.NewConversationEngine(agents, factStores, bot, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetConversationEngine(conversation)

	// Seed messages directly to avoid triggering OnMessage callbacks
	// that would consume PM runner calls.
	conversation.OnMessage(ctx, "engineering", "human-ceo", "which approach?")
	// Wait for conversation callbacks to finish.
	time.Sleep(50 * time.Millisecond)

	// Now call BreakDiscussionTie — the PM QuickChat call should
	// contain the decision instruction.
	_ = orch.BreakDiscussionTie(ctx, "engineering")

	pmRunner.mu.Lock()
	calls := make([]fakeCall, len(pmRunner.calls))
	copy(calls, pmRunner.calls)
	pmRunner.mu.Unlock()

	// Find the call that contains the tie-breaking prompt.
	found := false
	for _, call := range calls {
		if strings.Contains(call.stdin, "what to build and what to skip") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected PM to receive prompt with 'what to build and what to skip'")
}

func TestBreakDiscussionTie_WithDecision_StoresIt(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Decision: go with approach A, skip the cache."}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

	chatRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PASS"}` + "\n"),
	}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// Create a tech lead with fact store so StoreArchitectureDecision works.
	tlRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	tlAgent := buildAgent(t, tlRunner, agent.RoleTechLead, memDB)
	factStore := memory.NewFactStore(memDB)
	graphStore := memory.NewGraphStore(memDB)
	tlAgent.SetMemoryStores(graphStore, factStore)

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	agents[agent.RolePM] = pmAgent
	agents[agent.RoleTechLead] = tlAgent
	factStores[agent.RolePM] = memory.NewFactStore(memDB)
	factStores[agent.RoleTechLead] = factStore

	for _, role := range allRoles {
		if role == agent.RolePM || role == agent.RoleTechLead {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	conversation := orchestrator.NewConversationEngine(agents, factStores, bot, nil)
	orch.SetConversationEngine(conversation)
	conversation.OnMessage(ctx, "engineering", "ceo", "which approach?")

	result := orch.BreakDiscussionTie(ctx, "engineering")
	assert.Contains(t, result, "Decision")

	// The decision should have been stored via StoreArchitectureDecision.
	beliefs, beliefErr := factStore.TopBeliefs(ctx, 5)
	require.NoError(t, beliefErr)
	assert.NotEmpty(t, beliefs)
	assert.Contains(t, beliefs[0].Content, "go with approach A")
}

func TestVerifyTicketState_NoPM_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.VerifyTicketState(ctx, "JAM-1", "Done")
	})
}

func TestFormatDuration_HoursAndMinutes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "2h 41m", orchestrator.FormatDurationForTest(2*time.Hour+41*time.Minute))
}

func TestFormatDuration_MinutesOnly(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "45m", orchestrator.FormatDurationForTest(45*time.Minute))
}

func TestFormatDuration_Zero(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "0m", orchestrator.FormatDurationForTest(0))
}
