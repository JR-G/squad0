package orchestrator_test

import (
	"database/sql"
	"sort"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBlockedTickets_Block_Then_IsBlocked_True(t *testing.T) {
	t.Parallel()
	b := orchestrator.NewBlockedTickets()

	b.Block("JAM-42")

	assert.True(t, b.IsBlocked("JAM-42"))
	assert.False(t, b.IsBlocked("JAM-99"))
}

func TestBlockedTickets_Clear_Removes(t *testing.T) {
	t.Parallel()
	b := orchestrator.NewBlockedTickets()

	b.Block("JAM-1")
	b.Clear("JAM-1")

	assert.False(t, b.IsBlocked("JAM-1"))
}

func TestBlockedTickets_Snapshot_ReturnsAllBlocked(t *testing.T) {
	t.Parallel()
	b := orchestrator.NewBlockedTickets()
	b.Block("JAM-1")
	b.Block("JAM-2")
	b.Block("JAM-3")

	got := b.Snapshot()
	sort.Strings(got)

	assert.Equal(t, []string{"JAM-1", "JAM-2", "JAM-3"}, got)
}

func TestBlockedTickets_Snapshot_Empty(t *testing.T) {
	t.Parallel()
	b := orchestrator.NewBlockedTickets()

	assert.Empty(t, b.Snapshot())
}

func TestOrchestrator_BlockedTickets_ReturnsSet(t *testing.T) {
	t.Parallel()
	orch := newMinimalOrchestrator(t)

	assert.NotNil(t, orch.BlockedTickets())
	orch.BlockedTickets().Block("JAM-42")
	assert.True(t, orch.BlockedTickets().IsBlocked("JAM-42"))
}

func TestOrchestrator_ReconcileGitHubState_NoPipelineStore_Noop(t *testing.T) {
	t.Parallel()
	orch := newMinimalOrchestrator(t)

	assert.NotPanics(t, func() {
		orch.ReconcileGitHubState(t.Context())
	})
}

func TestOrchestrator_ReconcileGitHubState_NoTargetRepoDir_Noop(t *testing.T) {
	t.Parallel()
	orch := newMinimalOrchestrator(t)

	assert.NotPanics(t, func() {
		orch.ReconcileGitHubState(t.Context())
	})
}

func TestOrchestrator_ForceAdvancePipeline_NoStore_NoPanic(t *testing.T) {
	t.Parallel()
	orch := newMinimalOrchestrator(t)

	// No pipeline store wired — should be a no-op, not a panic.
	assert.NotPanics(t, func() {
		orch.ForceAdvancePipelineForTest(t.Context(), 0, "merged", "test")
	})
}

// TestOrchestrator_ReconcileGitHubState_BadRepoDir_HandlesFetchError
// exercises the fetcher-dispatch path (both pipeline store + target
// repo dir present) so the NewGHPRStateFetcher call and its error
// handling run. The fetcher shells out to gh in a non-repo dir,
// which fails gracefully — the orchestrator must not panic.
func TestOrchestrator_ReconcileGitHubState_BadRepoDir_HandlesFetchError(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := newPipelineStore(t, sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{TargetRepoDir: t.TempDir()},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	assert.NotPanics(t, func() {
		orch.ReconcileGitHubState(ctx)
	})
}
