package orchestrator

import (
	"context"
	"log"

	"github.com/JR-G/squad0/internal/agent"
)

// TriggerPendingReviewsForTest exports triggerPendingReviews for testing.
func (orch *Orchestrator) TriggerPendingReviewsForTest(ctx context.Context, idleRoles []agent.Role) {
	orch.triggerPendingReviews(ctx, idleRoles)
}

// ContainsRoleForTest exports containsRole for testing.
func ContainsRoleForTest(roles []agent.Role, target agent.Role) bool {
	return containsRole(roles, target)
}

// triggerPendingReviews checks for unreviewed PRs in the pipeline and
// starts a review if the reviewer is idle. Runs before new work
// assignment so existing PRs don't pile up.
func (orch *Orchestrator) triggerPendingReviews(ctx context.Context, idleRoles []agent.Role) {
	if orch.pipelineStore == nil {
		return
	}

	if !containsRole(idleRoles, agent.RoleReviewer) {
		return
	}

	openPRs, err := orch.pipelineStore.OpenWithPR(ctx)
	if err != nil || len(openPRs) == 0 {
		return
	}

	for _, item := range openPRs {
		if item.Stage != "pr_opened" {
			continue
		}

		log.Printf("review trigger: starting review of %s (%s)", item.Ticket, item.PRURL)
		orch.startReview(ctx, item.PRURL, item.Ticket, item.ID, item.Engineer)
		return // One review at a time.
	}
}

func containsRole(roles []agent.Role, target agent.Role) bool {
	for _, role := range roles {
		if role == target {
			return true
		}
	}
	return false
}
