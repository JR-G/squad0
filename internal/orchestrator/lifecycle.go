package orchestrator

import (
	"context"
	"fmt"
	"log"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
)

// PauseAgent pauses the given agent: cancels any running session and
// sets the check-in status to paused so the tick loop will not assign
// new work.
func (orch *Orchestrator) PauseAgent(ctx context.Context, role agent.Role) error {
	orch.cancelSession(role)
	log.Printf("paused session for %s", role)

	err := orch.checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         role,
		Status:        coordination.StatusPaused,
		FilesTouching: []string{},
		Message:       "paused by CEO",
	})
	if err != nil {
		return fmt.Errorf("pausing %s: %w", role, err)
	}

	orch.postAsRole(ctx, "feed", fmt.Sprintf("%s has been paused.", role), agent.RolePM)
	return nil
}

// ResumeAgent unpauses the given agent, making them available for
// assignment on the next tick.
func (orch *Orchestrator) ResumeAgent(ctx context.Context, role agent.Role) error {
	err := orch.checkIns.SetIdle(ctx, role)
	if err != nil {
		return fmt.Errorf("resuming %s: %w", role, err)
	}

	orch.postAsRole(ctx, "feed", fmt.Sprintf("%s has been resumed.", role), agent.RolePM)
	return nil
}

// PauseAll pauses every agent, cancelling all running sessions.
func (orch *Orchestrator) PauseAll(ctx context.Context) error {
	orch.cancelAllSessions()

	for role := range orch.agents {
		if err := orch.PauseAgent(ctx, role); err != nil {
			return err
		}
	}
	return nil
}

// ResumeAll resumes every agent.
func (orch *Orchestrator) ResumeAll(ctx context.Context) error {
	for role := range orch.agents {
		if err := orch.ResumeAgent(ctx, role); err != nil {
			return err
		}
	}
	return nil
}

// IsPaused returns whether the given agent is currently paused.
func (orch *Orchestrator) IsPaused(ctx context.Context, role agent.Role) bool {
	checkIn, err := orch.checkIns.GetByAgent(ctx, role)
	if err != nil {
		return false
	}
	return checkIn.Status == coordination.StatusPaused
}

// Status returns all current agent check-ins.
func (orch *Orchestrator) Status(ctx context.Context) ([]coordination.CheckIn, error) {
	return orch.checkIns.GetAll(ctx)
}
