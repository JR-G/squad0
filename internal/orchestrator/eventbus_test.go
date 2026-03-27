package orchestrator_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewEventBus_ReturnsNonNil(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	require.NotNil(t, bus)
}

func TestEventBus_On_RegistersHandler(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	bus.On(orchestrator.EventPRApproved, func(_ context.Context, _ orchestrator.Event) {})

	assert.Equal(t, 1, bus.HandlerCount(orchestrator.EventPRApproved))
}

func TestEventBus_On_MultipleHandlersSameKind(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	bus.On(orchestrator.EventMergeComplete, func(_ context.Context, _ orchestrator.Event) {})
	bus.On(orchestrator.EventMergeComplete, func(_ context.Context, _ orchestrator.Event) {})
	bus.On(orchestrator.EventMergeComplete, func(_ context.Context, _ orchestrator.Event) {})

	assert.Equal(t, 3, bus.HandlerCount(orchestrator.EventMergeComplete))
}

func TestEventBus_HandlerCount_ZeroForUnregistered(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	assert.Equal(t, 0, bus.HandlerCount(orchestrator.EventSessionFailed))
}

func TestEventBus_Emit_CallsHandler(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var called atomic.Bool

	bus.On(orchestrator.EventPRApproved, func(_ context.Context, _ orchestrator.Event) {
		called.Store(true)
	})

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:   orchestrator.EventPRApproved,
		Ticket: "SQ-1",
	})

	assert.True(t, called.Load())
}

func TestEventBus_Emit_PassesEventData(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var received orchestrator.Event

	bus.On(orchestrator.EventChangesRequested, func(_ context.Context, event orchestrator.Event) {
		received = event
	})

	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:         orchestrator.EventChangesRequested,
		Ticket:       "SQ-42",
		PRURL:        "https://github.com/test-org/test-repo/pull/7",
		WorkItemID:   99,
		EngineerRole: agent.RoleEngineer1,
	})

	assert.Equal(t, orchestrator.EventChangesRequested, received.Kind)
	assert.Equal(t, "SQ-42", received.Ticket)
	assert.Equal(t, "https://github.com/test-org/test-repo/pull/7", received.PRURL)
	assert.Equal(t, int64(99), received.WorkItemID)
	assert.Equal(t, agent.RoleEngineer1, received.EngineerRole)
}

func TestEventBus_Emit_SetsTimestamp(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var received orchestrator.Event

	bus.On(orchestrator.EventMergeComplete, func(_ context.Context, event orchestrator.Event) {
		received = event
	})

	before := time.Now()
	bus.EmitSync(context.Background(), orchestrator.Event{Kind: orchestrator.EventMergeComplete})
	after := time.Now()

	assert.False(t, received.Timestamp.IsZero())
	assert.True(t, received.Timestamp.After(before) || received.Timestamp.Equal(before))
	assert.True(t, received.Timestamp.Before(after) || received.Timestamp.Equal(after))
}

func TestEventBus_Emit_PreservesExplicitTimestamp(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var received orchestrator.Event

	bus.On(orchestrator.EventSessionComplete, func(_ context.Context, event orchestrator.Event) {
		received = event
	})

	explicit := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	bus.EmitSync(context.Background(), orchestrator.Event{
		Kind:      orchestrator.EventSessionComplete,
		Timestamp: explicit,
	})

	assert.Equal(t, explicit, received.Timestamp)
}

func TestEventBus_Emit_MultipleHandlersAllCalled(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var count atomic.Int32

	for range 3 {
		bus.On(orchestrator.EventAgentIdle, func(_ context.Context, _ orchestrator.Event) {
			count.Add(1)
		})
	}

	bus.EmitSync(context.Background(), orchestrator.Event{Kind: orchestrator.EventAgentIdle})

	assert.Equal(t, int32(3), count.Load())
}

func TestEventBus_Emit_NoHandlers_DoesNotPanic(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()

	assert.NotPanics(t, func() {
		bus.EmitSync(context.Background(), orchestrator.Event{Kind: orchestrator.EventMergeFailed})
	})
}

