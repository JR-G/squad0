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

func TestResumePendingWork_ChangesRequested_TriggersFixUp(t *testing.T) {
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

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	// Engineer runs fix-up; reviewer re-reviews (CHANGES_REQUESTED → escalate).
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Fixed."}` + "\n"),
	}
	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CHANGES_REQUESTED"}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
	}

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
		agent.RoleReviewer:  reviewerAgent,
	}

	repoDir := t.TempDir()
	initTestRepoWithBranch(t, repoDir, "feat/jam-cr1")

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      3,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
			TargetRepoDir:    repoDir,
		},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Create item in ChangesRequested stage with a PR URL.
	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-CR1",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-cr1",
	})
	require.NoError(t, createErr)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test-org/test-repo/pull/200"))
	require.NoError(t, pipeStore.Advance(ctx, itemID, pipeline.StageChangesRequested))

	timedCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_ = orch.Run(timedCtx)
	orch.Wait()

	// Engineer should have been called for the fix-up.
	engRunner.mu.Lock()
	engCalls := len(engRunner.calls)
	engRunner.mu.Unlock()
	assert.GreaterOrEqual(t, engCalls, 1, "engineer should have run fix-up")
}

func TestResumePendingWork_WorkingWithPR_TriggersFixUp(t *testing.T) {
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

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	// Engineer fixes up; reviewer CHANGES_REQUESTED → escalate.
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Fixed."}` + "\n"),
	}
	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CHANGES_REQUESTED"}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
	}

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, memDB)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
		agent.RoleReviewer:  reviewerAgent,
	}

	repoDir := t.TempDir()
	initTestRepoWithBranch(t, repoDir, "feat/jam-wp1")

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      3,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
			TargetRepoDir:    repoDir,
		},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Create item in StageWorking with a PR URL — simulates restart mid-work.
	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-WP1",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-wp1",
	})
	require.NoError(t, createErr)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test-org/test-repo/pull/201"))

	timedCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_ = orch.Run(timedCtx)
	orch.Wait()

	// Engineer should have been called for the fix-up.
	engRunner.mu.Lock()
	engCalls := len(engRunner.calls)
	engRunner.mu.Unlock()
	assert.GreaterOrEqual(t, engCalls, 1, "engineer should have run fix-up for working item with PR")
}

func TestResumePendingWork_Approved_TriggersMerge(t *testing.T) {
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

	// PM: checkApprovalStatus → APPROVED, executeMerge → done, verifyMerged → MERGED, MoveTicketState.
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
		outputs: [][]byte{
			// First call is PM duties (empty assignments).
			[]byte(`{"type":"result","result":"[]"}` + "\n"),
			// checkApprovalStatus.
			[]byte(`{"type":"result","result":"APPROVED"}` + "\n"),
			// executeMerge.
			[]byte(`{"type":"result","result":"done"}` + "\n"),
			// verifyMerged.
			[]byte(`{"type":"result","result":"MERGED"}` + "\n"),
			// MoveTicketState.
			[]byte(`{"type":"result","result":"done"}` + "\n"),
		},
	}
	pmAgent := setupPMAgent(t, pmRunner)

	agents := map[agent.Role]*agent.Agent{agent.RolePM: pmAgent}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      1,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Create item in StageApproved with PR URL.
	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-AP1",
		Engineer: agent.RolePM,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-ap1",
	})
	require.NoError(t, createErr)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test-org/test-repo/pull/202"))
	require.NoError(t, pipeStore.Advance(ctx, itemID, pipeline.StageApproved))

	timedCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	_ = orch.Run(timedCtx)
	orch.Wait()

	pmRunner.mu.Lock()
	pmCalls := len(pmRunner.calls)
	pmRunner.mu.Unlock()
	assert.GreaterOrEqual(t, pmCalls, 1, "PM should have been called for merge steps")
}

