package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/JR-G/squad0/internal/pipeline"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTriggerPendingReviews_ReviewerIdle_StartsReview(t *testing.T) {
	t.Parallel()

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

	// Create a PR in pr_opened stage.
	itemID, _ := pipeStore.Create(ctx, pipeline.WorkItem{
		Ticket: "JAM-PR1", Engineer: agent.RoleEngineer1, Stage: pipeline.StagePROpened,
	})
	require.NoError(t, pipeStore.SetPRURL(ctx, itemID, "https://github.com/test/repo/pull/1"))

	reviewRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"APPROVED"}` + "\n"),
	}
	reviewAgent := buildAgent(t, reviewRunner, agent.RoleReviewer, memDB)

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"done"}` + "\n")}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{AcknowledgePause: 0},
		map[agent.Role]*agent.Agent{
			agent.RoleReviewer: reviewAgent,
			agent.RolePM:       pmAgent,
		},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	orch.TriggerPendingReviewsForTest(ctx, []agent.Role{agent.RoleReviewer})

	// startReview runs in a goroutine — wait for it.
	orch.Wait()

	reviewRunner.mu.Lock()
	callCount := len(reviewRunner.calls)
	reviewRunner.mu.Unlock()

	assert.GreaterOrEqual(t, callCount, 1, "reviewer should have been called")
}

func TestTriggerPendingReviews_NoReviewer_DoesNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	pipeStore := pipeline.NewWorkItemStore(sqlDB)
	require.NoError(t, pipeStore.InitSchema(ctx))

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
	orch.SetPipeline(pipeStore)

	// No reviewer in idle list — should not panic.
	orch.TriggerPendingReviewsForTest(ctx, []agent.Role{agent.RoleEngineer1})
}

func TestTriggerPendingReviews_NoPipeline_DoesNotPanic(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	orch.TriggerPendingReviewsForTest(context.Background(), []agent.Role{agent.RoleReviewer})
}

func TestContainsRole(t *testing.T) {
	t.Parallel()

	roles := []agent.Role{agent.RoleEngineer1, agent.RoleReviewer}
	assert.True(t, orchestrator.ContainsRoleForTest(roles, agent.RoleReviewer))
	assert.False(t, orchestrator.ContainsRoleForTest(roles, agent.RolePM))
}