func TestEventBus_Emit_PanickingHandler_DoesNotBreakOthers(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var safeHandlerCalled atomic.Bool

	bus.On(orchestrator.EventFixUpComplete, func(_ context.Context, _ orchestrator.Event) {
		panic("deliberate test panic")
	})
	bus.On(orchestrator.EventFixUpComplete, func(_ context.Context, _ orchestrator.Event) {
		safeHandlerCalled.Store(true)
	})

	bus.EmitSync(context.Background(), orchestrator.Event{Kind: orchestrator.EventFixUpComplete})

	assert.True(t, safeHandlerCalled.Load())
}

func TestEventBus_Emit_DifferentKinds_IsolateHandlers(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var approvedCalled atomic.Bool
	var failedCalled atomic.Bool

	bus.On(orchestrator.EventPRApproved, func(_ context.Context, _ orchestrator.Event) {
		approvedCalled.Store(true)
	})
	bus.On(orchestrator.EventMergeFailed, func(_ context.Context, _ orchestrator.Event) {
		failedCalled.Store(true)
	})

	bus.EmitSync(context.Background(), orchestrator.Event{Kind: orchestrator.EventPRApproved})

	assert.True(t, approvedCalled.Load())
	assert.False(t, failedCalled.Load())
}

func TestEventBus_Emit_ConcurrentSafety(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var count atomic.Int32

	bus.On(orchestrator.EventReviewComplete, func(_ context.Context, _ orchestrator.Event) {
		count.Add(1)
	})

	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bus.EmitSync(context.Background(), orchestrator.Event{Kind: orchestrator.EventReviewComplete})
		}()
	}

	wg.Wait()
	assert.Equal(t, int32(50), count.Load())
}

func TestEventBus_ConcurrentOnAndEmit(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var count atomic.Int32

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(2)
		go func() {
			defer wg.Done()
			bus.On(orchestrator.EventSessionComplete, func(_ context.Context, _ orchestrator.Event) {
				count.Add(1)
			})
		}()
		go func() {
			defer wg.Done()
			bus.Emit(context.Background(), orchestrator.Event{Kind: orchestrator.EventSessionComplete})
		}()
	}

	wg.Wait()

	// We don't assert on count because registration and emission race,
	// but the test must not panic or deadlock.
	assert.GreaterOrEqual(t, bus.HandlerCount(orchestrator.EventSessionComplete), 1)
}

func TestEventBus_Emit_PassesContext(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	type ctxKey string
	var received string

	bus.On(orchestrator.EventMergeReady, func(ctx context.Context, _ orchestrator.Event) {
		val, ok := ctx.Value(ctxKey("test")).(string)
		if ok {
			received = val
		}
	})

	ctx := context.WithValue(context.Background(), ctxKey("test"), "hello")
	bus.EmitSync(ctx, orchestrator.Event{Kind: orchestrator.EventMergeReady})

	assert.Equal(t, "hello", received)
}

func TestEventBus_Emit_Async_EventuallyCallsHandler(t *testing.T) {
	t.Parallel()

	bus := orchestrator.NewEventBus()
	var called atomic.Bool

	bus.On(orchestrator.EventSessionFailed, func(_ context.Context, _ orchestrator.Event) {
		called.Store(true)
	})

	bus.Emit(context.Background(), orchestrator.Event{Kind: orchestrator.EventSessionFailed})

	require.Eventually(t, called.Load, time.Second, 5*time.Millisecond)
}

func TestEventKind_Constants_AreDistinct(t *testing.T) {
	t.Parallel()

	kinds := []orchestrator.EventKind{
		orchestrator.EventPRApproved,
		orchestrator.EventChangesRequested,
		orchestrator.EventMergeReady,
		orchestrator.EventMergeFailed,
		orchestrator.EventMergeComplete,
		orchestrator.EventFixUpComplete,
		orchestrator.EventSessionComplete,
		orchestrator.EventSessionFailed,
		orchestrator.EventAgentIdle,
		orchestrator.EventReviewComplete,
	}

	seen := make(map[orchestrator.EventKind]bool, len(kinds))
	for _, kind := range kinds {
		assert.False(t, seen[kind], "duplicate EventKind: %s", kind)
		seen[kind] = true
	}
}
