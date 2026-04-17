package orchestrator_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWorkScheduler_EmptyEligible_ReturnsFalse(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	sched := orchestrator.NewWorkScheduler(orchestrator.NewAssigner(pmAgent, "TEST"))

	got := sched.Schedule(context.Background(), nil, nil)

	assert.False(t, got)
	assert.False(t, sched.IsRunning())
}

func TestWorkScheduler_Concurrent_SecondScheduleReturnsFalse(t *testing.T) {
	t.Parallel()

	// PM runner blocks until released so the first Schedule's goroutine
	// is still in flight when we try to schedule again.
	release := make(chan struct{})
	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)
	sched := orchestrator.NewWorkScheduler(orchestrator.NewAssigner(pmAgent, "TEST"))

	dispatched := make(chan struct{})
	first := sched.Schedule(context.Background(), []agent.Role{agent.RoleEngineer1},
		func(_ context.Context, _ []orchestrator.Assignment, _ []agent.Role) {
			<-release
			close(dispatched)
		})
	require.True(t, first)

	// Wait until the first Schedule's goroutine is genuinely running.
	require.Eventually(t, sched.IsRunning, time.Second, 5*time.Millisecond)

	second := sched.Schedule(context.Background(), []agent.Role{agent.RoleEngineer2}, nil)
	assert.False(t, second, "second Schedule must be gated while first is in flight")

	close(release)
	<-dispatched
}

func TestWorkScheduler_DispatcherReceivesAssignments(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[{\"role\":\"engineer-1\",\"ticket\":\"JAM-1\",\"description\":\"do thing\"}]"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)
	sched := orchestrator.NewWorkScheduler(orchestrator.NewAssigner(pmAgent, "TEST"))

	var captured atomic.Pointer[[]orchestrator.Assignment]
	done := make(chan struct{})

	first := sched.Schedule(context.Background(), []agent.Role{agent.RoleEngineer1},
		func(_ context.Context, assignments []orchestrator.Assignment, _ []agent.Role) {
			defer close(done)
			cp := append([]orchestrator.Assignment(nil), assignments...)
			captured.Store(&cp)
		})
	require.True(t, first)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher never called")
	}

	got := captured.Load()
	require.NotNil(t, got)
	require.Len(t, *got, 1)
	assert.Equal(t, "JAM-1", (*got)[0].Ticket)
}

func TestWorkScheduler_NilDispatcher_DiscardsResult(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{output: []byte(`{"type":"result","result":"[]"}` + "\n")}
	pmAgent := setupPMAgent(t, pmRunner)
	sched := orchestrator.NewWorkScheduler(orchestrator.NewAssigner(pmAgent, "TEST"))

	first := sched.Schedule(context.Background(), []agent.Role{agent.RoleEngineer1}, nil)
	require.True(t, first)

	// Gate releases after the goroutine completes even with no dispatcher.
	require.Eventually(t, func() bool { return !sched.IsRunning() }, time.Second, 5*time.Millisecond)
}

func TestWorkScheduler_AssignmentError_DispatcherCalledWithNil(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{
		output: []byte("garbage that won't parse"),
	}
	pmAgent := setupPMAgent(t, pmRunner)
	sched := orchestrator.NewWorkScheduler(orchestrator.NewAssigner(pmAgent, "TEST"))

	called := make(chan []orchestrator.Assignment, 1)
	first := sched.Schedule(context.Background(), []agent.Role{agent.RoleEngineer1},
		func(_ context.Context, assignments []orchestrator.Assignment, _ []agent.Role) {
			called <- assignments
		})
	require.True(t, first)

	select {
	case assignments := <-called:
		// Garbled response parses to no assignments — not an error,
		// just an empty result. Dispatcher gets the nil/empty slice.
		assert.Empty(t, assignments)
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher was never called")
	}
}

func TestWorkScheduler_GateReleasedAfterDispatcher(t *testing.T) {
	t.Parallel()

	pmRunner := &fakeProcessRunner{
		output: []byte(`{"type":"result","result":"[]"}` + "\n"),
	}
	pmAgent := setupPMAgent(t, pmRunner)
	sched := orchestrator.NewWorkScheduler(orchestrator.NewAssigner(pmAgent, "TEST"))

	done := make(chan struct{})
	first := sched.Schedule(context.Background(), []agent.Role{agent.RoleEngineer1},
		func(_ context.Context, _ []orchestrator.Assignment, _ []agent.Role) { close(done) })
	require.True(t, first)

	<-done

	// After dispatcher returns, gate should be released — eventually,
	// because release happens in the deferred path of the goroutine.
	require.Eventually(t, func() bool { return !sched.IsRunning() }, time.Second, 5*time.Millisecond)
}
