package orchestrator

import (
	"context"
	"log"
	"sync"

	"github.com/JR-G/squad0/internal/agent"
)

// WorkScheduler owns the per-tick assignment dance: it gates
// concurrent assignment runs, asks the Assigner for ticket→engineer
// matches, and dispatches each match via a caller-supplied
// dispatcher. The dispatcher is responsible for actually spawning
// the session — the scheduler doesn't reach into agent state, the
// pipeline, or the check-in store.
//
// Third slice of the orchestrator god-object split. The previous
// in-orchestrator implementation interleaved gate management,
// assignment fetch, and idle-duty fan-out in one anonymous goroutine
// inside tryAssignWork. Splitting the gate / fetch out leaves the
// orchestrator to handle dispatch and idle handling, which still
// need orchestrator-wide context.
type WorkScheduler struct {
	assigner *Assigner

	mu      sync.Mutex
	running bool
}

// NewWorkScheduler wraps an Assigner with the in-flight gate.
func NewWorkScheduler(assigner *Assigner) *WorkScheduler {
	return &WorkScheduler{assigner: assigner}
}

// Dispatcher is the callback Schedule invokes once an assignment
// round completes. ctx is the same context Schedule was called with
// so callers can honour cancellation; assignments is nil if the
// underlying Assigner errored or returned nothing.
type Dispatcher func(ctx context.Context, assignments []Assignment, eligible []agent.Role)

// Schedule kicks off an assignment round in the background. Returns
// false if a round is already in flight (the caller should drop and
// try again next tick). The dispatcher is invoked with the result
// once the round completes; if dispatcher is nil the result is
// discarded.
//
// Errors from the underlying Assigner are logged and surfaced via
// dispatcher with assignments=nil so the caller can do its
// "no-work-available" idle handling consistently.
func (sched *WorkScheduler) Schedule(ctx context.Context, eligible []agent.Role, dispatcher Dispatcher) bool {
	if len(eligible) == 0 {
		return false
	}

	if !sched.acquire() {
		return false
	}

	go func() {
		defer sched.release()
		sched.runRound(ctx, eligible, dispatcher)
	}()

	return true
}

func (sched *WorkScheduler) runRound(ctx context.Context, eligible []agent.Role, dispatcher Dispatcher) {
	assignments, err := sched.assigner.RequestAssignments(ctx, eligible)
	if err != nil {
		log.Printf("tick: assignment failed: %v", err)
		notifyDispatcher(ctx, dispatcher, nil, eligible)
		return
	}
	notifyDispatcher(ctx, dispatcher, assignments, eligible)
}

func notifyDispatcher(ctx context.Context, dispatcher Dispatcher, assignments []Assignment, eligible []agent.Role) {
	if dispatcher == nil {
		return
	}
	dispatcher(ctx, assignments, eligible)
}

// IsRunning reports whether a Schedule round is currently in flight.
// Useful for tests verifying the gate works.
func (sched *WorkScheduler) IsRunning() bool {
	sched.mu.Lock()
	defer sched.mu.Unlock()
	return sched.running
}

func (sched *WorkScheduler) acquire() bool {
	sched.mu.Lock()
	defer sched.mu.Unlock()
	if sched.running {
		return false
	}
	sched.running = true
	return true
}

func (sched *WorkScheduler) release() {
	sched.mu.Lock()
	defer sched.mu.Unlock()
	sched.running = false
}
