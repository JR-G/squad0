package orchestrator

import (
	"context"
	"fmt"

	"github.com/JR-G/squad0/internal/agent"
)

// setupFixUpWorktree creates an isolated worktree for a fix-up
// session on the existing PR branch. Returns an error on failure —
// callers must abort rather than fall back to the shared target repo,
// because parallel fix-ups in one directory cause cross-ticket commit
// pollution on the PR branch.
func (orch *Orchestrator) setupFixUpWorktree(ctx context.Context, prURL string, role agent.Role, ticket string) (string, *WorkSession, error) {
	if ctx.Err() != nil {
		return "", nil, ctx.Err()
	}
	session, err := NewFixUpSession(ctx, orch.cfg.TargetRepoDir, prURL, role, ticket)
	if err != nil {
		return "", nil, err
	}
	dir := session.Dir()
	return dir, session, nil
}

// escalateWorktreeFailure pushes a critical situation so the CEO
// sees the blocked fix-up and can intervene. Fix-ups must never run
// in the shared target repo as a fallback, so the ticket is stuck
// until worktree setup can succeed.
func (orch *Orchestrator) escalateWorktreeFailure(ctx context.Context, ticket, prURL string, engineerRole agent.Role, cause error) {
	if orch.situations == nil {
		return
	}
	name := orch.NameForRole(engineerRole)
	orch.situations.Push(Situation{
		Type:        SitPipelineDrift,
		Severity:    SeverityCritical,
		Engineer:    engineerRole,
		Ticket:      ticket,
		PRURL:       prURL,
		Description: fmt.Sprintf("%s's fix-up for %s could not start — worktree setup failed: %v", name, ticket, cause),
	})
	orch.announceAsRole(ctx, "triage",
		fmt.Sprintf("Can't start fix-up for %s — worktree setup failed. Manual resolution needed.", ticket),
		agent.RolePM)
}
