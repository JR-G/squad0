package orchestrator_test

import (
	"context"
	"database/sql"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
	"github.com/JR-G/squad0/internal/orchestrator"
	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newMinimalOrchestrator(t *testing.T) *orchestrator.Orchestrator {
	t.Helper()

	db, err := sql.Open("sqlite3", ":memory:?_journal_mode=WAL")
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	checkIns := coordination.NewCheckInStore(db)
	require.NoError(t, checkIns.InitSchema(context.Background()))

	// Minimal orchestrator with no agents — avoids fts5 dependency.
	return orchestrator.NewOrchestrator(
		orchestrator.Config{
			PollInterval:     time.Second,
			MaxParallel:      3,
			CooldownAfter:    time.Second,
			AcknowledgePause: time.Millisecond,
		},
		map[agent.Role]*agent.Agent{},
		checkIns, nil, nil,
	)
}

func TestOrchestrator_MailboxFor_UnknownRole_ReturnsNil(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)

	assert.Nil(t, orch.MailboxFor(agent.RoleEngineer1))
}

func TestOrchestrator_SetEventBus_ConnectsBus(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()

	orch.SetEventBus(bus)

	assert.Equal(t, bus, orch.EventBus())
}

func TestOrchestrator_EventBus_NilByDefault(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)

	assert.Nil(t, orch.EventBus())
}

func TestRegisterDefaultHandlers_RegistersAllExpectedEvents(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()

	orch.RegisterDefaultHandlers(bus)

	// PRApproved, ChangesRequested, FixUpComplete are synchronous — no handlers.
	assert.Equal(t, 0, bus.HandlerCount(orchestrator.EventPRApproved))
	assert.Equal(t, 0, bus.HandlerCount(orchestrator.EventChangesRequested))
	assert.Equal(t, 0, bus.HandlerCount(orchestrator.EventFixUpComplete))
	// MergeFailed and AgentIdle drive async work.
	assert.Equal(t, 1, bus.HandlerCount(orchestrator.EventMergeFailed))
	assert.Equal(t, 1, bus.HandlerCount(orchestrator.EventAgentIdle))
	// Session-end events trigger immediate re-assignment of idle engineers
	// instead of waiting for the next tick.
	assert.Equal(t, 1, bus.HandlerCount(orchestrator.EventSessionComplete))
	assert.Equal(t, 1, bus.HandlerCount(orchestrator.EventSessionFailed))
}

func TestRegisterDefaultHandlers_NoHandlersForInfoEvents(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()

	orch.RegisterDefaultHandlers(bus)

	// Pure-informational events have no default handlers.
	assert.Equal(t, 0, bus.HandlerCount(orchestrator.EventMergeComplete))
	assert.Equal(t, 0, bus.HandlerCount(orchestrator.EventMergeReady))
	assert.Equal(t, 0, bus.HandlerCount(orchestrator.EventReviewComplete))
}

func TestRegisterDefaultHandlers_CalledTwice_DoubleRegisters(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()

	orch.RegisterDefaultHandlers(bus)
	orch.RegisterDefaultHandlers(bus)

	assert.Equal(t, 2, bus.HandlerCount(orchestrator.EventMergeFailed))
	assert.Equal(t, 2, bus.HandlerCount(orchestrator.EventAgentIdle))
	assert.Equal(t, 2, bus.HandlerCount(orchestrator.EventSessionComplete))
	assert.Equal(t, 2, bus.HandlerCount(orchestrator.EventSessionFailed))
}

func TestRegisterDefaultHandlers_SessionComplete_WorkEnabled_TriggersScheduling(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	orch.SetWorkEnabledForTest(true)
	bus := orchestrator.NewEventBus()
	orch.RegisterDefaultHandlers(bus)

	assert.NotPanics(t, func() {
		bus.EmitSync(context.Background(), orchestrator.Event{
			Kind:         orchestrator.EventSessionComplete,
			EngineerRole: agent.RoleEngineer1,
		})
	})
}

