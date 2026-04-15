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

func stubOutstandingComments(comments ...orchestrator.ReviewComment) func() {
	restoreComments := orchestrator.SetCommentFetcherForTest(
		func(_ context.Context, _, _ string) []orchestrator.ReviewComment {
			return comments
		},
	)
	restoreBot := orchestrator.SetLiveBotReviewCheckerForTest(
		func(_ context.Context, _, _ string) bool { return false },
	)
	return func() {
		restoreBot()
		restoreComments()
	}
}

func stubLiveBotReview(live bool) func() {
	restoreComments := orchestrator.SetCommentFetcherForTest(
		func(_ context.Context, _, _ string) []orchestrator.ReviewComment { return nil },
	)
	restoreBot := orchestrator.SetLiveBotReviewCheckerForTest(
		func(_ context.Context, _, _ string) bool { return live },
	)
	return func() {
		restoreBot()
		restoreComments()
	}
}

func TestReconcileItem_OpenApproved_OutstandingComments_StaysInReviewing(t *testing.T) {
	restore := stubOutstandingComments(orchestrator.ReviewComment{
		ID: "rc-1", Severity: "blocker", Body: "devin: fix the auth check",
	})
	defer restore()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-GATE-1",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/100")

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:       itemID,
		Ticket:   "JAM-GATE-1",
		PRURL:    "https://github.com/org/repo/pull/100",
		Stage:    pipeline.StagePROpened,
		Engineer: agent.RoleEngineer1,
	}, orchestrator.PRState{State: "OPEN", ReviewDecision: "APPROVED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageReviewing, item.Stage,
		"approved PR with unaddressed comments must drop back to reviewing")
}

func TestReconcileItem_OpenApproved_AlreadyReviewing_NoStageChange(t *testing.T) {
	restore := stubOutstandingComments(orchestrator.ReviewComment{
		ID: "rc-1", Severity: "blocker", Body: "coderabbit: race condition",
	})
	defer restore()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-GATE-2",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageReviewing,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/101")

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:       itemID,
		Ticket:   "JAM-GATE-2",
		PRURL:    "https://github.com/org/repo/pull/101",
		Stage:    pipeline.StageReviewing,
		Engineer: agent.RoleEngineer1,
	}, orchestrator.PRState{State: "OPEN", ReviewDecision: "APPROVED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageReviewing, item.Stage)
}

func TestReconcileItem_OpenApproved_LiveBotReview_StaysInReviewing(t *testing.T) {
	restore := stubLiveBotReview(true)
	defer restore()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-GATE-BOT",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StagePROpened,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/103")

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:       itemID,
		Ticket:   "JAM-GATE-BOT",
		PRURL:    "https://github.com/org/repo/pull/103",
		Stage:    pipeline.StagePROpened,
		Engineer: agent.RoleEngineer1,
	}, orchestrator.PRState{State: "OPEN", ReviewDecision: "APPROVED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageReviewing, item.Stage,
		"live Devin/CodeRabbit review must block merge even without structured tags")
}

func TestReconcileItem_OpenApproved_SuggestionsOnly_Advances(t *testing.T) {
	// Suggestions are advisory (see FormatFixUpChecklist: "Suggestions
	// are optional but appreciated"). A PR with only suggestion-level
	// feedback and no blockers must advance past the approval gate —
	// the alternative is an infinite reviewing→fix-up→re-review loop
	// every time a reviewer uses the word "suggestion" in an otherwise
	// approved review. This is the regression behind JAM-24's stuck
	// state during the Apr 15 incident.
	restore := stubOutstandingComments(
		orchestrator.ReviewComment{ID: "rc-1", Severity: "suggestion", Body: "consider renaming foo"},
		orchestrator.ReviewComment{ID: "rc-2", Severity: "suggestion", Body: "add a docstring here"},
	)
	defer restore()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-GATE-SUGGEST",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageReviewing,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/555")

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:       itemID,
		Ticket:   "JAM-GATE-SUGGEST",
		PRURL:    "https://github.com/org/repo/pull/555",
		Stage:    pipeline.StageReviewing,
		Engineer: agent.RoleEngineer1,
	}, orchestrator.PRState{State: "OPEN", ReviewDecision: "APPROVED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageApproved, item.Stage,
		"suggestion-only comments must not block an approved PR")
}

func TestReconcileItem_OpenApproved_NoOutstandingComments_Advances(t *testing.T) {
	restore := stubOutstandingComments()
	defer restore()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	store := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, store.InitSchema(ctx))

	itemID, _ := store.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-GATE-3",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageReviewing,
	})
	_ = store.SetPRURL(ctx, itemID, "https://github.com/org/repo/pull/102")

	orch := newReconcileOrch(t, store)
	orch.ReconcileItemForTest(ctx, pipeline.WorkItem{
		ID:       itemID,
		Ticket:   "JAM-GATE-3",
		PRURL:    "https://github.com/org/repo/pull/102",
		Stage:    pipeline.StageReviewing,
		Engineer: agent.RoleEngineer1,
	}, orchestrator.PRState{State: "OPEN", ReviewDecision: "APPROVED"})

	item, getErr := store.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageApproved, item.Stage)
}

func TestResumeWithGitHubState_ApprovedWithOutstandingComments_RevertsToReviewing(t *testing.T) {
	restore := stubOutstandingComments(orchestrator.ReviewComment{
		ID: "rc-1", Severity: "blocker", Body: "devin: unhandled error path",
	})
	defer restore()

	ctx := context.Background()
	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	pmRunner := &fakeProcessRunner{
		outputs: [][]byte{
			[]byte(`{"type":"result","result":"OPEN"}` + "\n"),
			[]byte(`{"type":"result","result":"APPROVED"}` + "\n"),
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
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      1,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent, agent.RoleEngineer1: engAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-GATE-RESUME",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-gate-resume",
	})
	require.NoError(t, createErr)
	prURL := "https://github.com/test-org/test-repo/pull/200"
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, prURL))

	orch.ResumeWorkItemForTest(ctx, pipeline.WorkItem{
		ID:       itemID,
		Ticket:   "JAM-GATE-RESUME",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		PRURL:    prURL,
		Branch:   "feat/jam-gate-resume",
	})
	orch.Wait()

	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageReviewing, item.Stage,
		"resume must not fast-forward to approved when comments are outstanding")
}
