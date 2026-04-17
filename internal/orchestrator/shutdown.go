package orchestrator

import (
	"context"
	"time"

	"github.com/JR-G/squad0/internal/agent"
)

// shutdownGrace is how long Run waits for in-flight session goroutines
// (work, reviews, memory flushes) to finish after ctx is cancelled. Long
// enough to let a memory flush extraction complete; short enough that a
// stuck goroutine can't block shutdown indefinitely.
const shutdownGrace = 30 * time.Second

// runPostSessionAsync detaches findings persistence and memory flush
// from the session context (which is cancelled on shutdown or pause)
// and tracks them on the SessionTracker so drainSessions waits for
// them.
func (orch *Orchestrator) runPostSessionAsync(ctx context.Context, agentInstance *agent.Agent, ticket, transcript string) {
	postCtx, postCancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)

	orch.sessions.Add(1)
	go func() {
		defer orch.sessions.Done()
		defer postCancel()

		orch.PersistFindings(postCtx, ticket, transcript)

		pmAgent := orch.agents[agent.RolePM]
		if pmAgent != nil {
			FlushSessionMemory(postCtx, pmAgent, agentInstance, ticket, transcript)
		}
	}()
}

// drainSessions waits for in-flight session goroutines or the
// shutdown grace period to elapse, whichever comes first. Critical
// for pre-exit memory flushes — without this, learnings from
// sessions still running at shutdown are silently dropped.
func (orch *Orchestrator) drainSessions() {
	orch.sessions.DrainFor(shutdownGrace)
}

// drainSessionsFor exposes the bounded wait for tests that need a
// short grace period to verify the timeout branch fires.
func (orch *Orchestrator) drainSessionsFor(grace time.Duration) {
	orch.sessions.DrainFor(grace)
}
