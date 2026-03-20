package orchestrator

import (
	"context"
	"fmt"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/coordination"
)

// PauseAgent sets the given agent to idle and posts a message.
func (orch *Orchestrator) PauseAgent(ctx context.Context, role agent.Role) error {
	err := orch.checkIns.Upsert(ctx, coordination.CheckIn{
		Agent:         role,
		Status:        coordination.StatusIdle,
		FilesTouching: []string{},
		Message:       "paused by CEO",
	})
	if err != nil {
		return fmt.Errorf("pausing %s: %w", role, err)
	}

	orch.postAsRole(ctx, "feed", fmt.Sprintf("%s has been paused.", role), agent.RolePM)
	return nil
}

// ResumeAgent clears the paused message for an agent, making them
// available for assignment on the next tick.
func (orch *Orchestrator) ResumeAgent(ctx context.Context, role agent.Role) error {
	err := orch.checkIns.SetIdle(ctx, role)
	if err != nil {
		return fmt.Errorf("resuming %s: %w", role, err)
	}

	orch.postAsRole(ctx, "feed", fmt.Sprintf("%s has been resumed.", role), agent.RolePM)
	return nil
}

// PauseAll pauses every agent.
func (orch *Orchestrator) PauseAll(ctx context.Context) error {
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

// Status returns all current agent check-ins.
func (orch *Orchestrator) Status(ctx context.Context) ([]coordination.CheckIn, error) {
	return orch.checkIns.GetAll(ctx)
}
