package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckStaleWork_ApprovedItem_NudgesAfterThreshold(t *testing.T) {
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
	eng1Agent := setupAgentWithRole(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}, agent.RoleEngineer1)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1Agent,
	}

	server := newTestSlackServer()
	t.Cleanup(server.Close)
	bot := newTestSlackBot(server.URL)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, bot, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)
	orch.SetRoster(map[agent.Role]string{
		agent.RoleEngineer1: "Mara",
	})

	// Create an approved item that's been sitting for over 15 minutes.
	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-STALE-AP",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-stale-ap",
	})
	require.NoError(t, createErr)
	require.NoError(t, pipeStore.Advance(ctx, itemID, pipeline.StageApproved))

	// Age the item past the approved threshold.
	_, err = pipeStore.DB().ExecContext(ctx,
		`UPDATE work_items SET updated_at = datetime('now', '-20 minutes') WHERE id = ?`, itemID)
	require.NoError(t, err)

	// Run PM duties — should nudge.
	assert.NotPanics(t, func() {
		orch.RunPMDuties(ctx)
	})
}

func TestCheckStaleWork_RecentApproved_DoesNotNudge(t *testing.T) {
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
	eng1Agent := setupAgentWithRole(t, &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}, agent.RoleEngineer1)

	agents := map[agent.Role]*agent.Agent{
		agent.RolePM:        pmAgent,
		agent.RoleEngineer1: eng1Agent,
	}

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{PollInterval: time.Second, MaxParallel: 3, CooldownAfter: time.Second, AcknowledgePause: time.Millisecond},
		agents, checkIns, nil, orchestrator.NewAssigner(pmAgent, "TEST"),
	)
	orch.SetPipeline(pipeStore)

	// Create a recently-approved item (not stale yet).
	itemID, createErr := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket:   "JAM-RECENT-AP",
		Engineer: agent.RoleEngineer1,
		Stage:    pipeline.StageWorking,
		Branch:   "feat/jam-recent-ap",
	})
	require.NoError(t, createErr)
	require.NoError(t, pipeStore.Advance(ctx, itemID, pipeline.StageApproved))

	// Run PM duties — should NOT nudge (item is recent).
	assert.NotPanics(t, func() {
		orch.RunPMDuties(ctx)
	})

	// Item should still be approved (not touched).
	item, getErr := pipeStore.GetByID(ctx, itemID)
	require.NoError(t, getErr)
	assert.Equal(t, pipeline.StageApproved, item.Stage)
}

func TestBuildEngineerMergePrompt_ContainsMergeCommands(t *testing.T) {
	t.Parallel()

	prompt := orchestrator.BuildEngineerMergePrompt(
		"https://github.com/test-org/test-repo/pull/42",
		"JAM-7",
	)

	assert.Contains(t, prompt, "JAM-7")
	assert.Contains(t, prompt, "https://github.com/test-org/test-repo/pull/42")
	assert.Contains(t, prompt, "gh pr view https://github.com/test-org/test-repo/pull/42 --comments")
	assert.Contains(t, prompt, "gh pr checks https://github.com/test-org/test-repo/pull/42")
	assert.Contains(t, prompt, "gh pr merge https://github.com/test-org/test-repo/pull/42 --squash --delete-branch")
	assert.Contains(t, prompt, "gh pr view https://github.com/test-org/test-repo/pull/42 --json state --jq .state")
	assert.Contains(t, prompt, "approved")
}

func TestBuildEngineerMergePrompt_UsesURLForGHCommands(t *testing.T) {
	t.Parallel()

	prompt := orchestrator.BuildEngineerMergePrompt(
		"https://github.com/test-org/test-repo/pull/42",
		"JAM-7",
	)

	// gh commands use full URL, not bare number.
	assert.NotContains(t, prompt, "gh pr merge 42")
	assert.NotContains(t, prompt, "gh pr view 42")
	assert.NotContains(t, prompt, "gh pr checks 42")
}
