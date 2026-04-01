package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
)

// ResolveConflictsForTest exports resolveConflicts for testing.
func (orch *Orchestrator) ResolveConflictsForTest(ctx context.Context, idleRoles []agent.Role) {
	orch.resolveConflicts(ctx, idleRoles)
}

// IsRoleInListForTest exports isRoleInList for testing.
func IsRoleInListForTest(role agent.Role, roles []agent.Role) bool {
	return isRoleInList(role, roles)
}

// resolveConflicts checks all open PRs in the pipeline for merge
// conflicts and sends the owning engineer to rebase. This is the
// HIGHEST priority action — nothing ships until conflicts are cleared.
func (orch *Orchestrator) resolveConflicts(ctx context.Context, idleRoles []agent.Role) {
	if orch.pipelineStore == nil {
		return
	}

	openPRs, err := orch.pipelineStore.OpenWithPR(ctx)
	if err != nil || len(openPRs) == 0 {
		return
	}

	for _, item := range openPRs {
		engineer := item.Engineer
		if !isRoleInList(engineer, idleRoles) {
			continue
		}

		if !orch.prHasConflicts(ctx, item.PRURL) {
			continue
		}

		log.Printf("conflicts: %s has conflicts on %s — sending %s to rebase", item.Ticket, item.PRURL, engineer)
		orch.startConflictResolution(ctx, item)
	}
}

func (orch *Orchestrator) startConflictResolution(ctx context.Context, item pipeline.WorkItem) {
	engineerAgent, ok := orch.agents[item.Engineer]
	if !ok {
		return
	}

	ticketLink := orch.cfg.Links.TicketLink(item.Ticket)
	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("PR for %s has merge conflicts — rebasing now.", ticketLink),
		item.Engineer)

	workSession, err := NewWorkSession(ctx, orch.cfg.TargetRepoDir, item.Engineer, item.Ticket)
	if err != nil {
		log.Printf("conflicts: worktree creation failed for %s: %v", item.Ticket, err)
		return
	}
	defer workSession.Cleanup(ctx)

	branch := fmt.Sprintf("feat/%s", strings.ToLower(item.Ticket))
	prompt := fmt.Sprintf(
		"The PR for %s has merge conflicts.\n\n"+
			"PR: %s\nBranch: %s\n\n"+
			"Steps:\n"+
			"1. git fetch origin main\n"+
			"2. git checkout %s\n"+
			"3. git rebase origin/main\n"+
			"4. Resolve any conflicts\n"+
			"5. git push --force-with-lease origin %s\n\n"+
			"If conflicts are too complex, respond with FAILED.",
		item.Ticket, item.PRURL, branch, branch, branch)

	_, taskErr := engineerAgent.ExecuteTask(ctx, prompt, nil, workSession.Dir())
	if taskErr != nil {
		log.Printf("conflicts: rebase failed for %s: %v", item.Ticket, taskErr)
		orch.announceAsRole(ctx, "engineering",
			fmt.Sprintf("%s has conflicts that need manual resolution", item.Ticket),
			agent.RolePM)
		return
	}

	log.Printf("conflicts: %s rebased successfully", item.Ticket)
	orch.postAsRole(ctx, "engineering",
		fmt.Sprintf("Rebased %s — conflicts resolved, PR is clean.", ticketLink),
		item.Engineer)
}

func isRoleInList(role agent.Role, roles []agent.Role) bool {
	for _, candidate := range roles {
		if candidate == role {
			return true
		}
	}
	return false
}