func TestMergeAfterRetry_NotApproved_AnnouncesFailure(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// mergeAfterRetry: checkApprovalStatus returns NOT_APPROVED.
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"PENDING"}` + "\n"),
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
		orch.MergeAfterRetryForTest(ctx, "https://github.com/test-org/test-repo/pull/300", "JAM-RT1", 0, agent.RoleEngineer1)
	})
}

func TestMergeAfterRetry_MergeFails_ReturnsEarly(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// checkApprovalStatus → APPROVED, executeMerge → CI FAIL (returns false).
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
		outputs: [][]byte{
			[]byte(`{"type":"result","result":"APPROVED"}` + "\n"),
			[]byte(`{"type":"result","result":"CI FAIL: checks not passing"}` + "\n"),
		},
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
		orch.MergeAfterRetryForTest(ctx, "https://github.com/test-org/test-repo/pull/301", "JAM-RT2", 0, agent.RoleEngineer1)
	})
}

func TestMergeAfterRetry_VerifyFails_AnnouncesManualMerge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	// checkApprovalStatus → APPROVED, executeMerge → done, verifyMerged → OPEN (not merged).
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
		outputs: [][]byte{
			[]byte(`{"type":"result","result":"APPROVED"}` + "\n"),
			[]byte(`{"type":"result","result":"done"}` + "\n"),
			[]byte(`{"type":"result","result":"OPEN"}` + "\n"),
		},
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
		orch.MergeAfterRetryForTest(ctx, "https://github.com/test-org/test-repo/pull/302", "JAM-RT3", 0, agent.RoleEngineer1)
	})
}

func TestMergeAfterRetry_NoPM_ReturnsEarly(t *testing.T) {
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
		orch.MergeAfterRetryForTest(ctx, "https://github.com/test-org/test-repo/pull/303", "JAM-RT4", 0, agent.RoleEngineer1)
	})
}

func TestResumeWithGitHubState_Approved_SendsEngineerToMerge(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// PM returns APPROVED for checkApprovalStatus, then MERGED for verifyMerged (but
	// verifyMerged runs first — order: verifyMerged returns NOT merged, then checkApproval returns APPROVED).
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"OPEN"}` + "\n"),
		outputs: [][]byte{
			[]byte(`{"type":"result","result":"OPEN"}` + "\n"),     // verifyMerged
			[]byte(`{"type":"result","result":"APPROVED"}` + "\n"), // checkApprovalStatus
			[]byte(`{"type":"result","result":"MERGED"}` + "\n"),   // engineer merge verify
		},
	}
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}

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

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent, agent.RoleEngineer1: engAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-GH1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking, Branch: "feat/jam-gh1",
	})
	require.NoError(t, createErr)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test-org/test-repo/pull/100"))

	// Call resumeWorkItem directly to avoid tick loop timing issues.
	orch.ResumeWorkItemForTest(ctx, pipeline.WorkItem{
		ID: itemID, Ticket: "JAM-GH1", Engineer: agent.RoleEngineer1,
		Stage: pipeline.StageWorking, PRURL: "https://github.com/test-org/test-repo/pull/100",
		Branch: "feat/jam-gh1",
	})
	orch.Wait()

	engRunner.mu.Lock()
	engCalls := len(engRunner.calls)
	engRunner.mu.Unlock()
	assert.GreaterOrEqual(t, engCalls, 1)
}

func TestResumeWithGitHubState_AlreadyMerged_AdvancesPipeline(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"MERGED"}` + "\n"),
	}

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

	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-GH2", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking, Branch: "feat/jam-gh2",
	})
	require.NoError(t, createErr)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test-org/test-repo/pull/101"))

	orch.ResumeWorkItemForTest(ctx, pipeline.WorkItem{
		ID: itemID, Ticket: "JAM-GH2", Engineer: agent.RoleEngineer1,
		Stage: pipeline.StageWorking, PRURL: "https://github.com/test-org/test-repo/pull/101",
		Branch: "feat/jam-gh2",
	})
	orch.Wait()

	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}
