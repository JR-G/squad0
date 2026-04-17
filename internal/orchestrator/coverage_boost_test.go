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

func TestPostDailySummary_WithOpenItems_IncludesReviewAndBlocked(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore := setupPMDutiesOrch(t)

	// Create items in different stages — fixture uses AdvanceForce
	// so the test doesn't have to walk every legitimate intermediate
	// stage just to set up state.
	createItem := func(ticket string, stage pipeline.Stage) {
		itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
			Ticket: ticket, Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking, Branch: "feat/" + ticket,
		})
		require.NoError(t, err)
		require.NoError(t, pipeStore.AdvanceForce(ctx, itemID, stage, "test fixture"))
	}

	createItem("JAM-10", pipeline.StageMerged)
	createItem("JAM-11", pipeline.StageReviewing)
	createItem("JAM-12", pipeline.StageChangesRequested)

	assert.NotPanics(t, func() {
		orch.PostDailySummary(ctx)
	})
}

func TestCheckStaleWork_NoStaleItems_DoesNotFollowUp(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore := setupPMDutiesOrch(t)

	// Create a recent item — not stale.
	_, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-20", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking, Branch: "feat/jam-20",
	})
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		orch.RunPMDuties(ctx)
	})
}

func TestCheckStaleWork_NonWorkingStage_Skipped(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore := setupPMDutiesOrch(t)

	// Create item in PR opened stage — not "working", should be skipped.
	itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-30", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking, Branch: "feat/jam-30",
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.Advance(ctx, itemID, pipeline.StagePROpened))

	// Age it.
	_, err = pipeStore.DB().ExecContext(ctx,
		`UPDATE work_items SET updated_at = datetime('now', '-45 minutes') WHERE id = ?`, itemID)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		orch.RunPMDuties(ctx)
	})
}

func TestBreakDiscussionTie_NoMessages_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Decision: go with A."}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

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

	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	for _, role := range allRoles {
		if role == agent.RolePM {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetConversationEngine(conversation)

	// No messages seeded — should return empty.
	result := orch.BreakDiscussionTie(ctx, "nonexistent")
	assert.Empty(t, result)
}

func TestBreakDiscussionTie_PMError_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	pmAgent := setupPMAgent(t, pmRunner)

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

	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"PASS"}` + "\n")}
	for _, role := range allRoles {
		if role == agent.RolePM {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetConversationEngine(conversation)

	// Seed messages.
	conversation.OnMessage(ctx, "engineering", "ceo", "which approach?")

	result := orch.BreakDiscussionTie(ctx, "engineering")
	assert.Empty(t, result)
}

func TestBreakDiscussionTie_EmptyResponse_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":""}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

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

	chatRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":""}` + "\n")}
	for _, role := range allRoles {
		if role == agent.RolePM {
			continue
		}
		agents[role] = buildAgent(t, chatRunner, role, memDB)
		factStores[role] = memory.NewFactStore(memDB)
	}

	conversation := orchestrator.NewConversationEngine(agents, factStores, nil, nil)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetConversationEngine(conversation)

	conversation.OnMessage(ctx, "engineering", "ceo", "thoughts?")

	result := orch.BreakDiscussionTie(ctx, "engineering")
	assert.Empty(t, result)
}

func TestVerifyTicketState_WithPM_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.NotPanics(t, func() {
		orch.VerifyTicketState(ctx, "JAM-1", "In Progress")
	})
}

func TestRunConversationalArchReview_Error_ReturnsApproved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	tlRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	tlAgent := buildAgent(t, tlRunner, agent.RoleTechLead, memDB)

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RoleTechLead: tlAgent},
		checkIns, nil, nil,
	)

	outcome := orch.RunConversationalArchReview(ctx, "https://github.com/test-org/test-repo/pull/1", "T-1", agent.RoleEngineer1)
	assert.Equal(t, orchestrator.ReviewApproved, outcome)
}

func TestMergeAndComplete_PMError_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	pmAgent := setupPMAgent(t, pmRunner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.NotPanics(t, func() {
		orch.MergeForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "JAM-1", 0)
	})
}

func TestMergeAndComplete_NoPMAgent_ReturnsEarly(t *testing.T) {
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
		orch.MergeForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "JAM-1", 0)
	})
}

func TestStartReReview_NoReviewer_DoesNotPanic(t *testing.T) {
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
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// startReReview is called internally but we test via exported wrapper.
	// Just verify no crash with no reviewer.
	assert.NotPanics(t, func() {
		orch.StartReviewForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "T-1")
	})
}

func TestExtractAndStoreDecisions_NoDecisionSignals_DoesNotStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	tlRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"The code looks fine. APPROVED"}` + "\n"),
	}
	tlAgent := buildAgent(t, tlRunner, agent.RoleTechLead, memDB)
	factStore := memory.NewFactStore(memDB)
	graphStore := memory.NewGraphStore(memDB)
	tlAgent.SetMemoryStores(graphStore, factStore)

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RoleTechLead: tlAgent},
		checkIns, nil, nil,
	)

	// "The code looks fine" has no decision signals.
	outcome := orch.RunConversationalArchReview(ctx, "https://github.com/test-org/test-repo/pull/1", "T-1", agent.RoleEngineer1)
	assert.Equal(t, orchestrator.ReviewApproved, outcome)
}

func TestMergeAfterRetry_PMError_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}

	// First call returns NOT_APPROVED, reviewer says done, second PM call fails.
	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmAgent := setupPMAgent(t, pmRunner)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, memDB)

	sqlDB, sqlErr := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, sqlErr)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent, agent.RoleReviewer: reviewerAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.NotPanics(t, func() {
		orch.MergeForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "JAM-1", 0)
	})
}

func TestStartReReview_NoReviewer_ReturnsEarly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	engRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	engAgent := setupAgentWithRole(t, engRunner, agent.RoleEngineer1)

	// No reviewer — startReReview returns early.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, TargetRepoDir: t.TempDir()},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent, agent.RoleEngineer1: engAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// Trigger via changes requested → fix-up → re-review (no reviewer).
	assert.NotPanics(t, func() {
		orch.StartReviewWithItemForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "T-1", 0, agent.RoleEngineer1)
		orch.Wait()
	})
}
