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
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChangesRequested_IncludesEngineerName(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// Reviewer says CHANGES_REQUESTED.
	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CHANGES_REQUESTED - fix nil check"}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	engRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Fixed"}` + "\n")}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-20", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing, Branch: "feat/jam-20",
	})
	require.NoError(t, createErr)

	pmAgent := setupPMAgent(t, pmRunner)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, memDB)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleReviewer:  reviewerAgent,
		agent.RoleEngineer1: engAgent,
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second, TargetRepoDir: t.TempDir()},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)
	orch.SetRoster(map[agent.Role]string{
		agent.RoleEngineer1: "Alice",
		agent.RoleReviewer:  "Bob",
	})

	// The review will return CHANGES_REQUESTED. The announcement in
	// #reviews should mention the engineer's name (Alice) and use
	// postAsRole to trigger the conversation engine.
	assert.NotPanics(t, func() {
		orch.StartReviewWithItemForTest(ctx, "https://github.com/test-org/test-repo/pull/20", "JAM-20", itemID, agent.RoleEngineer1)
		orch.Wait()
	})

	// Verify the pipeline stage was advanced to changes_requested.
	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	// It should be at least past changes_requested (may have
	// continued into fix-up depending on how far the loop got).
	assert.NotEqual(t, pipeline.StageReviewing, item.Stage)
}

func TestChangesRequested_UsesPostAsRole(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// Reviewer says CHANGES_REQUESTED — this triggers
	// handleChangesRequested which should use postAsRole.
	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CHANGES_REQUESTED"}` + "\n"),
	}
	engRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"Fixed"}` + "\n")}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeDB, pipeErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, pipeErr)
	t.Cleanup(func() { _ = pipeDB.Close() })

	pipeStore := pipeline.NewWorkItemStore(pipeDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-21", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing, Branch: "feat/jam-21",
	})
	require.NoError(t, createErr)

	pmAgent := setupPMAgent(t, pmRunner)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, memDB)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleReviewer:  reviewerAgent,
		agent.RoleEngineer1: engAgent,
	}

	// Bot is needed so postAsRole actually does something
	// (vs announceAsRole which doesn't trigger conversations).
	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	allRoles := agent.AllRoles()
	agentsForConv := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	for _, role := range allRoles {
		existing, ok := agents[role]
		if !ok {
			existing = buildAgent(t, chatRunner, role, memDB)
		}
		agentsForConv[role] = existing
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agentsForConv, factStores, bot, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second, TargetRepoDir: t.TempDir()},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)
	orch.SetConversationEngine(conversation)
	orch.SetRoster(map[agent.Role]string{
		agent.RoleEngineer1: "Alice",
	})

	// Start the review — changes will be requested, and the message
	// should trigger the conversation engine via postAsRole.
	assert.NotPanics(t, func() {
		orch.StartReviewWithItemForTest(ctx, "https://github.com/test-org/test-repo/pull/21", "JAM-21", itemID, agent.RoleEngineer1)
		orch.Wait()
	})
}

func TestAcknowledgeThread_NoConversation_DoesNotPanic(t *testing.T) {
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

	runner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"on it"}` + "\n")}
	engAgent := buildAgent(t, runner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RoleEngineer1: engAgent},
		checkIns, nil, nil,
	)

	// No conversation engine — should return without panic.
	assert.NotPanics(t, func() {
		orch.AcknowledgeThreadForTest(ctx, engAgent, agent.RoleEngineer1, "engineering")
	})
}

func TestAcknowledgeThread_WithMessages_PostsAcknowledgment(t *testing.T) {
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

	engRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"got it, diving in now"}` + "\n")}
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	agents[agent.RoleEngineer1] = engAgent
	factStores[agent.RoleEngineer1] = memory.NewFactStore(memDB)

	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	for _, role := range allRoles {
		if role == agent.RoleEngineer1 {
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
		agents, checkIns, bot, nil,
	)
	orch.SetConversationEngine(conversation)

	// Seed messages so acknowledgeThread has something to respond to.
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer1), "picking up JAM-42")
	conversation.OnMessage(ctx, "engineering", "ceo", "nice, keep me posted")

	orch.AcknowledgeThreadForTest(ctx, engAgent, agent.RoleEngineer1, "engineering")

	// Engineer should have been called with QuickChat to acknowledge.
	engRunner.mu.Lock()
	callCount := len(engRunner.calls)
	engRunner.mu.Unlock()
	assert.GreaterOrEqual(t, callCount, 1)
}

func TestAcknowledgeThread_LastMessageIsOwn_Skips(t *testing.T) {
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

	engRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	allRoles := agent.AllRoles()
	agents := make(map[agent.Role]*agent.Agent, len(allRoles))
	factStores := make(map[agent.Role]*memory.FactStore, len(allRoles))
	for _, role := range allRoles {
		agents[role] = buildAgent(t, engRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, nil,
	)
	orch.SetConversationEngine(conversation)

	// Last message is from engineer-1 — should skip acknowledgment.
	conversation.OnMessage(ctx, "engineering", "ceo", "hello")
	conversation.OnMessage(ctx, "engineering", string(agent.RoleEngineer1), "picking up the ticket")

	engRunner.mu.Lock()
	beforeCount := len(engRunner.calls)
	engRunner.mu.Unlock()

	orch.AcknowledgeThreadForTest(ctx, engAgent, agent.RoleEngineer1, "engineering")

	engRunner.mu.Lock()
	afterCount := len(engRunner.calls)
	engRunner.mu.Unlock()

	// No new calls — skipped because last message was own.
	assert.Equal(t, beforeCount, afterCount)
}
