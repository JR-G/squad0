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

func newSensorOrchestrator(t *testing.T) (*orchestrator.Orchestrator, *pipeline.WorkItemStore, *orchestrator.SituationQueue) {
	t.Helper()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	memDB, memErr := memory.Open(ctx, ":memory:")
	require.NoError(t, memErr)
	t.Cleanup(func() { _ = memDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
	}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	queue := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(queue)

	return orch, pipeStore, queue
}

func TestSenseUnmergedApproved_StaleApprovedPR_PushesSituation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, queue := newSensorOrchestrator(t)

	itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-AP1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test/repo/pull/1"))
	require.NoError(t, pipeStore.Advance(ctx, itemID, pipeline.StageApproved))

	// Backdate the item to make it stale.
	_, execErr := pipeStore.DB().ExecContext(ctx, `UPDATE work_items SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-20*time.Minute), itemID)
	require.NoError(t, execErr)

	orch.RunSensorsForTest(t)

	assert.Equal(t, 1, queue.Len(), "should detect stale approved PR")
}

func TestSenseStaleWorking_LongWorkingNoPR_PushesSituation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, queue := newSensorOrchestrator(t)

	itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-SW1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, err)

	// Backdate to stale.
	_, execErr := pipeStore.DB().ExecContext(ctx, `UPDATE work_items SET updated_at = ? WHERE id = ?`,
		time.Now().Add(-60*time.Minute), itemID)
	require.NoError(t, execErr)

	orch.RunSensorsForTest(t)

	assert.Equal(t, 1, queue.Len(), "should detect stale working item")
}

func TestSensePipelineDrift_IdleWithOpenPR_PushesSituation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, queue := newSensorOrchestrator(t)

	itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-PD1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageReviewing,
	})
	require.NoError(t, err)
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test/repo/pull/2"))

	// Engineer must be idle for drift to be detected.
	require.NoError(t, orch.CheckIns().SetIdle(ctx, agent.RoleEngineer1))

	orch.RunSensorsForTest(t)

	assert.Equal(t, 1, queue.Len(), "should detect pipeline drift")
}

func TestSenseRepeatedFailures_MultipleFails_PushesSituation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, queue := newSensorOrchestrator(t)

	// Create 3 failed items for the same ticket.
	for range 3 {
		itemID, err := pipeStore.Create(ctx, pipeline.WorkItem{
			Ticket: "JAM-RF1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
		})
		require.NoError(t, err)
		require.NoError(t, pipeStore.Advance(ctx, itemID, pipeline.StageFailed))
	}

	orch.RunSensorsForTest(t)

	assert.Equal(t, 1, queue.Len(), "should detect repeated failures")
}

func TestSensors_FreshItems_NothingQueued(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	orch, pipeStore, queue := newSensorOrchestrator(t)

	// Create items that are fresh — should NOT trigger.
	_, err := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-FR1", Engineer: agent.RoleEngineer1, Stage: pipeline.StageWorking,
	})
	require.NoError(t, err)

	orch.RunSensorsForTest(t)

	assert.Equal(t, 0, queue.Len(), "fresh items should not trigger sensors")
}

func TestSensors_EscalationTracker_Set(t *testing.T) {
	t.Parallel()

	orch, _, _ := newSensorOrchestrator(t)
	tracker := orchestrator.NewEscalationTracker()
	orch.SetEscalationTracker(tracker)

	// Should not panic with escalation tracker set.
	orch.RunSensorsForTest(t)
}
