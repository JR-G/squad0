package orchestrator_test

import (
	"context"
	"database/sql"
	"testing"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/memory"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newPMDutiesOrchestrator(t *testing.T) (*orchestrator.Orchestrator, *orchestrator.SituationQueue, *fakeProcessRunner) {
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

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"1. Callum, merge your approved PR for JAM-1. 2. JAM-3 needs investigation."}` + "\n"),
	}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, nil,
	)

	queue := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(queue)

	return orch, queue, pmRunner
}

func TestProcessSituations_Empty_NoCalls(t *testing.T) {
	t.Parallel()

	orch, _, pmRunner := newPMDutiesOrchestrator(t)

	// No situations queued — PM should not be called.
	orch.RunPMDuties(context.Background())

	pmRunner.mu.Lock()
	calls := len(pmRunner.calls)
	pmRunner.mu.Unlock()

	assert.Equal(t, 0, calls, "empty queue should not trigger PM session")
}

func TestProcessSituations_WithSituations_CallsPM(t *testing.T) {
	t.Parallel()

	orch, queue, pmRunner := newPMDutiesOrchestrator(t)

	queue.Push(orchestrator.Situation{
		Type:        orchestrator.SitUnmergedApprovedPR,
		Severity:    orchestrator.SeverityInfo,
		Engineer:    agent.RoleEngineer1,
		Ticket:      "JAM-1",
		Description: "Callum has approved PR sitting unmerged",
	})

	orch.RunPMDuties(context.Background())

	pmRunner.mu.Lock()
	calls := len(pmRunner.calls)
	pmRunner.mu.Unlock()

	assert.GreaterOrEqual(t, calls, 1, "PM should be called with situations")
	assert.Equal(t, 0, queue.Len(), "queue should be drained after processing")
}

func TestProcessSituations_CriticalSituation_TrackedForEscalation(t *testing.T) {
	t.Parallel()

	orch, queue, _ := newPMDutiesOrchestrator(t)

	tracker := orchestrator.NewEscalationTracker()
	orch.SetEscalationTracker(tracker)

	queue.Push(orchestrator.Situation{
		Type:        orchestrator.SitRepeatedFailure,
		Severity:    orchestrator.SeverityCritical,
		Ticket:      "JAM-CRIT",
		Description: "JAM-CRIT has failed 5 times",
	})

	orch.RunPMDuties(context.Background())

	assert.Equal(t, 1, tracker.Len(), "critical situation should be tracked for escalation")
}

func TestProcessSituations_InfoSituation_NotTracked(t *testing.T) {
	t.Parallel()

	orch, queue, _ := newPMDutiesOrchestrator(t)

	tracker := orchestrator.NewEscalationTracker()
	orch.SetEscalationTracker(tracker)

	queue.Push(orchestrator.Situation{
		Type:        orchestrator.SitUnmergedApprovedPR,
		Severity:    orchestrator.SeverityInfo,
		Ticket:      "JAM-INFO",
		Description: "info level situation",
	})

	orch.RunPMDuties(context.Background())

	assert.Equal(t, 0, tracker.Len(), "info situations should not be tracked for escalation")
}

func TestProcessSituations_PMError_DoesNotPanic(t *testing.T) {
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

	// PM that errors.
	pmRunner := &fakeProcessRunner{err: assert.AnError}
	pmAgent := buildAgent(t, pmRunner, agent.RolePM, memDB)

	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{agent.RolePM: pmAgent},
		checkIns, nil, nil,
	)

	queue := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(queue)

	queue.Push(orchestrator.Situation{
		Type: orchestrator.SitStaleWorkingAgent, Ticket: "JAM-ERR",
	})

	// Should not panic even when PM errors.
	orch.RunPMDuties(ctx)
}

func TestProcessSituations_NoPMAgent_DoesNotPanic(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sqlDB, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	checkIns := coordination.NewCheckInStore(sqlDB)
	require.NoError(t, checkIns.InitSchema(ctx))

	// No PM agent.
	orch := orchestrator.NewOrchestrator(
		orchestrator.Config{},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)

	queue := orchestrator.NewSituationQueue()
	orch.SetSituationQueue(queue)
	queue.Push(orchestrator.Situation{Type: orchestrator.SitPipelineDrift, Ticket: "JAM-NoPM"})

	orch.RunPMDuties(ctx)
}
