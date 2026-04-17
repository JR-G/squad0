package orchestrator_test

import (
	"sort"
	"testing"

	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
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
