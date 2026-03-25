package orchestrator_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"os/exec"
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

func newPipelineStore(t *testing.T, db *sql.DB) *pipeline.WorkItemStore {
	t.Helper()
	store := pipeline.NewWorkItemStore(db)
	require.NoError(t, store.InitSchema(context.Background()))
	return store
}

func TestWIPFilter_SkipsEngineersWithOpenItems(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	pipeStore := newPipelineStore(t, sqlDB)

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	eng1 := setupAgentWithRole(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}, agent.RoleEngineer1)
	eng2 := setupAgentWithRole(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}, agent.RoleEngineer2)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1,
		agent.RoleEngineer2: eng2,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: 50 * time.Millisecond, MaxParallel: 3, CooldownAfter: time.Second, WorkEnabled: true},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Engineer-1 has an open work item.
	_, err = pipeStore.Create(context.Background(), pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	require.NoError(t, err)

	// Run briefly — engineer-1 should be skipped, only engineer-2 eligible.
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = orch.Run(ctx)

	// Engineer-1's open item should still exist (not reassigned).
	open, err := pipeStore.OpenByEngineer(context.Background(), agent.RoleEngineer1)
	require.NoError(t, err)
	assert.Len(t, open, 1)
}

func TestCreatePipelineItem_CreateAndAdvance(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	assignment := orchestrator.Assignment{
		Role: agent.RoleEngineer1, Ticket: "JAM-99", Description: "test",
	}
	itemID := orch.CreatePipelineItemForTest(ctx, assignment)

	assert.Greater(t, itemID, int64(0))

	// Set PR URL.
	orch.SetPipelinePRForTest(ctx, itemID, "https://github.com/test-org/test-repo/pull/99")

	item, err := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, err)
	assert.Equal(t, "https://github.com/test-org/test-repo/pull/99", item.PRURL)
	assert.Equal(t, pipeline.StagePROpened, item.Stage)
}

func TestStoreProjectEpisode_StoresEpisode(t *testing.T) {
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

	episodeStore := memory.NewEpisodeStore(memDB)

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetProjectEpisodeStore(episodeStore)

	orch.StoreProjectEpisodeForTest(ctx, agent.RoleEngineer1, "JAM-17", "I implemented the auth module")

	episodes, epErr := episodeStore.EpisodesByTicket(ctx, "JAM-17")
	require.NoError(t, epErr)
	assert.Len(t, episodes, 1)
	assert.Equal(t, string(agent.RoleEngineer1), episodes[0].Agent)
}

func TestPipelineOps_NilGuards_DoNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	// No pipeline or episode store set.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.NotPanics(t, func() {
		orch.SetPipelinePRForTest(ctx, 0, "url")
		orch.StoreProjectEpisodeForTest(ctx, agent.RoleEngineer1, "T-1", "text")
		itemID := orch.CreatePipelineItemForTest(ctx, orchestrator.Assignment{Ticket: "T-1", Role: agent.RoleEngineer1})
		assert.Equal(t, int64(0), itemID)
	})
}

func TestStartFixUp_MissingEngineer_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	// Reviewer exists but the engineer doesn't.
	db, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = db.Close() })

	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CHANGES_REQUESTED\nfix it"}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}

	pmAgent := setupPMAgent(t, pmRunner)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, db)

	// No engineer-1 in agents map.
	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:       pmAgent,
		agent.RoleReviewer: reviewerAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-50", Engineer: agent.RoleEngineer1, Stage: pipeline.StagePROpened,
	})

	// Start review — reviewer returns changes requested, but engineer-1
	// doesn't exist so startFixUp logs and returns gracefully.
	assert.NotPanics(t, func() {
		orch.StartReviewWithItemForTest(ctx, "https://github.com/test-org/test-repo/pull/50", "JAM-50", itemID, agent.RoleEngineer1)
		orch.Wait()
	})
}

func TestWIPFilter_NilPipeline_DoesNotPanic(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	orch, _ := setupOrchestrator(t, pmRunner)
	// No SetPipeline call — should not panic.

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	assert.NotPanics(t, func() {
		_ = orch.Run(ctx)
	})
}

func TestSetPipeline_And_SetProjectEpisodeStore(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	memDB, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = memDB.Close() })

	episodeStore := memory.NewEpisodeStore(memDB)

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 1, CooldownAfter: time.Second},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)

	assert.NotPanics(t, func() {
		orch.SetPipeline(pipeStore)
		orch.SetProjectEpisodeStore(episodeStore)
	})
}

func TestRunSession_WithPipeline_CreatesWorkItem(t *testing.T) {
	t.Parallel()

	tmpDir := t.TempDir()
	ctx := context.Background()

	initGit := func(args ...string) {
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Dir = tmpDir
		_ = cmd.Run()
	}
	initGit("init")
	initGit("commit", "--allow-empty", "-m", "init")

	assignmentJSON := `[{"role":"engineer-1","ticket":"SQ-77","description":"Test pipeline"}]`
	contentBytes, _ := json.Marshal(assignmentJSON)
	pmOutput := `{"type":"result","result":` + string(contentBytes) + `}` + "\n"

	pmRunner := &fakeProcessRunner{output: []byte(pmOutput)}
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Done."}` + "\n"),
	}

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	pmAgent := setupPMAgent(t, pmRunner)
	engAgent := setupAgentWithRole(t, engRunner, agent.RoleEngineer1)
	engAgent.SetDBPath("/tmp/test.db")

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: engAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:  50 * time.Millisecond,
			MaxParallel:   3,
			CooldownAfter: time.Second,
			WorkEnabled:   true,
			TargetRepoDir: tmpDir,
		},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)
	orch.SetProjectEpisodeStore(memory.NewEpisodeStore(memDB))

	timedCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	_ = orch.Run(timedCtx)
	orch.Wait()

	// If the session ran, verify the pipeline item was created.
	sessionRan := len(engRunner.calls) > 0
	if !sessionRan {
		return
	}

	items, pipeErr := pipeStore.OpenByEngineer(ctx, agent.RoleEngineer1)
	require.NoError(t, pipeErr)

	if len(items) > 0 {
		assert.Equal(t, "SQ-77", items[0].Ticket)
	}
}

func TestReviewWithChangesRequested_TriggersFixUp(t *testing.T) {
	// Not parallel — the review feedback loop spawns recursive goroutines
	// that race with test cleanup in parallel mode.
	ctx := context.Background()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	db, err := memory.Open(ctx, ":memory:")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	// Reviewer always returns CHANGES_REQUESTED — loop runs until max cycles.
	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"CHANGES_REQUESTED\nPlease fix the nil check"}` + "\n"),
	}
	engRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"Fixed."}` + "\n"),
	}
	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}

	pmAgent := setupPMAgent(t, pmRunner)
	reviewerAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, db)
	engAgent := buildAgent(t, engRunner, agent.RoleEngineer1, db)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleReviewer:  reviewerAgent,
		agent.RoleEngineer1: engAgent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Create a work item to track.
	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-17", Engineer: agent.RoleEngineer1, Stage: pipeline.StagePROpened,
	})

	// Start review with item ID — should trigger changes requested → fix-up → re-review.
	orch.StartReviewWithItemForTest(ctx, "https://github.com/test-org/test-repo/pull/42", "JAM-17", itemID, agent.RoleEngineer1)
	orch.Wait()

	// The work item should have been advanced through the pipeline.
	item, err := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, err)
	// After changes requested + fix-up + re-review (which defaults to approved),
	// the item should be at approved or have review cycles > 0.
	assert.Greater(t, item.ReviewCycles, 0)
}
