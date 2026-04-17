package pipeline_test

import (
	"errors"
	"testing"

	"github.com/JR-G/squad0/internal/pipeline"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCanTransition_LegalLinearPath(t *testing.T) {
	t.Parallel()

	chain := []pipeline.Stage{
		pipeline.StageAssigned,
		pipeline.StageWorking,
		pipeline.StagePROpened,
		pipeline.StageReviewing,
		pipeline.StageApproved,
		pipeline.StageMerged,
	}

	for i := 0; i < len(chain)-1; i++ {
		assert.NoError(t, pipeline.CanTransition(chain[i], chain[i+1]),
			"step %d: %s → %s should be legal", i, chain[i], chain[i+1])
	}
}

func TestCanTransition_Idempotent_AlwaysAllowedForLiveStages(t *testing.T) {
	t.Parallel()

	live := []pipeline.Stage{
		pipeline.StageAssigned, pipeline.StageWorking, pipeline.StagePROpened,
		pipeline.StageReviewing, pipeline.StageChangesRequested, pipeline.StageApproved,
	}

	for _, stage := range live {
		assert.NoError(t, pipeline.CanTransition(stage, stage),
			"%s → %s should be idempotent and legal", stage, stage)
	}
}

func TestCanTransition_TerminalStages_RejectAllMoves(t *testing.T) {
	t.Parallel()

	for _, terminal := range []pipeline.Stage{pipeline.StageMerged, pipeline.StageFailed} {
		err := pipeline.CanTransition(terminal, pipeline.StageWorking)
		require.Error(t, err, "%s should refuse to move", terminal)
		assert.ErrorIs(t, err, pipeline.ErrIllegalTransition)
	}
}

func TestCanTransition_WorkingDirectlyToApproved_Rejected(t *testing.T) {
	t.Parallel()

	// Skipping review must not be possible via the lifecycle alone.
	err := pipeline.CanTransition(pipeline.StageWorking, pipeline.StageApproved)

	require.Error(t, err)
	assert.ErrorIs(t, err, pipeline.ErrIllegalTransition)
}

func TestCanTransition_ApprovedRevertingToReviewing_Allowed(t *testing.T) {
	t.Parallel()

	// JAM-24-style legitimate revert: a fresh blocking comment after
	// approval pushes the PR back into review. The supersession check
	// (in the orchestrator) decides whether the revert is justified;
	// the lifecycle just permits the move.
	assert.NoError(t, pipeline.CanTransition(pipeline.StageApproved, pipeline.StageReviewing))
}

func TestCanTransition_AssignedDirectlyToFailed_Allowed(t *testing.T) {
	t.Parallel()

	// Worktree creation failure or immediate session death should be
	// able to abandon the ticket without faking a working transition.
	assert.NoError(t, pipeline.CanTransition(pipeline.StageAssigned, pipeline.StageFailed))
}

func TestCanTransition_ChangesRequestedToWorking_Allowed(t *testing.T) {
	t.Parallel()

	// The most common live path: reviewer asks for fixes, engineer
	// resumes work.
	assert.NoError(t, pipeline.CanTransition(pipeline.StageChangesRequested, pipeline.StageWorking))
}

func TestCanTransition_UnknownStages_Rejected(t *testing.T) {
	t.Parallel()

	err := pipeline.CanTransition("never_existed", pipeline.StageWorking)
	require.Error(t, err)
	assert.ErrorIs(t, err, pipeline.ErrIllegalTransition)
	assert.Contains(t, err.Error(), "not recognised")

	err = pipeline.CanTransition(pipeline.StageWorking, "made_up")
	require.Error(t, err)
	assert.ErrorIs(t, err, pipeline.ErrIllegalTransition)
	assert.Contains(t, err.Error(), "not recognised")
}

func TestLegalNextStages_ReturnsCopy(t *testing.T) {
	t.Parallel()

	stages := pipeline.LegalNextStages(pipeline.StageReviewing)
	require.NotEmpty(t, stages)

	// Mutating the returned slice must not affect the internal table.
	stages[0] = "polluted"
	fresh := pipeline.LegalNextStages(pipeline.StageReviewing)
	assert.NotEqual(t, "polluted", fresh[0])
}

func TestLegalNextStages_TerminalStage_ReturnsEmpty(t *testing.T) {
	t.Parallel()

	assert.Empty(t, pipeline.LegalNextStages(pipeline.StageMerged))
	assert.Empty(t, pipeline.LegalNextStages(pipeline.StageFailed))
}

func TestLegalNextStages_UnknownStage_ReturnsNil(t *testing.T) {
	t.Parallel()

	assert.Nil(t, pipeline.LegalNextStages("nonsense"))
}

func TestErrIllegalTransition_IsSentinel(t *testing.T) {
	t.Parallel()

	err := pipeline.CanTransition(pipeline.StageMerged, pipeline.StageWorking)
	require.Error(t, err)
	assert.True(t, errors.Is(err, pipeline.ErrIllegalTransition),
		"errors.Is should match the sentinel for branch-friendly callers")
}