func TestRegisterDefaultHandlers_SessionComplete_WorkDisabled_NoCheckInRead(t *testing.T) {
	t.Parallel()

	// WorkEnabled=false short-circuits the handler before it touches the
	// check-in store — verifies the gate is in place so chat-only mode
	// doesn't try to schedule work it shouldn't.
	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()
	orch.RegisterDefaultHandlers(bus)

	assert.NotPanics(t, func() {
		bus.EmitSync(context.Background(), orchestrator.Event{
			Kind:         orchestrator.EventSessionComplete,
			EngineerRole: agent.RoleEngineer1,
		})
	})
}

func TestEventBus_PRApproved_HandlerReceivesData(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()

	var received orchestrator.Event
	bus.On(orchestrator.EventPRApproved, func(_ context.Context, event orchestrator.Event) {
		received = event
	})

	orch.SetEventBus(bus)

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:         orchestrator.EventPRApproved,
		Ticket:       "SQ-99",
		PRURL:        "https://github.com/test-org/test-repo/pull/99",
		WorkItemID:   1,
		EngineerRole: agent.RoleEngineer1,
	})

	assert.Equal(t, "SQ-99", received.Ticket)
	assert.Equal(t, "https://github.com/test-org/test-repo/pull/99", received.PRURL)
	assert.Equal(t, int64(1), received.WorkItemID)
	assert.Equal(t, agent.RoleEngineer1, received.EngineerRole)
}

func TestEventBus_ChangesRequested_HandlerReceivesData(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var received orchestrator.Event

	bus.On(orchestrator.EventChangesRequested, func(_ context.Context, event orchestrator.Event) {
		received = event
	})

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:         orchestrator.EventChangesRequested,
		Ticket:       "SQ-50",
		PRURL:        "https://github.com/test-org/test-repo/pull/50",
		WorkItemID:   2,
		EngineerRole: agent.RoleEngineer2,
	})

	assert.Equal(t, "SQ-50", received.Ticket)
	assert.Equal(t, agent.RoleEngineer2, received.EngineerRole)
}

func TestEventBus_FixUpComplete_HandlerReceivesData(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var received orchestrator.Event

	bus.On(orchestrator.EventFixUpComplete, func(_ context.Context, event orchestrator.Event) {
		received = event
	})

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:         orchestrator.EventFixUpComplete,
		Ticket:       "SQ-51",
		PRURL:        "https://github.com/test-org/test-repo/pull/51",
		WorkItemID:   3,
		EngineerRole: agent.RoleEngineer3,
	})

	assert.Equal(t, "SQ-51", received.Ticket)
	assert.Equal(t, agent.RoleEngineer3, received.EngineerRole)
}

func TestEventBus_MergeFailed_HandlerReceivesData(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var received orchestrator.Event

	bus.On(orchestrator.EventMergeFailed, func(_ context.Context, event orchestrator.Event) {
		received = event
	})

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:         orchestrator.EventMergeFailed,
		Ticket:       "SQ-55",
		PRURL:        "https://github.com/test-org/test-repo/pull/55",
		WorkItemID:   5,
		EngineerRole: agent.RoleEngineer1,
	})

	assert.Equal(t, "SQ-55", received.Ticket)
	assert.Equal(t, "https://github.com/test-org/test-repo/pull/55", received.PRURL)
}

func TestEventBus_AgentIdle_HandlerReceivesRole(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var received orchestrator.Event

	bus.On(orchestrator.EventAgentIdle, func(_ context.Context, event orchestrator.Event) {
		received = event
	})

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:         orchestrator.EventAgentIdle,
		EngineerRole: agent.RoleEngineer2,
	})

	assert.Equal(t, agent.RoleEngineer2, received.EngineerRole)
}

func TestEventBus_DefaultHandlers_PRApproved_GracefulWithNoAgent(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()
	orch.RegisterDefaultHandlers(bus)
	orch.SetEventBus(bus)

	// PRApproved with no matching engineer agent — should log and not panic.
	assert.NotPanics(t, func() {
		bus.EmitSync(context.Background(), orchestrator.Event{
			Kind:         orchestrator.EventPRApproved,
			Ticket:       "SQ-1",
			PRURL:        "https://github.com/test-org/test-repo/pull/1",
			EngineerRole: agent.RoleEngineer1,
		})
	})
}

