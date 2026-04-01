package orchestrator

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// EventKind identifies the type of event flowing through the bus.
type EventKind string

const (
	// EventPRApproved fires when a PR passes review.
	EventPRApproved EventKind = "pr_approved"
	// EventChangesRequested fires when a reviewer asks for fixes.
	EventChangesRequested EventKind = "changes_requested"
	// EventMergeReady fires when a PR is ready to merge.
	EventMergeReady EventKind = "merge_ready"
	// EventMergeFailed fires when a merge attempt fails.
	EventMergeFailed EventKind = "merge_failed"
	// EventMergeComplete fires when a PR has been merged.
	EventMergeComplete EventKind = "merge_complete"
	// EventFixUpComplete fires when a fix-up session finishes.
	EventFixUpComplete EventKind = "fixup_complete"
	// EventSessionComplete fires when an engineer session finishes.
	EventSessionComplete EventKind = "session_complete"
	// EventSessionFailed fires when an engineer session fails.
	EventSessionFailed EventKind = "session_failed"
	// EventAgentIdle fires when an agent becomes idle with no work.
	EventAgentIdle EventKind = "agent_idle"
	// EventReviewComplete fires when a review session finishes.
	EventReviewComplete EventKind = "review_complete"
)

// Event carries data about something that happened in the pipeline.
type Event struct {
	Kind         EventKind
	Ticket       string
	PRURL        string
	WorkItemID   int64
	EngineerRole agent.Role
	Transcript   string
	Timestamp    time.Time
}

// EventHandler is a function that reacts to an event.
type EventHandler func(ctx context.Context, event Event)

// EventBus dispatches events to registered handlers. Handlers run in
// separate goroutines with panic recovery so one failing handler
// cannot break others.
type EventBus struct {
	mu       sync.RWMutex
	handlers map[EventKind][]EventHandler
}

// NewEventBus creates an empty event bus ready for handler registration.
func NewEventBus() *EventBus {
	return &EventBus{
		handlers: make(map[EventKind][]EventHandler),
	}
}

// On registers a handler for the given event kind. Multiple handlers
// can be registered for the same kind.
func (bus *EventBus) On(kind EventKind, handler EventHandler) {
	bus.mu.Lock()
	defer bus.mu.Unlock()
	bus.handlers[kind] = append(bus.handlers[kind], handler)
}

// Emit dispatches an event to all registered handlers. Each handler
// runs in its own goroutine with panic recovery. Errors are logged
// but never propagated — the emitter is fire-and-forget.
func (bus *EventBus) Emit(ctx context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	bus.mu.RLock()
	handlers := make([]EventHandler, len(bus.handlers[event.Kind]))
	copy(handlers, bus.handlers[event.Kind])
	bus.mu.RUnlock()

	for _, handler := range handlers {
		go runHandler(ctx, event, handler)
	}
}

// EmitSync dispatches an event and waits for all handlers to finish.
// Useful in tests where you need deterministic sequencing.
func (bus *EventBus) EmitSync(ctx context.Context, event Event) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	bus.mu.RLock()
	handlers := make([]EventHandler, len(bus.handlers[event.Kind]))
	copy(handlers, bus.handlers[event.Kind])
	bus.mu.RUnlock()

	var wg sync.WaitGroup
	wg.Add(len(handlers))

	for _, handler := range handlers {
		go func() {
			defer wg.Done()
			runHandler(ctx, event, handler)
		}()
	}

	wg.Wait()
}

// HandlerCount returns the number of handlers registered for a kind.
func (bus *EventBus) HandlerCount(kind EventKind) int {
	bus.mu.RLock()
	defer bus.mu.RUnlock()
	return len(bus.handlers[kind])
}

func runHandler(ctx context.Context, event Event, handler EventHandler) {
	defer func() {
		if recovered := recover(); recovered != nil {
			log.Printf("event handler panicked for %s: %v", event.Kind, recovered)
		}
	}()
	handler(ctx, event)
}

// SetEventBus connects the event bus for event-driven coordination.
func (orch *Orchestrator) SetEventBus(bus *EventBus) {
	orch.eventBus = bus
}

// EventBus returns the orchestrator's event bus, or nil if not set.
func (orch *Orchestrator) EventBus() *EventBus {
	return orch.eventBus
}

// EmitEventForTest exports emitEvent for testing.
func (orch *Orchestrator) EmitEventForTest(ctx context.Context, kind EventKind, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	orch.emitEvent(ctx, kind, prURL, ticket, workItemID, engineerRole)
}

func (orch *Orchestrator) emitEvent(ctx context.Context, kind EventKind, prURL, ticket string, workItemID int64, engineerRole agent.Role) {
	if orch.eventBus == nil {
		return
	}
	orch.eventBus.Emit(ctx, Event{
		Kind:         kind,
		Ticket:       ticket,
		PRURL:        prURL,
		WorkItemID:   workItemID,
		EngineerRole: engineerRole,
	})
}

// RegisterDefaultHandlers wires up the standard event handlers that
// connect events to existing orchestrator methods.
// RegisterDefaultHandlers wires up event handlers for async operations.
// Note: review → approve → merge is SYNCHRONOUS (not event-driven)
// to prevent races. These handlers are for genuinely async operations.
func (orch *Orchestrator) RegisterDefaultHandlers(bus *EventBus) {
	bus.On(EventMergeFailed, func(ctx context.Context, event Event) {
		orch.startFixUp(ctx, event.PRURL, event.Ticket, event.WorkItemID, event.EngineerRole)
	})

	bus.On(EventAgentIdle, func(ctx context.Context, event Event) {
		orch.RunIdleDuties(ctx, []agent.Role{event.EngineerRole})
	})
}
