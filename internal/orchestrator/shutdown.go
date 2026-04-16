package orchestrator

import (
	"context"
	"log"
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
// and tracks them on orch.wg so drainSessions waits for them.
func (orch *Orchestrator) runPostSessionAsync(ctx context.Context, agentInstance *agent.Agent, ticket, transcript string) {
	postCtx, postCancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)

	orch.wg.Add(1)
	go func() {
		defer orch.wg.Done()
		defer postCancel()

		orch.PersistFindings(postCtx, ticket, transcript)

		pmAgent := orch.agents[agent.RolePM]
		if pmAgent != nil {
			FlushSessionMemory(postCtx, pmAgent, agentInstance, ticket, transcript)
		}
	}()
}

// drainSessions blocks until in-flight session goroutines finish or the
// shutdown grace period elapses, whichever comes first. Critical for
// pre-exit memory flushes — without this, learnings from sessions still
// running at shutdown are silently dropped.
func (orch *Orchestrator) drainSessions() {
	orch.drainSessionsFor(shutdownGrace)
}

func (orch *Orchestrator) drainSessionsFor(grace time.Duration) {
	done := make(chan struct{})
	go func() {
		orch.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("orchestrator: all sessions drained cleanly")
	case <-time.After(grace):
		log.Printf("orchestrator: shutdown grace (%s) elapsed with sessions still running — exiting anyway", grace)
	}
}