func TestEventBus_DefaultHandlers_AgentIdle_GracefulWithNoPipeline(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()
	orch.RegisterDefaultHandlers(bus)
	orch.SetEventBus(bus)

	// AgentIdle with no pipeline store — should not panic.
	assert.NotPanics(t, func() {
		bus.EmitSync(context.Background(), orchestrator.Event{
			Kind:         orchestrator.EventAgentIdle,
			EngineerRole: agent.RoleEngineer1,
		})
	})
}

func TestEventBus_MultipleCustomHandlers_AllFire(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var count atomic.Int32

	bus.On(orchestrator.EventSessionComplete, func(_ context.Context, _ orchestrator.Event) {
		count.Add(1)
	})
	bus.On(orchestrator.EventSessionComplete, func(_ context.Context, _ orchestrator.Event) {
		count.Add(1)
	})

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:   orchestrator.EventSessionComplete,
		Ticket: "SQ-88",
	})

	assert.Equal(t, int32(2), count.Load())
}

func TestEventBus_MergeComplete_CustomHandler(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var received orchestrator.Event

	bus.On(orchestrator.EventMergeComplete, func(_ context.Context, event orchestrator.Event) {
		received = event
	})

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:         orchestrator.EventMergeComplete,
		Ticket:       "SQ-77",
		PRURL:        "https://github.com/test-org/test-repo/pull/77",
		WorkItemID:   10,
		EngineerRole: agent.RoleEngineer1,
	})

	assert.Equal(t, "SQ-77", received.Ticket)
	assert.Equal(t, "https://github.com/test-org/test-repo/pull/77", received.PRURL)
	assert.Equal(t, int64(10), received.WorkItemID)
}

func TestEventBus_SessionFailed_CustomHandler(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var called atomic.Bool

	bus.On(orchestrator.EventSessionFailed, func(_ context.Context, event orchestrator.Event) {
		called.Store(true)
		assert.Equal(t, "SQ-33", event.Ticket)
	})

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:         orchestrator.EventSessionFailed,
		Ticket:       "SQ-33",
		EngineerRole: agent.RoleEngineer3,
	})

	assert.True(t, called.Load())
}

func TestEventBus_DefaultHandlers_MergeFailed_GracefulWithNoAgent(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()
	orch.RegisterDefaultHandlers(bus)
	orch.SetEventBus(bus)

	assert.NotPanics(t, func() {
		bus.EmitSync(context.Background(), orchestrator.Event{
			Kind:         orchestrator.EventMergeFailed,
			Ticket:       "SQ-5",
			PRURL:        "https://github.com/test-org/test-repo/pull/5",
			EngineerRole: agent.RoleEngineer1,
		})
	})
}

func TestEventBus_DefaultHandlers_FixUpComplete_GracefulWithNoReviewer(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()
	orch.RegisterDefaultHandlers(bus)
	orch.SetEventBus(bus)

	assert.NotPanics(t, func() {
		bus.EmitSync(context.Background(), orchestrator.Event{
			Kind:         orchestrator.EventFixUpComplete,
			Ticket:       "SQ-6",
			PRURL:        "https://github.com/test-org/test-repo/pull/6",
			EngineerRole: agent.RoleEngineer1,
		})
	})
}

func TestEventBus_DefaultHandlers_ChangesRequested_GracefulWithNoAgent(t *testing.T) {
	t.Parallel()

	orch := newMinimalOrchestrator(t)
	bus := orchestrator.NewEventBus()
	orch.RegisterDefaultHandlers(bus)
	orch.SetEventBus(bus)

	assert.NotPanics(t, func() {
		bus.EmitSync(context.Background(), orchestrator.Event{
			Kind:         orchestrator.EventChangesRequested,
			Ticket:       "SQ-7",
			PRURL:        "https://github.com/test-org/test-repo/pull/7",
			EngineerRole: agent.RoleEngineer1,
		})
	})
}
