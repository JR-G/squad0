package pipeline_test

import (
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
	"github.com/stretchr/testify/assert"
)

func TestComputeStats_EmptyItems(t *testing.T) {
	t.Parallel()

	stats := pipeline.ComputeStats(agent.RoleEngineer1, nil)

	assert.Equal(t, 0, stats.Completed)
	assert.Equal(t, 0, stats.Failed)
}

func TestComputeStats_MixedOutcomes(t *testing.T) {
	t.Parallel()

	now := time.Now()
	oneHourAgo := now.Add(-time.Hour)
	twoHoursAgo := now.Add(-2 * time.Hour)

	items := []pipeline.WorkItem{
		{Stage: pipeline.StageMerged, StartedAt: twoHoursAgo, FinishedAt: &now, ReviewCycles: 1},
		{Stage: pipeline.StageMerged, StartedAt: oneHourAgo, FinishedAt: &now, ReviewCycles: 3},
		{Stage: pipeline.StageFailed, StartedAt: oneHourAgo, FinishedAt: &now, ReviewCycles: 0},
		{Stage: pipeline.StageWorking}, // Not terminal — skipped.
	}

	stats := pipeline.ComputeStats(agent.RoleEngineer1, items)

	assert.Equal(t, agent.RoleEngineer1, stats.Agent)
	assert.Equal(t, 2, stats.Completed)
	assert.Equal(t, 1, stats.Failed)
	assert.Greater(t, stats.AvgDuration, time.Duration(0))
	assert.InDelta(t, 1.33, stats.AvgReviewCycles, 0.1)
}

func TestComputeStats_AllFailed(t *testing.T) {
	t.Parallel()

	now := time.Now()
	items := []pipeline.WorkItem{
		{Stage: pipeline.StageFailed, StartedAt: now, FinishedAt: &now},
	}

	stats := pipeline.ComputeStats(agent.RoleEngineer1, items)

	assert.Equal(t, 0, stats.Completed)
	assert.Equal(t, 1, stats.Failed)
}
