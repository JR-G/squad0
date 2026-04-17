package orchestrator_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/orchestrator"
	"github.com/stretchr/testify/assert"
)

func TestSessionTracker_Cancel_InvokesAndForgets(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewSessionTracker()
	var called atomic.Int32

	tracker.Register(agent.RoleEngineer1, func() { called.Add(1) })
	tracker.Cancel(agent.RoleEngineer1)

	assert.Equal(t, int32(1), called.Load())

	// Calling again is a no-op — entry was forgotten.
	tracker.Cancel(agent.RoleEngineer1)
	assert.Equal(t, int32(1), called.Load())
}

func TestSessionTracker_Cancel_UnknownRole_Noop(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewSessionTracker()

	assert.NotPanics(t, func() {
		tracker.Cancel(agent.RoleDesigner)
	})
}

func TestSessionTracker_Clear_DoesNotInvokeCancel(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewSessionTracker()
	var called atomic.Int32

	tracker.Register(agent.RoleEngineer1, func() { called.Add(1) })
	tracker.Clear(agent.RoleEngineer1)

	assert.Equal(t, int32(0), called.Load(), "Clear must not invoke cancel")

	// Cancel after Clear is a no-op since entry was forgotten.
	tracker.Cancel(agent.RoleEngineer1)
	assert.Equal(t, int32(0), called.Load())
}

func TestSessionTracker_CancelAll_InvokesEvery(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewSessionTracker()
	var calls atomic.Int32

	for _, role := range []agent.Role{agent.RoleEngineer1, agent.RoleEngineer2, agent.RoleEngineer3} {
		tracker.Register(role, func() { calls.Add(1) })
	}

	tracker.CancelAll()
	assert.Equal(t, int32(3), calls.Load())

	// All entries should be cleared.
	tracker.CancelAll()
	assert.Equal(t, int32(3), calls.Load())
}

func TestSessionTracker_Register_ReplacesPriorWithoutInvoking(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewSessionTracker()
	var firstCalled, secondCalled atomic.Int32

	tracker.Register(agent.RoleEngineer1, func() { firstCalled.Add(1) })
	tracker.Register(agent.RoleEngineer1, func() { secondCalled.Add(1) })

	assert.Equal(t, int32(0), firstCalled.Load(), "Register must not invoke the prior cancel")

	tracker.Cancel(agent.RoleEngineer1)

	assert.Equal(t, int32(0), firstCalled.Load())
	assert.Equal(t, int32(1), secondCalled.Load())
}

func TestSessionTracker_DrainFor_NoGoroutines_ReturnsImmediately(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewSessionTracker()

	start := time.Now()
	tracker.DrainFor(100 * time.Millisecond)
	elapsed := time.Since(start)

	assert.Less(t, elapsed, 50*time.Millisecond)
}

func TestSessionTracker_Wait_WaitsForGoroutines(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewSessionTracker()
	release := make(chan struct{})

	tracker.Add(1)
	go func() {
		defer tracker.Done()
		<-release
	}()

	close(release)
	tracker.Wait()
}

func TestSessionTracker_CancelDuringActiveSession_PropagatesToCtx(t *testing.T) {
	t.Parallel()

	tracker := orchestrator.NewSessionTracker()
	ctx, cancel := context.WithCancel(context.Background())
	tracker.Register(agent.RoleEngineer1, cancel)

	tracker.Cancel(agent.RoleEngineer1)

	select {
	case <-ctx.Done():
		// Expected.
	case <-time.After(50 * time.Millisecond):
		t.Fatal("ctx should have been cancelled")
	}
}
