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

func TestCheckApprovalStatus_Approved(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	result := orch.CheckApprovalStatusForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/42")
	assert.Equal(t, "APPROVED", result)
}

func TestCheckApprovalStatus_NotApproved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		output string
	}{
		{"REVIEW_REQUIRED", "REVIEW_REQUIRED"},
		{"CHANGES_REQUESTED", "CHANGES_REQUESTED"},
		{"PENDING", "PENDING"},
		{"empty response", ""},
		{"random text", "something unexpected"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := context.Background()
			runner := &fakeProcessRunner{
				output: []byte(`{"type":"result","result":"` + tt.output + `"}` + "\n"),
			}
			pmAgent := setupPMAgent(t, runner)

			sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
			require.NoError(t, err)
			t.Cleanup(func() { _ = sqlDB.Close() })

			checkIns := coordination.NewCheckInStore(sqlDB)
			require.NoError(t, checkIns.InitSchema(ctx))

			orch := orchestrator.NewOrchestrator(
				orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
				map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
				checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
			)

			result := orch.CheckApprovalStatusForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/42")
			assert.Equal(t, "NOT_APPROVED", result)
		})
	}
}

func TestCheckApprovalStatus_Error(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	result := orch.CheckApprovalStatusForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/42")
	assert.Equal(t, "ERROR", result)
}

func TestCheckApprovalStatus_PromptContainsPRNumber(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	_ = orch.CheckApprovalStatusForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/99")

	require.NotEmpty(t, runner.calls)
	assert.Contains(t, runner.calls[0].stdin, "gh pr view https://github.com/test-org/test-repo/pull/99 --json reviewDecision")
}

func TestExecuteMerge_Success(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	result := orch.ExecuteMergeForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/42", "JAM-1", agent.RoleEngineer1)
	assert.True(t, result)

	require.NotEmpty(t, runner.calls)
	assert.Contains(t, runner.calls[0].stdin, "gh pr merge https://github.com/test-org/test-repo/pull/42 --squash --delete-branch")
}

func TestExecuteMerge_CIFail_ReturnsFalse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CI FAIL: required checks not passing"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	result := orch.ExecuteMergeForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/42", "JAM-1", agent.RoleEngineer1)
	assert.False(t, result)
}

func TestExecuteMerge_Error_ReturnsFalse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	result := orch.ExecuteMergeForTest(ctx, pmAgent, "https://github.com/test-org/test-repo/pull/42", "JAM-1", agent.RoleEngineer1)
	assert.False(t, result)
}

func TestMergeAndComplete_ApprovalError_Announces(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	pmAgent := setupPMAgent(t, runner)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	// Should not panic — ERROR path announces and returns.
	assert.NotPanics(t, func() {
		orch.MergeForTest(ctx, "https://github.com/test-org/test-repo/pull/1", "JAM-1", 0)
	})
}

func TestForceApproval_SubmitsGHReview(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"done"}` + "\n"),
	}
	reviewerAgent := setupAgentWithRole(t, runner, agent.RoleReviewer)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RoleReviewer: reviewerAgent},
		checkIns, nil, nil,
	)

	orch.ForceApprovalForTest(ctx, reviewerAgent,
		"https://github.com/test-org/test-repo/pull/42", "JAM-1")

	require.NotEmpty(t, runner.calls)
	assert.Contains(t, runner.calls[0].stdin,
		"gh pr review https://github.com/test-org/test-repo/pull/42 --approve")
}

func TestForceApproval_Error_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	runner := &fakeProcessRunner{
		output: []byte(`{"type":"error","content":"fail"}` + "\n"),
		err:    assert.AnError,
	}
	reviewerAgent := setupAgentWithRole(t, runner, agent.RoleReviewer)

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RoleReviewer: reviewerAgent},
		checkIns, nil, nil,
	)

	assert.NotPanics(t, func() {
		orch.ForceApprovalForTest(ctx, reviewerAgent,
			"https://github.com/test-org/test-repo/pull/42", "JAM-1")
	})
}

func TestMergeAfterRetry_FullSuccess(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	// mergeAfterRetry calls: checkApprovalStatus, executeMerge, verifyMerged.
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
		outputs: [][]byte{
			[]byte(`{"type":"result","result":"APPROVED"}` + "\n"),
			[]byte(`{"type":"result","result":"done"}` + "\n"),
			[]byte(`{"type":"result","result":"MERGED"}` + "\n"),
		},
	}
	pmAgent := setupPMAgent(t, pmRunner)

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
		Ticket: "JAM-MR", Engineer: agent.RoleEngineer1, Stage: pipeline.StageApproved, Branch: "feat/jam-mr",
	})
	require.NoError(t, createErr)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	orch.MergeAfterRetryForTest(ctx, "https://github.com/test-org/test-repo/pull/99", "JAM-MR", itemID, agent.RoleEngineer1)

	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageMerged, item.Stage)
}
