package orchestrator

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/JR-G/squad0/internal/agent"
	"github.com/JR-G/squad0/internal/pipeline"
)

// RunIdleDuties engages idle agents with productive activities that
// are not ticket implementation: informal PR reviews, UX observations,
// and architectural commentary. Each item is commented on at most once
// per orchestrator lifetime to avoid noise.
func (orch *Orchestrator) RunIdleDuties(ctx context.Context, idleRoles []agent.Role) {
	if orch.pipelineStore == nil {
		return
	}

	openPRs, err := orch.pipelineStore.OpenWithPR(ctx)
	if err != nil || len(openPRs) == 0 {
		return
	}

	for _, role := range idleRoles {
		orch.tryIdleDuty(ctx, role, openPRs)
	}
}

func (orch *Orchestrator) tryIdleDuty(ctx context.Context, role agent.Role, openPRs []pipeline.WorkItem) {
	item := orch.pickUncommentedPR(role, openPRs)
	if item == nil {
		return
	}

	orch.markCommented(role, item.ID)

	prompt := orch.buildIdlePrompt(role, item)
	if prompt == "" {
		return
	}

	agentInstance, ok := orch.agents[role]
	if !ok {
		return
	}

	response, err := agentInstance.QuickChat(ctx, prompt)
	if err != nil {
		log.Printf("idle duty: %s QuickChat failed: %v", role, err)
		return
	}

	response = filterPassResponse(response)
	if response == "" {
		return
	}

	log.Printf("idle duty: %s commenting on %s's PR for %s", role, item.Engineer, item.Ticket)
	orch.postAsRole(ctx, "engineering", response, role)
}

func (orch *Orchestrator) pickUncommentedPR(role agent.Role, openPRs []pipeline.WorkItem) *pipeline.WorkItem {
	for idx := range openPRs {
		item := &openPRs[idx]

		// Don't comment on your own PR.
		if item.Engineer == role {
			continue
		}

		if orch.hasCommented(role, item.ID) {
			continue
		}

		return item
	}
	return nil
}

func (orch *Orchestrator) buildIdlePrompt(role agent.Role, item *pipeline.WorkItem) string {
	engineerName := orch.NameForRole(item.Engineer)

	// Build context so agents know what the PR is about.
	context := fmt.Sprintf("%s is working on %s (stage: %s, branch: %s).",
		engineerName, item.Ticket, item.Stage, item.Branch)
	if item.PRURL != "" {
		context += fmt.Sprintf(" PR: %s", item.PRURL)
	}

	rules := "You are posting this directly to Slack as yourself. " +
		"Do NOT say you can't access anything. Do NOT draft for someone else. " +
		"Respond with ONLY what you'd type. 1-2 sentences. " +
		"Use " + engineerName + "'s name. No markdown headers."

	switch {
	case isEngineerRole(role):
		return fmt.Sprintf(
			"%s\n\nWrite a casual comment — something encouraging, a question, or an observation. %s",
			context, rules,
		)

	case role == agent.RoleDesigner:
		return fmt.Sprintf(
			"%s\n\nIf this might touch the UI, write a brief UX observation. If purely backend, say PASS. %s",
			context, rules,
		)

	case role == agent.RoleTechLead:
		return fmt.Sprintf(
			"%s\n\nWrite a brief architectural observation about the approach or structure. %s",
			context, rules,
		)

	default:
		return ""
	}
}

func (orch *Orchestrator) hasCommented(role agent.Role, itemID int64) bool {
	key := idleCommentKey(role, itemID)
	return orch.followedUp[key]
}

func (orch *Orchestrator) markCommented(role agent.Role, itemID int64) {
	key := idleCommentKey(role, itemID)
	orch.followedUp[key] = true
}

// idleCommentKey produces a unique int64 key by combining the role
// hash and item ID. Uses a simple offset to avoid collisions with
// the existing followedUp entries keyed by plain item ID.
func idleCommentKey(role agent.Role, itemID int64) int64 {
	roleHash := int64(0)
	for _, ch := range string(role) {
		roleHash = roleHash*31 + int64(ch)
	}
	// Shift into a range that won't collide with raw item IDs.
	return roleHash*1_000_000 + itemID
}

func isEngineerRole(role agent.Role) bool {
	return strings.HasPrefix(string(role), "engineer-")
}

// FilterIdleDutyRolesForTest exports filterIdleDutyRoles for testing.
func FilterIdleDutyRolesForTest(roles []agent.Role) []agent.Role {
	return filterIdleDutyRoles(roles)
}

// filterIdleDutyRoles returns roles eligible for idle duties: Tech Lead,
// Designer, and engineers. PM and Reviewer are excluded.
func filterIdleDutyRoles(roles []agent.Role) []agent.Role {
	eligible := make([]agent.Role, 0, len(roles))
	for _, role := range roles {
		switch role { //nolint:exhaustive // only excluding specific roles
		case agent.RolePM, agent.RoleReviewer:
			continue
		default:
			eligible = append(eligible, role)
		}
	}
	return eligible
}
